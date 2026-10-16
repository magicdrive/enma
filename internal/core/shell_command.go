package core

import "github.com/magicdrive/enma/internal/shellcmd"

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
