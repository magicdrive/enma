package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
)

type WatchRunner struct {
	Options     *option.WatchOption
	CmdTimeout  time.Duration
	Delay       time.Duration
	ExecCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
	// StopGrace is how long a running command may take to exit after SIGTERM
	// on shutdown before its process group is killed; KillWait bounds the wait
	// after SIGKILL. Zero means DefaultStopGrace / DefaultKillWait.
	StopGrace     time.Duration
	KillWait      time.Duration
	watcher       *fsnotify.Watcher
	scope         commandScope
	paths         watchScope // the project and watch directories for the watch rules
	startOnce     startAtOnce
	mu            sync.Mutex
	debounceTimer *time.Timer
	fileHashes    sync.Map
}

type pendingChangeWatch struct {
	path string
	time time.Time
}

var (
	pendingWatch   = make(map[string]pendingChangeWatch)
	pendingMuWatch sync.Mutex
)

func NewWatchRunner(opt *option.WatchOption) *WatchRunner {
	timeout, _ := opt.Timeout.TimeDuration()
	delay, _ := opt.Delay.TimeDuration()
	return &WatchRunner{
		Options:     opt,
		CmdTimeout:  timeout,
		Delay:       delay,
		ExecCommand: exec.CommandContext,
		StopGrace:   DefaultStopGrace,
		KillWait:    DefaultKillWait,
	}
}

func (r *WatchRunner) stopGrace() time.Duration { return orDefault(r.StopGrace, DefaultStopGrace) }
func (r *WatchRunner) killWait() time.Duration  { return orDefault(r.KillWait, DefaultKillWait) }

// runStartAtOnce runs the start-at-once command; only the first call does anything.
func (r *WatchRunner) runStartAtOnce() error {
	return r.startOnce.run(func() error {
		return runStartAtOnceCommand(r.Options.StartAtOnce, r.ExecCommand, &r.scope, r.stopGrace(), r.killWait())
	})
}

// ReplacePlaceholders returns command with the placeholder replaced by path as plain text.
// It is for display only: commands are run through ShellCommandArgs, which never
// splices the path into the shell source.
func (r *WatchRunner) ReplacePlaceholders(command, path string) string {
	return common.ReplacePlaceholders(command, r.Options.Placeholder, path)
}

func (r *WatchRunner) AddWatchDirs(dirs []string) {
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

func (r *WatchRunner) AddWatchRecursive(path string) error {
	dirs, err := r.CollectWatchDirs(path)
	if err != nil {
		return err
	}
	r.AddWatchDirs(dirs)
	return nil
}

func (r *WatchRunner) SetWatcher(watcher *fsnotify.Watcher) {
	r.watcher = watcher
}

// CollectWatchDirs returns root and the directories below it to watch (see collectWatchDirs).
func (r *WatchRunner) CollectWatchDirs(root string) ([]string, error) {
	r.paths.init(r.Options.WatchDirList)
	// directories rejected by --exclude-dir or .enmaignore are not descended into
	return collectWatchDirs(root, func(dir string) bool { return r.dirExcluded(dir) || r.ignored(dir) })
}

func (r *WatchRunner) Start() (err error) {
	env, err := openStartEnv(r.Options.PidPathOpt, r.Options.LogPathOpt)
	if err != nil {
		return err
	}
	defer env.failed(&err)

	fmt.Println(common.StartMessage())
	fmt.Printf("Start Watch mode.\n\n\n")

	env.handleSignals("🛑 Stopping file monitor daemon...", r.shutdown, r.forceKill)

	// start-at-once runs before the watcher exists: file changes made meanwhile
	// are not seen, monitoring starts after it.
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

	log.Println("🚀 Start file monitor daemon")

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					info, err := os.Lstat(event.Name)
					if err == nil {
						if info.Mode()&os.ModeSymlink != 0 {
							if resolved, ok := r.paths.symlinkedDirToFollow(event.Name); ok && !r.dirExcluded(resolved) {
								_ = r.AddWatchRecursive(resolved)
								log.Printf("👀 Watching symlinked dir: %s", resolved)
							}
						} else if info.IsDir() && r.ShouldWatchDir(event.Name) {
							_ = r.AddWatchRecursive(event.Name)
							log.Printf("👀 Watching new dir: %s", event.Name)
							r.enqueueFilesIn(event.Name)
						}
					}
				}

				r.enqueue(event)
			case err := <-watcher.Errors:
				log.Println("Watcher error:", err)
			}
		}
	}()

	select {}

}

func (r *WatchRunner) handleChangeDirect(pathStyled string) {
	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, pathStyled) {
			log.Println("✅  Command Action success")
			sleepCtx(r.scope.context(), r.Delay)
			return
		}
		if !sleepCtx(r.scope.context(), 1*time.Second) {
			return
		}
	}
}

