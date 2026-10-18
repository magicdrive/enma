//go:build windows

package core

import "os/exec"

// adaptShell prepares a `sh -c` command for Windows: the script and the path
// are passed in the environment and argv holds a fixed launcher, because the
// command line Go builds is split again by the MSYS2/Cygwin sh with rules that
// change quotes, braces and patterns (see shellcmd.EnvArgs).
func adaptShell(cmd *exec.Cmd) { envShell(cmd) }
