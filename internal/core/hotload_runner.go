package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
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
)

type HotloadRunner struct {
	Options      *option.HotloadOption
	BuildTimeout time.Duration
	Delay        time.Duration
	ExecCommand  func(ctx context.Context, name string, args ...string) *exec.Cmd
	// StopGrace is how long the daemon may take to exit after the stop signal
	// before its process group is killed; KillWait bounds the wait after SIGKILL.
	// Zero means DefaultStopGrace / DefaultKillWait.
	StopGrace     time.Duration
	KillWait      time.Duration
	watcher       *fsnotify.Watcher
	procMu        sync.Mutex // guards daemon
	daemon        *managedProcess
	scope         commandScope
	startOnce     startAtOnce
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
		StopGrace:    DefaultStopGrace,
		KillWait:     DefaultKillWait,
	}
}

func (r *HotloadRunner) stopGrace() time.Duration { return orDefault(r.StopGrace, DefaultStopGrace) }
func (r *HotloadRunner) killWait() time.Duration  { return orDefault(r.KillWait, DefaultKillWait) }

// runStartAtOnce runs the start-at-once command; only the first call does anything.
func (r *HotloadRunner) runStartAtOnce() error {
	return r.startOnce.run(func() error {
		return runStartAtOnceCommand(r.Options.StartAtOnce, r.ExecCommand, &r.scope, r.stopGrace(), r.killWait())
	})
}