func (r *WatchRunner) hasChanged(path string) bool {
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

func (r *WatchRunner) computeHash(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(data)
	return h[:], nil
}

// IsExcludedDir reports whether a directory above path (a file or directory),
// within the project, is named in --exclude-dir.
func (r *WatchRunner) IsExcludedDir(path string) bool {
	r.paths.init(r.Options.WatchDirList)
	return hasExcludedElement(r.paths.rel(path), r.Options.ExcludeDirList, false)
}

// dirExcluded reports whether the directory dir or one above it, within the
// project, is named in --exclude-dir.
func (r *WatchRunner) dirExcluded(dir string) bool {
	r.paths.init(r.Options.WatchDirList)
	return hasExcludedElement(r.paths.rel(dir), r.Options.ExcludeDirList, true)
}

// ignored reports whether .enmaignore rules (on the project-relative path) reject path.
func (r *WatchRunner) ignored(path string) bool {
	if r.Options.EnmaIgnore == nil {
		return false
	}
	r.paths.init(r.Options.WatchDirList)
	rel := r.paths.rel(path)
	return rel != "." && r.Options.EnmaIgnore.Matches(rel)
}

func (r *WatchRunner) ShouldWatchDir(dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	// --exclude-dir
	if r.Options.ExcludeDir != "" && r.dirExcluded(absDir) {
		return false
	}

	// --ignore-dir-regex (on the absolute path, as documented)
	if r.Options.IgnoreDirRegexp != nil {
		if r.Options.IgnoreDirRegexp.MatchString(absDir) {
			return false
		}
	}

	// .enmaignore
	if r.ignored(absDir) {
		return false
	}

	return true
}

func (r *WatchRunner) ShouldTrigger(event fsnotify.Event) bool {
	path := event.Name
	absPath, _ := filepath.Abs(path)

	if event.Op == fsnotify.Chmod {
		return false
	}

	if r.Options.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result := r.Options.PatternRegexp.MatchString(baseName)
		if !result {
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
		if result {
			return false
		}
	}

	if r.Options.IgnoreFileRegexp != nil {
		baseName := filepath.Base(absPath)
		result := r.Options.IgnoreFileRegexp.MatchString(baseName)
		if result {
			return false
		}
	}

	if r.ignored(path) {
		return false
	}

	if r.Options.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		if slices.Contains(r.Options.ExcludeExtList, ext) {
			return false
		}
	}

	return true
}

func (r *WatchRunner) RunBuildSequence(attempt int, path string) bool {
	steps := []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"PreCmd", r.RunPreCmd},
		{"Cmd", r.RunCmd},
		{"PostCmd", r.RunPostCmd},
	}

	for _, step := range steps {
		ctx, cancel := context.WithTimeout(r.scope.context(), r.CmdTimeout)
		err := timeoutError(step.fn(ctx, path), r.CmdTimeout)
		cancel()
		if err != nil {
			log.Printf("❌  %s failed (attempt %d): %v", step.name, attempt+1, err)
			return false
		}
	}

	return true
}

func (r *WatchRunner) RunPreCmd(ctx context.Context, path string) error {
	if r.Options.PreCmd == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.PreCmd, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runInScope(cmd, &r.scope, r.stopGrace(), r.killWait(), "pre-cmd")
}

func (r *WatchRunner) RunCmd(ctx context.Context, path string) error {
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.Cmd, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runInScope(cmd, &r.scope, r.stopGrace(), r.killWait(), "cmd")
}

func (r *WatchRunner) RunPostCmd(ctx context.Context, path string) error {
	if r.Options.PostCmd == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", ShellCommandArgs(r.Options.PostCmd, r.Options.Placeholder, path)...)
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runInScope(cmd, &r.scope, r.stopGrace(), r.killWait(), "post-cmd")
}

// enqueue records a change that passes the watch rules and (re)starts the
// debounce timer. It is called from the watcher goroutine only.
func (r *WatchRunner) enqueue(event fsnotify.Event) {
	if !r.ShouldTrigger(event) {
		return
	}
	absPath := filepath.Clean(event.Name)
	pendingMuWatch.Lock()
	pendingWatch[absPath] = pendingChangeWatch{path: absPath, time: time.Now()}
	pendingMuWatch.Unlock()

	if r.debounceTimer != nil {
		r.debounceTimer.Stop()
	}
	r.debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
		pendingMuWatch.Lock()
		for _, change := range pendingWatch {
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
		pendingWatch = make(map[string]pendingChangeWatch)
		pendingMuWatch.Unlock()
	})
}

// enqueueFilesIn queues the files already in a directory that was just added to
// the watcher: they may have been written before its watch existed, and their
// events are then lost (the debounce merges any that did arrive).
func (r *WatchRunner) enqueueFilesIn(dir string) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if !r.ShouldWatchDir(path) { // the same rules as for watching (A4R2-01)
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			r.enqueue(fsnotify.Event{Name: path, Op: fsnotify.Create})
		}
		return nil
	})
}

// shutdown aborts running commands: SIGTERM to their process groups, SIGKILL
// after StopGrace.
func (r *WatchRunner) shutdown() error {
	if err := r.scope.abort(r.stopGrace() + r.killWait() + time.Second); err != nil {
		log.Printf("❌ Failed to abort command: %v", err)
		return err
	}
	return nil
}

// forceKill kills the running commands' process groups at once.
func (r *WatchRunner) forceKill() {
	r.scope.forceKill()
	_ = r.scope.abort(r.killWait())
}

func (r *WatchRunner) applyArgsPathStyle(path string) string {
	var target = common.ToAbsolutePath(path)
	if !r.Options.AbsolutePathFlag.Bool() {
		target = common.ToRelativePath(target)
	}
	if r.Options.ArgsPathStyleString != "" {
		return r.Options.ArgsPathStyle.ArgsPathString(target)
	}
	return target
}
