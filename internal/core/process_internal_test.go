//go:build !windows

package core

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A daemon that exited on its own leaves a handle behind. Once its group was seen
// empty, its pid may belong to an unrelated process group: stop must not signal it.
func TestManagedProcess_NeverSignalsAReusedPid(t *testing.T) {
	p, err := startManaged(exec.Command("sh", "-c", "exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	<-p.done
	if !p.groupGone.Load() {
		t.Fatal("empty group after reaping was not recorded")
	}

	other := exec.Command("sleep", "30")
	setProcessGroup(other)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); _ = other.Wait() })
	p.pid = other.Process.Pid // simulate the pid being reused by an unrelated group leader

	if err := p.stop(syscall.SIGTERM, 100*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stop of an exited daemon: %v", err)
	}
	if err := syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatalf("an unrelated process group was signalled: %v", err)
	}
}

// enter must fail at once, not block, while abort is waiting for running commands.
// (The context check catches this case first; the TryRLock fallback for the window
// between that check and abort taking the lock has no deterministic hook.)
func TestCommandScope_EnterFailsWhileAbortIsPending(t *testing.T) {
	var s commandScope
	release, err := s.enter() // a running command
	if err != nil {
		t.Fatal(err)
	}
	aborted := make(chan error, 1)
	go func() { aborted <- s.abort(5 * time.Second) }()
	// wait until abort has cancelled and is blocked on the write lock
	deadline := time.Now().Add(2 * time.Second)
	for s.context().Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := s.enter()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a command entered while abort was pending")
		}
	case <-time.After(time.Second):
		t.Fatal("enter blocked while abort was pending")
	}
	release()
	if err := <-aborted; err != nil {
		t.Fatalf("abort: %v", err)
	}
}

// Without the context check, enter must still refuse once abort holds the lock.
func TestCommandScope_TryLockRefusesAfterAbort(t *testing.T) {
	var s commandScope
	if err := s.abort(time.Second); err != nil {
		t.Fatal(err)
	}
	if s.running.TryRLock() {
		t.Fatal("read lock available after abort")
	}
}

// A leader that exits while a member of its group lives on: once that member is
// gone too, the group must be latched as gone before the pid can be reused.
func TestManagedProcess_LatchesGroupEmptiedAfterTheLeader(t *testing.T) {
	p, err := startManaged(exec.Command("sh", "-c", "sleep 0.3 & exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	<-p.done
	if p.groupGone.Load() {
		t.Fatal("group latched as gone while a member was still alive")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !p.groupGone.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.groupGone.Load() {
		t.Fatal("group emptied after the leader was never latched as gone")
	}

	other := exec.Command("sleep", "30")
	setProcessGroup(other)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); _ = other.Wait() })
	p.pid = other.Process.Pid // simulate the reuse
	if err := p.stop(syscall.SIGTERM, 100*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var ws syscall.WaitStatus
	if pid, _ := syscall.Wait4(other.Process.Pid, &ws, syscall.WNOHANG, nil); pid != 0 {
		t.Fatalf("an unrelated process group was signalled (status %v)", ws)
	}
}
