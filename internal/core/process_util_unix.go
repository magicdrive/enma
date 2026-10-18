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
