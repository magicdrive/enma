package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/magicdrive/enma/internal/common"
)

// startAtOnce runs the start-at-once command of a runner at most once in the
// runner's lifetime. Start calls it before the watcher exists and before the
// first build, so file events, retries and daemon restarts never reach it.
type startAtOnce struct {
	once sync.Once
	err  error
}

func (s *startAtOnce) run(fn func() error) error {
	s.once.Do(func() { s.err = fn() })
	return s.err
}

// startAtOnceInStart runs the start-at-once step of Start: on failure the pid
// file Start created is removed (Start returns without a shutdown); when a
// signal interrupted it, the signal handler owns the exit and the pid file.
// The error is not logged here: the caller prints it once, to stderr.
func startAtOnceInStart(run func() error, pidPath string) error {
	err := run()
	if err == nil || errors.Is(err, errShuttingDown) {
		return err
	}
	if pidPath != "" {
		if derr := common.DeletePidFile(pidPath); derr != nil {
			log.Printf("failed delete pidfile.: %s\n", derr.Error())
		}
	}
	return err
}

// StartAtOnceArgs returns the arguments for `sh` that run the start-at-once
// command as written: no placeholder is bound (there is no changed file), so a
// placeholder in it is plain text for the shell.
func StartAtOnceArgs(command string) []string {
	return []string{"-c", command, ShellArgv0}
}

// runStartAtOnceCommand runs command synchronously in the current (working)
// directory through the runner's command machinery: its own process group,
// stopped with the scope on shutdown (SIGTERM, grace, SIGKILL; at once on a
// force kill). It has no timeout, retry or delay. Group members it leaves
// behind are stopped before it returns, so nothing outlives it.
// An empty command does nothing.
func runStartAtOnceCommand(command string, execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd,
	scope *commandScope, grace, killWait time.Duration) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	log.Printf("🏁 Run start-at-once: %s", command)
	if execCommand == nil {
		execCommand = exec.CommandContext
	}
	cmd := execCommand(scope.context(), "sh", StartAtOnceArgs(command)...)
	killGroupOnCancel(cmd, scope, grace, killWait)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	release, err := scope.enter()
	if err != nil {
		return err
	}
	runErr := cmd.Run()
	var leftErr error
	if cmd.Process != nil {
		leftErr = stopLeftovers(cmd, cmd.Process.Pid, scope, grace, killWait)
	}
	release()
	if scope.context().Err() != nil {
		return errShuttingDown
	}
	if runErr != nil {
		return fmt.Errorf("start-at-once failed: %w", runErr)
	}
	if leftErr != nil {
		return fmt.Errorf("start-at-once left processes behind: %w", leftErr)
	}
	log.Println("✅  start-at-once success")
	return nil
}

// stopLeftovers stops what is left of the process group led by pid after its
// leader has been reaped: SIGTERM, grace, then SIGKILL (at once on a force kill).
func stopLeftovers(cmd *exec.Cmd, pid int, scope *commandScope, grace, killWait time.Duration) error {
	if !groupAlive(pid) {
		return nil
	}
	log.Println("⚠️  start-at-once left processes behind; stopping them")
	_ = signalGroup(cmd, pid, syscall.SIGTERM)
	if waitGroupGone(pid, grace, scope.killed()) {
		return nil
	}
	_ = signalGroup(cmd, pid, syscall.SIGKILL)
	if waitGroupGone(pid, killWait, nil) {
		return nil
	}
	return fmt.Errorf("pgid %d: %w", pid, ErrStopTimeout)
}

// waitGroupGone polls until the group led by pid is empty, d passes or stop is closed.
func waitGroupGone(pid int, d time.Duration, stop <-chan struct{}) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(reapPollInterval)
	defer tick.Stop()
	for {
		if !groupAlive(pid) {
			return true
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			return !groupAlive(pid)
		case <-stop:
			return !groupAlive(pid)
		}
	}
}
