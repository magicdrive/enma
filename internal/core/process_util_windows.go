//go:build windows
// +build windows

package core

import (
	"os/exec"
	"syscall"
)

// Windows has no process groups here: SysProcAttr.Setpgid does not exist.
func setProcessGroup(cmd *exec.Cmd) {}

// signalGroup can only terminate the process itself on Windows.
func signalGroup(cmd *exec.Cmd, pid int, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// groupAlive cannot see descendants on Windows; the leader is tracked by Wait.
func groupAlive(pid int) bool { return false }
