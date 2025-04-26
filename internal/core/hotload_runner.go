package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/textbank"
)

type HotloadRunner struct {
	Options       *option.HotloadOption
	BuildTimeout  time.Duration
	Delay         time.Duration
	ExecCommand   func(ctx context.Context, name string, args ...string) *exec.Cmd
	watcher       *fsnotify.Watcher
	cmd           *exec.Cmd
	currentCmd    *exec.Cmd
	mu            sync.Mutex
	debounceTimer *time.Timer
	fileHashes    sync.Map
}

type pendingChangeHotload struct {
	path string
	time time.Time
}

var (
	pendingHotload   = make(map[string]pendingChangeHotload)
	pendingMuHotload sync.Mutex
)

func NewHotloadRunner(opt *option.HotloadOption) *HotloadRunner {
	timeout, _ := opt.Timeout.TimeDuration()
	delay, _ := opt.Delay.TimeDuration()
	return &HotloadRunner{
		Options:      opt,
		BuildTimeout: timeout,
		Delay:        delay,
		ExecCommand:  exec.CommandContext,
	}
}

func (r *HotloadRunner) ReplacePlaceholders(command, path string) string {
	return common.ReplacePlaceholders(command, r.Options.Placeholder, path)
}

func (r *HotloadRunner) CollectWatchDirs(root string) ([]string, error) {
	var result []string
	visited := make(map[string]bool)

	return result, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		if r.IsExcludedDir(real) {
			return filepath.SkipDir
		}

		if visited[real] {
			return filepath.SkipDir
		}

		visited[real] = true
		result = append(result, real)
		return nil
	})
}

func (r *HotloadRunner) AddWatchDirs(dirs []string) {
	for _, dir := range dirs {
		if !r.ShouldWatchDir(dir) {
			continue
		}
		if err := r.watcher.Add(dir); err != nil {
			log.Printf("⚠️  Failed to watch %s: %v", dir, err)
		} else {
			absPath, _ := filepath.Abs(dir)
			log.Printf("👀 Watching %s", absPath)
		}
	}
}

func (r *HotloadRunner) AddWatchRecursive(path string) error {
	dirs, err := r.CollectWatchDirs(path)
	if err != nil {
		return err
	}
	r.AddWatchDirs(dirs)
	return nil
}

func (r *HotloadRunner) SetWatcher(w *fsnotify.Watcher) {
	r.watcher = w
}

func (r *HotloadRunner) Start() error {
	if r.Options.PidPathOpt != "" {
		if err := common.CreatePidFile(r.Options.PidPathOpt); err != nil {
			return err
		}
	}

	if r.Options.LogPathOpt != "" {
		createErr := common.CreateFileWithDirs(r.Options.LogPathOpt, "")
		f, openErr := os.OpenFile(r.Options.LogPathOpt, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		multi := io.MultiWriter(os.Stdout, f)
		if createErr == nil && openErr == nil {
			log.SetOutput(multi)
			defer f.Close()
		}
	}

	fmt.Println(textbank.StartMessage)
	fmt.Printf("Start Hotload mode.\n\n\n")

	signalChan := make(chan os.Signal, 1)
	if runtime.GOOS != "windows" {
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	} else {
		signal.Notify(signalChan, os.Interrupt, os.Kill)
	}
	go func() {
		<-signalChan
		r.stopDaemon()
		r.stopCurrentCmd()
		if r.Options.PidPathOpt != "" {
			if err := common.DeletePidFile(r.Options.PidPathOpt); err != nil {
				log.Printf("failed delete pidfile.: %s\n", err.Error())
			}
		}
		os.Exit(0)
	}()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	r.watcher = watcher
	defer watcher.Close()

	for _, dir := range r.Options.WatchDirList {
		if err := r.AddWatchRecursive(dir); err != nil {
			return err
		}
	}

	if r.Options.BuildAtStart.Bool() {
		if r.Options.HasPlaceholderBuild {
			log.Printf("❗ build command placeholder found. skip build at start.")
			if err := r.startDaemon(); err != nil {
				return err
			}
		} else {
			if r.firstBuild() {
				if err := r.startDaemon(); err != nil {
					return err
				}
			}
		}
	}

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					info, err := os.Lstat(event.Name)
					if err == nil {
						if info.Mode()&os.ModeSymlink != 0 {
							resolved, err := filepath.EvalSymlinks(event.Name)
							if err == nil {
								info, err = os.Stat(resolved)
								if err == nil && info.IsDir() && !r.IsExcludedDir(resolved) {
									_ = r.AddWatchRecursive(resolved)
									log.Printf("👀 Watching symlinked dir: %s", resolved)
								}
							}
						} else if info.IsDir() && !r.IsExcludedDir(event.Name) {
							_ = r.AddWatchRecursive(event.Name)
							log.Printf("👀 Watching new dir: %s", event.Name)
						}
					}
				}

				if r.ShouldTrigger(event) {
					absPath := filepath.Clean(event.Name)
					pendingMuHotload.Lock()
					pendingHotload[absPath] = pendingChangeHotload{path: absPath, time: time.Now()}
					pendingMuHotload.Unlock()

					if r.debounceTimer != nil {
						r.debounceTimer.Stop()
					}
					r.debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
						pendingMuHotload.Lock()
						for _, change := range pendingHotload {
							r.mu.Lock()
							resolvedPath := r.applyArgsPathStyle(change.path)

							if r.Options.CheckContentDiff.Bool() {
								if !r.hasChanged(change.path) {
									log.Printf("🔁 Skipped: %s has no content change", change.path)
									r.mu.Unlock()
									continue
								} else {
									log.Printf("🔔 Change confirmed in: %s", change.path)
								}
							}

							r.handleChangeDirect(resolvedPath)
							r.mu.Unlock()
						}
						pendingHotload = make(map[string]pendingChangeHotload)
						pendingMuHotload.Unlock()
					})
				}
			case err := <-watcher.Errors:
				log.Println("Watcher error:", err)
			}
		}
	}()

	select {}
}

