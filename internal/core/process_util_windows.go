//go:build windows
// +build windows

package core

import (
	"os/exec"

	"github.com/magicdrive/enma/internal/model"
)

func setProcessGroup(cmd *exec.Cmd) {
	// SysProcAttr.Setpgid is not available on Windows so it does nothing.
}

func stopDaemonProcess(cmd *exec.Cmd, signalName model.SignalName) {
	stopProcess(cmd)
}

func stopProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
