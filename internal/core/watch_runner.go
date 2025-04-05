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

type WatchRunner struct {
	Options       *option.WatchOption
	CmdTimeout    time.Duration
	Delay         time.Duration
	ExecCommand   func(ctx context.Context, name string, args ...string) *exec.Cmd
	watcher       *fsnotify.Watcher
	currentCmd    *exec.Cmd
	mu            sync.Mutex
	debounceTimer *time.Timer
}

func NewWatchRunner(opt *option.WatchOption) *WatchRunner {
	timeout, _ := opt.Timeout.TimeDuration()
	delay, _ := opt.Delay.TimeDuration()
	return &WatchRunner{
		Options:     opt,
		CmdTimeout:  timeout,
		Delay:       delay,
		ExecCommand: exec.CommandContext,
	}
}

func (r *WatchRunner) ReplacePlaceholders(command, path string) string {
	return common.ReplacePlaceholders(command, r.Options.Placeholder, path)
}

func (r *WatchRunner) AddWatchRecursive(path string) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && !r.IsExcludedDir(p) {
			return r.watcher.Add(p)
		}
		return nil
	})
}

func (r *WatchRunner) SetWatcher(watcher *fsnotify.Watcher) {
	r.watcher = watcher
}

func (r *WatchRunner) Start() error {
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
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && !r.IsExcludedDir(path) {
				return watcher.Add(path)
			}
			return nil
		})
		if err != nil {
			return err
		}
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
			info, err := os.Stat(_event.Name)
			if err == nil && info.IsDir() && !r.IsExcludedDir(_event.Name) {
				_ = r.AddWatchRecursive(_event.Name)
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

func (r *WatchRunner) IsExcludedDir(path string) bool {
	for _, d := range r.Options.ExcludeDirList {
		if d != "" && strings.Contains(path, d) {
			return true
		}
	}
	return false
}

func (r *WatchRunner) ShouldTrigger(event fsnotify.Event) bool {
	path := event.Name
	absPath, _ := filepath.Abs(path)

	if event.Op == fsnotify.Chmod {
		return false
	}

	if r.Options.EnmaIgnore != nil {
		if r.Options.EnmaIgnore.Matches(common.TrimDotSlash(path)) {
			return false
		}
	}

	if r.Options.ExcludeDir != "" && r.IsExcludedDir(path) {
		return false
	}

	if r.Options.IgnoreDirRegexp != nil {
		dir := filepath.Dir(absPath)
		result, err := r.Options.IgnoreDirRegexp.MatchString(dir)
		if err != nil {
			log.Fatalf("Faital error: %s", err)
		} else {
			return !result
		}
	}

	if r.Options.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result, err := r.Options.PatternRegexp.MatchString(baseName)
		if err != nil {
			log.Fatalf("Faital error: %s", err)
		} else {
			return result
		}
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

	if r.Options.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		for _, excl := range r.Options.ExcludeExtList {
			if ext == excl {
				return false
			}
		}
	}

	if r.Options.IgnoreFileRegexp != nil {
		baseName := filepath.Base(absPath)
		result, err := r.Options.IgnoreFileRegexp.MatchString(baseName)
		if err != nil {
			log.Fatalf("Faital error: %s", err)
		} else {
			return !result
		}
	}

	return true
}

func (r *WatchRunner) handleChange(event fsnotify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := common.ToAbsolutePath(event.Name)

	for i := 0; i <= r.Options.Retry; i++ {
		if r.RunBuildSequence(i, path) {
			log.Println("✅  Command Action success")
			time.Sleep(r.Delay)
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func (r *WatchRunner) RunBuildSequence(attempt int, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.CmdTimeout)
	defer cancel()

	steps := []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"PreCmd", r.RunPreCmd},
		{"Cmd", r.RunCmd},
		{"PostCmd", r.RunPostCmd},
	}

	for _, step := range steps {
		if err := step.fn(ctx, path); err != nil {
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
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PreCmd, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *WatchRunner) RunCmd(ctx context.Context, path string) error {
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.Cmd, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *WatchRunner) RunPostCmd(ctx context.Context, path string) error {
	if r.Options.PostCmd == "" {
		return nil
	}
	cmd := r.ExecCommand(ctx, "sh", "-c", r.ReplacePlaceholders(r.Options.PostCmd, path))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *WatchRunner) stopCurrentCmd() {
	if r.currentCmd != nil && r.currentCmd.Process != nil {
		log.Println("🛑 Aborting command...")
		if runtime.GOOS == "windows" {
			_ = r.currentCmd.Process.Kill()
		} else {
			_ = r.currentCmd.Process.Signal(syscall.SIGTERM)
		}
		_ = r.currentCmd.Wait()
		r.currentCmd = nil
	}
}
