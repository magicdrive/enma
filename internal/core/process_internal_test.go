//go:build !windows

package core

import (
	"os"
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

// signalGroup never reaches a group enma did not start: pid 0 (kill(0) is the
// caller's own group), 1 (kill(-1) is every process) and enma's own group are
// refused. Probed with signal 0, which delivers nothing even if a guard breaks.
func TestSignalGroup_RefusesForeignGroups(t *testing.T) {
	for _, pid := range []int{-5, 0, 1, syscall.Getpgrp()} {
		if err := signalGroup(nil, pid, syscall.Signal(0)); err != errNotAChildGroup {
			t.Errorf("signalGroup(%d) = %v, want refusal", pid, err)
		}
		if pid <= 1 && groupAlive(pid) {
			t.Errorf("groupAlive(%d) = true", pid)
		}
	}
	child := exec.Command("sleep", "30")
	setProcessGroup(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL); _ = child.Wait() })
	if err := signalGroup(child, child.Process.Pid, syscall.Signal(0)); err != nil {
		t.Fatalf("own child's group refused: %v", err)
	}
}

// During shutdown a leader that ended on SIGKILL (the shutdown's escalation)
// needs no second grace period for what it left behind: its group is killed at
// once instead of waiting for a SIGTERM that is ignored.
func TestStopLeftovers_NoSecondGraceAfterSIGKILL(t *testing.T) {
	var s commandScope
	s.context()
	s.cancel() // shutting down
	cmd := exec.Command("sh", "-c", `(trap '' TERM; exec sleep 30) & sleep 0.2; kill -KILL $$`)
	setProcessGroup(cmd)
	_ = cmd.Run()
	if !killedBySIGKILL(cmd) {
		t.Fatalf("precondition: leader state %v", cmd.ProcessState)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = signalGroup(cmd, pid, syscall.SIGKILL) })
	const grace = 3 * time.Second
	start := time.Now()
	if err := stopLeftovers(cmd, pid, &s, grace, 2*time.Second, "test"); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el >= grace {
		t.Fatalf("waited %v: a second grace period after SIGKILL", el)
	}
	if groupAlive(pid) {
		t.Fatal("the leftover survived")
	}
}

// Outside shutdown a leader killed alone (kill -9, OOM) does not mean its group
// got SIGKILL: what it left behind gets SIGTERM and the grace period first (A4R2-02).
func TestStopLeftovers_LeaderKilledAloneLeftoversGetTerm(t *testing.T) {
	var s commandScope
	mark := t.TempDir() + "/term"
	cmd := exec.Command("sh", "-c", `(trap 'echo term > "`+mark+`"; exit 0' TERM; sleep 30 & wait) & sleep 0.3; kill -KILL $$`)
	setProcessGroup(cmd)
	_ = cmd.Run()
	if !killedBySIGKILL(cmd) {
		t.Fatalf("precondition: leader state %v", cmd.ProcessState)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = signalGroup(cmd, pid, syscall.SIGKILL) })
	if err := stopLeftovers(cmd, pid, &s, 3*time.Second, 2*time.Second, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("the leftover got SIGKILL without SIGTERM first")
	}
}