// ReplacePlaceholders returns command with the placeholder replaced by path as plain text.
// It is for display only: commands are run through ShellCommandArgs, which never
// splices the path into the shell source.
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

	fmt.Println(common.StartMessage())
	fmt.Printf("Start Hotload mode.\n\n\n")

	signalChan := make(chan os.Signal, 1)
	if runtime.GOOS != "windows" {
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	} else {
		signal.Notify(signalChan, os.Interrupt, os.Kill)
	}
	go func() {
		<-signalChan
		log.Println("🛑 Shutting down... (send the signal again to force)")
		done := make(chan error, 1)
		go func() { done <- r.shutdown() }()
		code := 0
		select {
		case err := <-done:
			if err != nil {
				log.Printf("❌ Shutdown incomplete: %v", err)
				code = 1
			}
		case <-signalChan:
			log.Println("⚡ Forced shutdown")
			r.forceKill()
			code = 1
		}
		if r.Options.PidPathOpt != "" {
			if err := common.DeletePidFile(r.Options.PidPathOpt); err != nil {
				log.Printf("failed delete pidfile.: %s\n", err.Error())
			}
		}
		os.Exit(code)
	}()

	// start-at-once runs before the watcher exists (and, in hotload, before the
	// first build): file changes made meanwhile are not seen, monitoring starts after it.
	if err := startAtOnceInStart(r.runStartAtOnce, r.Options.PidPathOpt); err != nil {
		return startErr(err)
	}

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
				return startErr(err)
			}
		} else {
			if r.firstBuild() {
				if err := r.startDaemon(); err != nil {
					return startErr(err)
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

// startErr returns err from Start, except when a signal has already begun the
// shutdown: then the signal handler owns the exit (code and pid file), so Start
// waits for it instead of returning a competing error.
func startErr(err error) error {
	if errors.Is(err, errShuttingDown) {
		select {}
	}
	return err
}

func (r *HotloadRunner) firstBuild() bool {
	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, "") {
			log.Println("✅  Build at start success")
			return sleepCtx(r.scope.context(), r.Delay)
		}
		if !sleepCtx(r.scope.context(), 1*time.Second) {
			return false
		}
	}
	log.Println("🟥  Build at start fail...")
	return false
}

func (r *HotloadRunner) handleChangeDirect(pathStyled string) {
	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, pathStyled) {
			log.Println("✅  Build success")
			if !sleepCtx(r.scope.context(), r.Delay) {
				return
			}
			if err := r.stopDaemon(); err != nil {
				log.Println("🟥 The previous daemon could not be stopped; not starting a new one.")
				return
			}
			if err := r.startDaemon(); err != nil {
				log.Printf("❌ Failed to start daemon: %v", err)
			}
			return
		}
		if !sleepCtx(r.scope.context(), 1*time.Second) {
			return
		}
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
	for dirname := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(path)), "/") {
		if slices.Contains(r.Options.ExcludeDirList, dirname) {
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
	steps := []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"PreBuild", r.RunPreBuild},
		{"Build", r.RunBuild},
		{"PostBuild", r.RunPostBuild},
	}

	for _, step := range steps {
		ctx, cancel := context.WithTimeout(r.scope.context(), r.BuildTimeout)
		err := step.fn(ctx, path)
		cancel()
		if err != nil {
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
	if r.Options.HasPlaceholderPreBuild && path == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.PreBuild, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	release, err := r.scope.enter()
	if err != nil {
		return err
	}
	defer release()
	return cmd.Run()
}

func (r *HotloadRunner) RunBuild(ctx context.Context, path string) error {
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.Build, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	release, err := r.scope.enter()
	if err != nil {
		return err
	}
	defer release()
	return cmd.Run()
}

func (r *HotloadRunner) RunPostBuild(ctx context.Context, path string) error {
	if r.Options.PostBuild == "" {
		return nil
	}
	if r.Options.HasPlaceholderPostBuild && path == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.PostBuild, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	release, err := r.scope.enter()
	if err != nil {
		return err
	}
	defer release()
	return cmd.Run()
}

// currentDaemon returns the tracked daemon (nil when none).
func (r *HotloadRunner) currentDaemon() *managedProcess {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	return r.daemon
}

// startDaemon starts a new daemon unless shutdown has begun or a tracked daemon
// is still alive: at most one daemon runs at a time, and a running one is never
// overwritten (which would orphan it). The checks and the start happen under
// procMu, and shutdown cancels the scope before it reads the daemon, so a daemon
// started concurrently is always seen and stopped.
func (r *HotloadRunner) startDaemon() error {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	if r.scope.context().Err() != nil {
		return errShuttingDown
	}
	if r.daemon != nil && !r.daemon.gone() {
		return errDaemonRunning
	}
	cmd := exec.Command("sh", "-c", r.Options.Daemon)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	p, err := startManaged(cmd)
	if err != nil {
		return err
	}
	log.Println("🚀 Start daemon")
	r.daemon = p
	return nil
}

// stopDaemon stops the running daemon and its process group: stop signal,
// StopGrace, then SIGKILL. It returns an error, and keeps the daemon tracked,
// when the group could not be confirmed gone.
func (r *HotloadRunner) stopDaemon() error {
	p := r.currentDaemon()
	if p == nil {
		return nil
	}
	log.Println("🛑 Stopping daemon...")
	// p.stop is safe to run concurrently (shutdown and a restart may both stop p).
	if err := p.stop(r.Options.SignalName.Signal(), r.stopGrace(), r.killWait()); err != nil {
		log.Printf("❌ Failed to stop daemon: %v", err)
		return err
	}
	r.procMu.Lock()
	if r.daemon == p {
		r.daemon = nil
	}
	r.procMu.Unlock()
	return nil
}

// shutdown aborts running builds (SIGTERM to their process groups, SIGKILL
// after StopGrace), then stops the daemon. Errors are returned, not hidden.
func (r *HotloadRunner) shutdown() error {
	errBuild := r.scope.abort(r.stopGrace() + r.killWait() + time.Second)
	if errBuild != nil {
		log.Printf("❌ Failed to abort build: %v", errBuild)
	}
	return errors.Join(errBuild, r.stopDaemon())
}

// forceKill kills the daemon's and the running builds' process groups at once.
func (r *HotloadRunner) forceKill() {
	r.scope.forceKill()
	if p := r.currentDaemon(); p != nil {
		_ = p.kill(r.killWait())
	}
	_ = r.scope.abort(r.killWait())
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
