//go:build !windows
// +build !windows

package core

import (
	"errors"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// errNotAChildGroup refuses a signal that would reach a group enma does not own.
var errNotAChildGroup = errors.New("refusing to signal a process group enma did not start")

// signalGroup sends sig to the process group led by pid (the group id equals
// the leader's pid because of setProcessGroup). kill(-pid) with pid 0 or 1
// would reach enma's own group or every process, and enma's own group is never
// a command's: those are refused.
func signalGroup(cmd *exec.Cmd, pid int, sig syscall.Signal) error {
	if pid <= 1 || pid == syscall.Getpgrp() {
		return errNotAChildGroup
	}
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil // nobody left
	}
	return err
}

// groupAlive reports whether any process of the group still exists.
func groupAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// groupAliveAfterLeader is groupAlive for a group whose leader has already been
// reaped (by its exec.Cmd): it first collects the zombies of the group that are
// enma's own children. When enma runs as pid 1 (a container without an init)
// or as a subreaper, orphaned members of a command's group are re-parented to
// enma and stay zombies once they exit, and kill(-pgid, 0) still finds a group
// with zombies: it would never look gone. Never call it while the leader may
// still be running: wait4 would take the leader's status away from cmd.Wait.
func groupAliveAfterLeader(pid int) bool {
	reapGroup(pid)
	return groupAlive(pid)
}

// reapGroup collects the exited children of enma in process group pgid
// (WNOHANG: it never waits for a running one).
func reapGroup(pgid int) {
	if pgid <= 1 {
		return
	}
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-pgid, &ws, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || pid <= 0 {
			return
		}
	}
}
