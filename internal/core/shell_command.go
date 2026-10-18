package core

import (
	"os/exec"

	"github.com/magicdrive/enma/internal/shellcmd"
)

// ShellArgv0 is the $0 seen by commands run through ShellCommandArgs.
const ShellArgv0 = shellcmd.Argv0

// ShellPathVar is the shell variable that holds the changed file's path inside
// a command run through ShellCommandArgs (see shellcmd.PathVar).
const ShellPathVar = shellcmd.PathVar

// ShellCommandArgs returns the arguments for `sh` that run command with every
// placeholder bound to path; the path is passed as $1, never spliced into the
// shell source (see shellcmd.Args).
func ShellCommandArgs(command, placeholder, path string) []string {
	return shellcmd.Args(command, placeholder, path)
}

// ShellScript rewrites command so that each placeholder refers to the path
// variable instead of containing the path (see shellcmd.Script).
func ShellScript(command, placeholder string) string {
	return shellcmd.Script(command, placeholder)
}

// envShell moves the script and the parameters of a `sh -c` command into its
// environment, after the environment it would have had (later entries win, so
// enma's values replace any of the same name), leaving a fixed launcher in
// argv (shellcmd.EnvArgs). adaptShell uses it on Windows; it has no build tag
// so that every platform's tests check it.
func envShell(cmd *exec.Cmd) {
	if len(cmd.Args) < 2 {
		return
	}
	args, env := shellcmd.EnvArgs(cmd.Args[1:])
	if env == nil {
		return
	}
	cmd.Args = append([]string{cmd.Args[0]}, args...)
	cmd.Env = append(cmd.Environ(), env...)
}
