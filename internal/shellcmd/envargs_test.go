package shellcmd_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/shellcmd"
)

// W4/W5: on Windows, Go joins argv into one command line and the MSYS2/Cygwin
// sh splits it again, interpreting quotes, braces and patterns. EnvArgs leaves
// only fixed text in argv: no path, no script, nothing those rules change.
func TestEnvArgs_ArgvHoldsOnlyTheLauncher(t *testing.T) {
	path := `C:\w\it's {a,b} [x]*?~@"$(id)".go`
	command := `printf '%s' "{}" | sed 's/\\/\//g'`
	args, env := shellcmd.EnvArgs(shellcmd.Args(command, "{}", path))
	if len(args) != 3 || args[0] != "-c" || args[2] != shellcmd.Argv0 {
		t.Fatalf("args %q", args)
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\"'\\{}[]*?()") || strings.HasPrefix(a, "~") || strings.HasPrefix(a, "@") {
			t.Errorf("argument %q holds a character the MSYS2 command-line parser interprets", a)
		}
		if strings.Contains(a, "printf") || strings.Contains(a, "it's") {
			t.Errorf("argument %q carries the command or the path", a)
		}
	}
	var gotPath, gotScript bool
	for _, e := range env {
		gotPath = gotPath || e == shellcmd.ArgEnvPrefix+"1="+path
		gotScript = gotScript || (strings.HasPrefix(e, shellcmd.ScriptEnv+"=") && strings.HasSuffix(e, shellcmd.Script(command, "{}")))
	}
	if !gotPath || !gotScript {
		t.Fatalf("environment %q does not carry the path and the script as they are", env)
	}
}

func TestEnvArgs_OtherFormsUnchanged(t *testing.T) {
	for _, args := range [][]string{nil, {"-c"}, {"-x", "a", "b"}} {
		got, env := shellcmd.EnvArgs(args)
		if env != nil || len(got) != len(args) {
			t.Errorf("EnvArgs(%q) = %q, %q", args, got, env)
		}
	}
}

// The daemon runs as `sh -c <daemon>` (no argv0): it is turned too, with $0
// left to sh (audit M-1).
func TestEnvArgs_WithoutArgv0(t *testing.T) {
	args, env := shellcmd.EnvArgs([]string{"-c", `./server{,.exe} --dir 'C:\\data'`})
	if len(args) != 2 || args[0] != "-c" || strings.Contains(args[1], "server") || len(env) != 1 {
		t.Fatalf("args %q env %q", args, env)
	}
}

// Turning twice would run nothing (the second script would replace the
// first): the launcher is left as it is (audit L-2).
func TestEnvArgs_Idempotent(t *testing.T) {
	once, env := shellcmd.EnvArgs(shellcmd.Args("echo {}", "{}", "a b"))
	twice, env2 := shellcmd.EnvArgs(once)
	if env2 != nil || len(twice) != len(once) || twice[1] != once[1] || len(env) != 2 {
		t.Fatalf("second EnvArgs changed %q into %q with %q", once, twice, env2)
	}
}