func (r *HotloadRunner) firstBuild() bool {
	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, "") {
			log.Println("✅  Build at start success")
			time.Sleep(r.Delay)
			return true
		}
		time.Sleep(1 * time.Second)
	}
	log.Println("🟥  Build at start fail...")
	return false
}

func (r *HotloadRunner) handleChangeDirect(pathStyled string) {
	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, pathStyled) {
			log.Println("✅  Build success")
			time.Sleep(r.Delay)
			r.stopDaemon()
			r.startDaemon()
			return
		}
		time.Sleep(1 * time.Second)
	}
	log.Println("🟥  Build fail...")
}

func (r *HotloadRunner) hasChanged(path string) bool {
	hash, err := r.computeHash(path)
	if err != nil {
		return true
	}
	if prev, ok := r.fileHashes.Load(path); ok {
		if bytes.Equal(prev.([]byte), hash) {
			return false
		}
	}
	r.fileHashes.Store(path, hash)
	return true
}

func (r *HotloadRunner) computeHash(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(data)
	return h[:], nil
}

func (r *HotloadRunner) IsExcludedDir(path string) bool {
	for _, d := range r.Options.ExcludeDirList {
		if d != "" && strings.Contains(path, d) {
			return true
		}
	}
	return false
}

func (r *HotloadRunner) ShouldWatchDir(dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	// --exclude-dir
	if r.Options.ExcludeDir != "" && r.IsExcludedDir(absDir) {
		return false
	}

	// --ignore-dir-regex
	if r.Options.IgnoreDirRegexp != nil {
		if r.Options.IgnoreDirRegexp.MatchString(absDir) {
			return false
		}
	}

	// .enmaignore
	if r.Options.EnmaIgnore != nil {
		if r.Options.EnmaIgnore.Matches(common.TrimDotSlash(absDir)) {
			return false
		}
	}

	return true
}

func (r *HotloadRunner) ShouldTrigger(event fsnotify.Event) bool {
	path := event.Name
	absPath, _ := filepath.Abs(path)

	if event.Op == fsnotify.Chmod {
		return false
	}

	if r.Options.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result := r.Options.PatternRegexp.MatchString(baseName)
		if result == false {
			return false
		}
	}

	if r.Options.ExcludeDir != "" && r.IsExcludedDir(path) {
		return false
	}

	if r.Options.IncludeExt != "" {
		ext := filepath.Ext(absPath)
		if !slices.Contains(r.Options.IncludeExtList, ext) {
			return false
		}
	}

	if r.Options.IgnoreDirRegexp != nil {
		dir := filepath.Dir(absPath)
		result := r.Options.IgnoreDirRegexp.MatchString(dir)
		if result == true {
			return false
		}
	}

	if r.Options.IgnoreFileRegexp != nil {
		baseName := filepath.Base(absPath)
		result := r.Options.IgnoreFileRegexp.MatchString(baseName)
		if result == true {
			return false
		}
	}

	if r.Options.EnmaIgnore != nil {
		if r.Options.EnmaIgnore.Matches(common.TrimDotSlash(path)) {
			return false
		}
	}

	if r.Options.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		if slices.Contains(r.Options.ExcludeExtList, ext) {
			return false
		}
	}

	return true
}

func (r *HotloadRunner) RunBuildSequence(attempt int, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.BuildTimeout)
	defer cancel()

	steps := []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"PreBuild", r.RunPreBuild},
		{"Build", r.RunBuild},
		{"PostBuild", r.RunPostBuild},
	}

	for _, step := range steps {
		if err := step.fn(ctx, path); err != nil {
			log.Printf("❌  %s failed (attempt %d): %v", step.name, attempt+1, err)
			return false
		}
	}

	r.currentCmd = nil
	return true
}

func (r *HotloadRunner) RunPreBuild(ctx context.Context, path string) error {
	if r.Options.PreBuild == "" {
		return nil
	}
	if r.Options.HasPlaceholderPreBuild && path == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PreBuild, path))
	setProcessGroup(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) RunBuild(ctx context.Context, path string) error {
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.Build, path))
	setProcessGroup(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) RunPostBuild(ctx context.Context, path string) error {
	if r.Options.PostBuild == "" {
		return nil
	}
	if r.Options.HasPlaceholderPostBuild && path == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PostBuild, path))
	setProcessGroup(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) startDaemon() error {
	cmd := exec.Command("sh", "-c", r.Options.Daemon)
	setProcessGroup(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Println("🚀 Start daemon")
	r.cmd = cmd
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func (r *HotloadRunner) stopDaemon() {
	if r.cmd != nil && r.cmd.Process != nil {
		log.Println("🛑 Stopping daemon...")
		stopDaemonProcess(r.cmd, r.Options.SignalName)
		_ = r.cmd.Wait()
		r.cmd = nil
	}
}

func (r *HotloadRunner) stopCurrentCmd() {
	if r.currentCmd != nil && r.currentCmd.Process != nil {
		log.Println("🛑 Aborting build...")
		stopProcess(r.currentCmd)
		_ = r.currentCmd.Wait()
		r.currentCmd = nil
	}
}

func (r *HotloadRunner) applyArgsPathStyle(path string) string {
	var target = common.ToAbsolutePath(path)
	if !r.Options.AbsolutePathFlag.Bool() {
		target = common.ToRelativePath(target)
	}
	if r.Options.ArgsPathStyleString != "" {
		return r.Options.ArgsPathStyle.ArgsPathString(target)
	}
	return target
}
