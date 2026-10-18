//go:build !windows

package core

import "os/exec"

// adaptShell prepares a `sh -c` command for the platform: on Unix argv reaches
// sh as it is, so nothing changes.
func adaptShell(cmd *exec.Cmd) {}
