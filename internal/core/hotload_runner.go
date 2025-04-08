package core

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
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
}

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
		if err := r.watcher.Add(dir); err != nil {
			log.Printf("⚠️  Failed to watch %s: %v", dir, err)
		} else {
			log.Printf("👀 Watching %s", dir)
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
	if r.Options.LogPathOpt != "" {
		createErr := common.CreateFileWithDirs(r.Options.LogPathOpt, "")
		f, openErr := os.OpenFile(r.Options.LogPathOpt, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if createErr == nil && openErr == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}

	signalChan := make(chan os.Signal, 1)
	if runtime.GOOS != "windows" {
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	} else {
		signal.Notify(signalChan, os.Interrupt)
	}
	go func() {
		<-signalChan
		r.stopDaemon()
		r.stopCurrentCmd()
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

	if err := r.startDaemon(); err != nil {
		return err
	}

	eventChan := make(chan fsnotify.Event, 1)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if r.ShouldTrigger(event) {
					select {
					case eventChan <- event:
					default:
					}
				}
			case err := <-watcher.Errors:
				log.Println("Watcher error:", err)
			}
		}
	}()

	for _event := range eventChan {
		if _event.Op&fsnotify.Create == fsnotify.Create {
			info, err := os.Lstat(_event.Name)
			if err != nil {
				continue
			}

			var dirPath string
			if info.Mode()&os.ModeSymlink != 0 {
				resolved, err := filepath.EvalSymlinks(_event.Name)
				if err != nil {
					log.Printf("⚠️  Could not resolve symlink %s: %v", _event.Name, err)
					continue
				}
				info, err = os.Stat(resolved)
				if err != nil || !info.IsDir() {
					continue
				}
				dirPath = resolved
			} else if info.IsDir() {
				dirPath = _event.Name
			} else {
				continue
			}

			if !r.IsExcludedDir(dirPath) {
				_ = r.AddWatchRecursive(dirPath)
			}
		}

		if r.debounceTimer != nil {
			r.debounceTimer.Stop()
		}
		r.debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
			r.handleChange(_event)
		})
	}

	return nil
}

func (r *HotloadRunner) IsExcludedDir(path string) bool {
	for _, d := range r.Options.ExcludeDirList {
		if d != "" && strings.Contains(path, d) {
			return true
		}
	}
	return false
}

func (r *HotloadRunner) ShouldTrigger(event fsnotify.Event) bool {
	path := event.Name
	absPath, _ := filepath.Abs(path)

	if event.Op == fsnotify.Chmod {
		return false
	}

	if r.Options.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result, err := r.Options.PatternRegexp.MatchString(baseName)
		if err != nil {
			log.Fatalf("Fatal error: %s", err)
		} else {
			if result == false {
				return false
			}
		}
	}

	if r.Options.ExcludeDir != "" && r.IsExcludedDir(path) {
		return false
	}

	if r.Options.IncludeExt != "" {
		ext := filepath.Ext(absPath)
		found := false
		for _, incl := range r.Options.IncludeExtList {
			if ext == incl {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if r.Options.IgnoreDirRegexp != nil {
		dir := filepath.Dir(absPath)
		result, err := r.Options.IgnoreDirRegexp.MatchString(dir)
		if err != nil {
			log.Fatalf("Fatal error: %s", err)
		} else {
			if result == true {
				return false
			}
		}
	}

	if r.Options.IgnoreFileRegexp != nil {
		baseName := filepath.Base(absPath)
		result, err := r.Options.IgnoreFileRegexp.MatchString(baseName)
		if err != nil {
			log.Fatalf("Fatal error: %s", err)
		} else {
			if result == true {
				return false
			}
		}
	}

	if r.Options.EnmaIgnore != nil {
		if r.Options.EnmaIgnore.Matches(common.TrimDotSlash(path)) {
			return false
		}
	}

	if r.Options.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		for _, excl := range r.Options.ExcludeExtList {
			if ext == excl {
				return false
			}
		}
	}


	return true
}

func (r *HotloadRunner) handleChange(event fsnotify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := common.ToAbsolutePath(event.Name)

	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, path) {
			log.Println("✅  Build success")
			time.Sleep(r.Delay)
			r.stopDaemon()
			r.startDaemon()
			return
		}
		time.Sleep(1 * time.Second)
	}
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

	return true
}

func (r *HotloadRunner) RunPreBuild(ctx context.Context, path string) error {
	if r.Options.PreBuild == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PreBuild, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) RunBuild(ctx context.Context, path string) error {
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.Build, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) RunPostBuild(ctx context.Context, path string) error {
	if r.Options.PostBuild == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PostBuild, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.currentCmd = cmd
	return cmd.Run()
}

func (r *HotloadRunner) startDaemon() error {
	cmd := exec.Command("sh", "-c", r.Options.Daemon)
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
		if runtime.GOOS == "windows" {
			_ = r.cmd.Process.Kill()
		} else {
			_ = r.cmd.Process.Signal(syscall.SIGTERM)
		}
		_ = r.cmd.Wait()
		r.cmd = nil
	}
}

func (r *HotloadRunner) stopCurrentCmd() {
	if r.currentCmd != nil && r.currentCmd.Process != nil {
		log.Println("🛑 Aborting build...")
		if runtime.GOOS == "windows" {
			_ = r.currentCmd.Process.Kill()
		} else {
			_ = r.currentCmd.Process.Signal(syscall.SIGTERM)
		}
		_ = r.currentCmd.Wait()
		r.currentCmd = nil
	}
}
