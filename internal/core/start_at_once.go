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
	"sync/atomic"
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
	return plainShellArgs(command)
}

// plainShellArgs returns the arguments for `sh` that run command as written,
// with no positional parameters (start-at-once, scheduler jobs).
func plainShellArgs(command string) []string {
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
	log.Println("🏁 Run start-at-once") // the command text is not logged: it may hold secrets
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
		leftErr = stopLeftovers(cmd, cmd.Process.Pid, scope, grace, killWait, "start-at-once")
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
// what names the command in the log line.
func stopLeftovers(cmd *exec.Cmd, pid int, scope *commandScope, grace, killWait time.Duration, what string) error {
	if !groupAlive(pid) {
		return nil
	}
	log.Printf("⚠️  %s left processes behind; stopping them", what)
	// During shutdown a leader killed by SIGKILL normally got it as a member of
	// its group (the escalation after the grace period, a force kill): the rest
	// had its SIGTERM and grace already. Then only SIGKILL and the reaping are
	// left — no second grace period (it would stretch shutdown). Outside shutdown
	// the leftovers always get SIGTERM and the grace period first: the leader may
	// have been killed alone (kill -9, OOM).
	if !(scope.context().Err() != nil && killedBySIGKILL(cmd)) {
		_ = signalGroup(cmd, pid, syscall.SIGTERM)
		if waitGroupGone(pid, grace, scope.killed()) {
			return nil
		}
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

// runInScope runs cmd as a command of scope (it fails at once after shutdown
// began) and then stops what the command left behind in its process group:
// SIGTERM, grace, SIGKILL. what names the command in log lines and errors.
func runInScope(cmd *exec.Cmd, scope *commandScope, grace, killWait time.Duration, what string) error {
	release, err := scope.enter()
	if err != nil {
		return err
	}
	defer release()
	// The command's own deadline (--timeout) cancels it only while it runs;
	// stopping what it left behind afterwards does not count (H6A2-01).
	var deadlineHit atomic.Bool
	if cancel := cmd.Cancel; cancel != nil {
		cmd.Cancel = func() error {
			if scope.context().Err() == nil { // not a shutdown: the step's deadline
				deadlineHit.Store(true)
			}
			return cancel()
		}
	}
	runErr := cmd.Run()
	// (a deadline already over at start, e.g. --timeout 0sec, fails Start
	// with the context's error without calling Cancel)
	expiredAtStart := errors.Is(runErr, context.DeadlineExceeded) && scope.context().Err() == nil
	if runErr != nil && (deadlineHit.Load() || expiredAtStart) {
		runErr = &stepTimeoutError{err: runErr}
	}
	var leftErr error
	// During shutdown a command that ended on SIGKILL was stopped with its whole
	// group (SIGTERM, grace, SIGKILL): nothing of it is left to stop, and the
	// shutdown, which waits for this command, gets no extra work.
	shutdownKilled := scope.context().Err() != nil && killedBySIGKILL(cmd)
	switch {
	case cmd.Process == nil:
	case shutdownKilled:
		// should the leader alone have been killed (kill -9, OOM), what is left gets
		// SIGKILL too, without a wait (A4R2-02)
		_ = signalGroup(cmd, cmd.Process.Pid, syscall.SIGKILL)
	default:
		leftErr = stopLeftovers(cmd, cmd.Process.Pid, scope, grace, killWait, what)
	}
	if runErr != nil {
		return runErr
	}
	if leftErr != nil {
		return fmt.Errorf("%s left processes behind: %w", what, leftErr)
	}
	return nil
}

// stepTimeoutError marks a command that its step's deadline stopped while it
// was running.
type stepTimeoutError struct{ err error }

func (e *stepTimeoutError) Error() string { return e.err.Error() }
func (e *stepTimeoutError) Unwrap() error { return e.err }

// timeoutError names the --timeout when a step failed because it ran past it
// (its own deadline, not a shutdown), so that "signal: killed" is explained.
func timeoutError(err error, timeout time.Duration) error {
	var t *stepTimeoutError
	if errors.As(err, &t) {
		return fmt.Errorf("timed out after %s (--timeout): %w", timeout, err)
	}
	return err
}

// killedBySIGKILL reports whether cmd's process ended on SIGKILL.
func killedBySIGKILL(cmd *exec.Cmd) bool {
	if cmd.ProcessState == nil {
		return false
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL
}
