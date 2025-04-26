//go:build !windows
// +build !windows

package core

import (
	"os/exec"
	"syscall"

	"github.com/magicdrive/enma/internal/model"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func stopDaemonProcess(cmd *exec.Cmd, signalName model.SignalName) {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, signalName.Signal())
	} else {
		_ = cmd.Process.Signal(signalName.Signal())
	}
}

func stopProcess(cmd *exec.Cmd) {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}
