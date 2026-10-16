package core

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultStopGrace is how long a stop signal may take before the process
	// group is killed.
	DefaultStopGrace = 5 * time.Second
	// DefaultKillWait bounds the wait for reaping after SIGKILL.
	DefaultKillWait = 2 * time.Second

	reapPollInterval   = 20 * time.Millisecond
	groupWatchInterval = 100 * time.Millisecond
)

// ErrStopTimeout reports a process (group) that could not be confirmed gone.
var ErrStopTimeout = errors.New("process did not exit after SIGKILL")

// managedProcess owns one started command. Exactly one goroutine calls Wait;
// everyone else waits on done. All methods are safe for concurrent use.
type managedProcess struct {
	cmd  *exec.Cmd
	pid  int // also the process group id (setProcessGroup)
	done chan struct{}
	err  error // Wait's result; read only after done is closed
	// groupGone records that the group was seen empty after the leader was
	// reaped. From then on the pid may be reused by an unrelated process, so
	// the group is never signalled again.
	groupGone atomic.Bool
}

// startManaged starts cmd in its own process group and reaps it in the background.
func startManaged(cmd *exec.Cmd) (*managedProcess, error) {
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &managedProcess{cmd: cmd, pid: cmd.Process.Pid, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		if !groupAlive(p.pid) {
			p.groupGone.Store(true)
		}
		close(p.done)
		// Members may outlive the leader. Keep watching until the group is empty
		// and latch it then, so a pid reused later is never taken for our group.
		for !p.groupGone.Load() {
			time.Sleep(groupWatchInterval)
			if !groupAlive(p.pid) {
				p.groupGone.Store(true)
			}
		}
	}()
	return p, nil
}

func (p *managedProcess) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// gone reports whether the leader has been reaped and no member of its process
// group is left.
func (p *managedProcess) gone() bool {
	if !p.exited() {
		return false
	}
	if p.groupGone.Load() {
		return true
	}
	if !groupAlive(p.pid) {
		p.groupGone.Store(true)
		return true
	}
	return false
}

// signal sends sig to the process group unless the group is known to be gone.
func (p *managedProcess) signal(sig syscall.Signal) {
	if !p.groupGone.Load() {
		_ = signalGroup(p.cmd, p.pid, sig)
	}
}

func (p *managedProcess) waitGone(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if p.gone() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-p.done:
			time.Sleep(reapPollInterval) // leader gone; poll the rest of the group
		case <-time.After(reapPollInterval):
		}
	}
}

// stop sends sig to the process group, waits up to grace for the whole group to
// go away, then kills the group and waits up to killWait. It returns nil only
// when the leader has been reaped and the group is empty.
func (p *managedProcess) stop(sig syscall.Signal, grace, killWait time.Duration) error {
	if p.gone() {
		return nil
	}
	p.signal(sig)
	if p.waitGone(grace) {
		return nil
	}
	return p.kill(killWait)
}

// kill sends SIGKILL to the process group and waits up to killWait.
func (p *managedProcess) kill(killWait time.Duration) error {
	if p.gone() {
		return nil
	}
	p.signal(syscall.SIGKILL)
	if p.waitGone(killWait) {
		return nil
	}
	return fmt.Errorf("pid %d: %w", p.pid, ErrStopTimeout)
}

// killGroupOnCancel runs cmd in its own process group and decides how a
// cancelled cmd is stopped. On a timeout (or once s is force-killed) the whole
// group is killed at once. On shutdown the group gets SIGTERM and grace to exit
// first, like the daemon; it is killed when grace runs out or on a force kill.
func killGroupOnCancel(cmd *exec.Cmd, s *commandScope, grace, killWait time.Duration) {
	setProcessGroup(cmd)
	if cmd.Cancel == nil { // not created by exec.CommandContext
		return
	}
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		if s.context().Err() == nil || s.killing() { // timeout, or force kill
			return signalGroup(cmd, pid, syscall.SIGKILL)
		}
		err := signalGroup(cmd, pid, syscall.SIGTERM)
		go escalateAfter(cmd, pid, grace, s.killed())
		return err
	}
	// Run must not give up on the leader (and kill only it) before the group
	// has had its grace period.
	cmd.WaitDelay = grace + killWait
}

// escalateAfter kills the process group led by pid if it outlives grace, or at
// once when killNow is closed. It stops as soon as the group is empty, so a pid
// reused afterwards is not signalled.
func escalateAfter(cmd *exec.Cmd, pid int, grace time.Duration, killNow <-chan struct{}) {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(groupWatchInterval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if !groupAlive(pid) {
				return
			}
			continue
		case <-deadline.C:
		case <-killNow:
		}
		if groupAlive(pid) {
			_ = signalGroup(cmd, pid, syscall.SIGKILL)
		}
		return
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// commandScope ties command runs to a runner's lifetime: abort cancels every
// running and future command (stopping their process groups through
// killGroupOnCancel) and waits until the running ones have returned; forceKill
// makes that stop a kill without grace.
type commandScope struct {
	once     sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	killNow  chan struct{}
	killOnce sync.Once
	running  sync.RWMutex // read-held while a command runs
	ended    chan struct{}
	endOnce  sync.Once
}

func (s *commandScope) context() context.Context {
	s.once.Do(func() {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.killNow = make(chan struct{})
	})
	return s.ctx
}

// killed is closed by forceKill.
func (s *commandScope) killed() <-chan struct{} {
	s.context()
	return s.killNow
}

func (s *commandScope) killing() bool {
	select {
	case <-s.killed():
		return true
	default:
		return false
	}
}

// forceKill cancels the scope so that running and future commands are killed
// at once, cutting any grace period already started. It does not wait.
func (s *commandScope) forceKill() {
	s.context()
	s.killOnce.Do(func() { close(s.killNow) }) // before cancel: Cancel sees it
	s.cancel()
}

// enter marks a command as running until the returned func is called. Once
// abort has begun it fails at once instead of blocking: no command starts after
// shutdown and no caller is left waiting on the lock.
func (s *commandScope) enter() (func(), error) {
	if s.context().Err() != nil {
		return nil, errShuttingDown
	}
	if !s.running.TryRLock() { // only abort ever takes the write lock
		return nil, errShuttingDown
	}
	return s.running.RUnlock, nil
}

// abort cancels the scope and waits up to d for running commands to return.
// It is idempotent and safe to call concurrently.
func (s *commandScope) abort(d time.Duration) error {
	s.context()
	s.cancel()
	s.endOnce.Do(func() {
		s.ended = make(chan struct{})
		go func() {
			s.running.Lock() // never released: no command may run after abort
			close(s.ended)
		}()
	})
	select {
	case <-s.ended:
		return nil
	case <-time.After(d):
		return errors.New("running commands did not end in time")
	}
}

// sleepCtx sleeps for d unless ctx ends first; it reports whether ctx is still live.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var (
	errShuttingDown  = errors.New("shutting down")
	errDaemonRunning = errors.New("the previous daemon is still running")
)
