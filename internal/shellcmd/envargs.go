package shellcmd

import (
	"fmt"
	"strings"
)

// Environment variables that carry a command when EnvArgs is used: the script,
// and the positional parameters as ArgEnvPrefix1, ArgEnvPrefix2, ...
const (
	ScriptEnv    = "__ENMA_SCRIPT"
	ArgEnvPrefix = "__ENMA_ARG"
)

// launcher is the only script EnvArgs leaves in argv. It holds no character a
// command-line parser treats as quoting, escaping or a pattern (no quotes,
// backslashes, braces, brackets, *, ?, and it does not start with ~ or @), so
// it reaches sh unchanged. With IFS empty and globbing off, the unquoted
// expansion is one word: it is neither split nor matched against files.
const launcher = "IFS=; set -f; eval $" + ScriptEnv

// EnvArgs turns arguments for `sh` (-c script [argv0 [params...]]) into
// arguments holding only a fixed launcher and argv0, and environment entries
// carrying the script and the parameters. The command then runs as with the
// original arguments: same script, $0, $1..., $#, IFS and globbing. Under eval,
// shells word some messages differently, and bash exits with 1 instead of 2 on
// a syntax error.
//
// enma uses it on Windows. There Go joins argv into one command line and an
// MSYS2/Cygwin sh (Git for Windows) splits it again with its own rules: quotes,
// braces and glob patterns are interpreted, so a path like `a{b}` or `it's`
// would reach the command changed. Environment values are passed as they are.
// Arguments of another form, or already turned (the launcher), are returned
// unchanged, with no environment.
func EnvArgs(args []string) ([]string, []string) {
	if len(args) < 2 || args[0] != "-c" || args[1] == launcher {
		return args, nil
	}
	script, rest := args[1], args[2:] // rest: argv0 and the parameters, if any
	var params []string
	if len(rest) > 0 {
		params = rest[1:]
	}
	var env []string
	var b strings.Builder
	b.WriteString("set --")
	for i, p := range params {
		name := fmt.Sprintf("%s%d", ArgEnvPrefix, i+1)
		b.WriteString(` "$` + name + `"`)
		env = append(env, name+"="+p)
	}
	b.WriteString("; unset " + ScriptEnv) // the command's children do not inherit them
	for i := range params {
		fmt.Fprintf(&b, " %s%d", ArgEnvPrefix, i+1)
	}
	// back to the defaults of sh -c, on the script's first line (no newline:
	// line numbers in messages stay those of the script)
	b.WriteString("; IFS=$(printf ' \\t\\n.'); IFS=${IFS%.}; set +f; ")
	b.WriteString(script)
	env = append(env, ScriptEnv+"="+b.String())
	return append([]string{"-c", launcher}, rest[:min(len(rest), 1)]...), env
}
