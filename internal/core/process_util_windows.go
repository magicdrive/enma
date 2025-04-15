//go:build windows
// +build windows

package core

import (
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// SysProcAttr.Setpgid is not available on Windows so it does nothing.
}

func stopProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
