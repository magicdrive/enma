//go:build !windows

package shellcmd_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/magicdrive/enma/internal/shellcmd"
)

// The command runs the same through EnvArgs as through argv: output, exit
// status, $0, $#, $@, IFS, globbing, the placeholder's value, and nothing of
// enma's variables left for its children. Checked with every POSIX shell
// found here (Windows runs the same code with the Git for Windows sh).
var envArgsCommands = []string{
	`printf '[%s]' {} "{}" '{}' "x{}y"`,
	`printf '<%s|%s|%s>' "$0" "$#" "$@"`,
	`printf '%s' "$IFS" | od -c | tr -s ' '`,
	`printf '%s' "$-" | tr -d 'ilmsBhH' ; echo`,
	`echo * ; set -- a b; echo $#`,
	`env | grep -c __ENMA_ || true`,
	`f() { printf '%s' "{}"; }; f; shift; printf '%s' "{}"`,
	`exit 7`,
	"printf 'multi\nline %s' {}\n# comment {}",
	`sed 's/\\/\//g' <<EOF
{}
EOF`,
}

var envArgsPaths = []string{"", "plain.go", "/src/$HOME${IFS}x.go", "a'b\"c`d$(e)f\\g", "x{,.bak}.go", "[a].go", "~/a", "@file", "a b\tc\nd", `C:\w\a{b.go`, "*"}

func shells(t testing.TB) []string {
	var out []string
	for _, s := range []string{"sh", "bash", "dash"} { // enma runs "sh"; bash and dash stand for other implementations
		if p, err := exec.LookPath(s); err == nil {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		t.Skip("no POSIX shell")
	}
	return out
}

func runBoth(t testing.TB, sh, dir string, args []string) (direct, viaEnv string, dCode, eCode int) {
	run := func(a, env []string) (string, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, sh, a...)
		cmd.Dir = dir
		cmd.WaitDelay = 2 * time.Second // a process left holding the output does not block the test
		if env != nil {
			cmd.Env = append(cmd.Environ(), env...)
		}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("%s %q (env %q) did not finish: %q", sh, a, env, out.String())
		}
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		return out.String(), code
	}
	direct, dCode = run(args, nil)
	ea, env := shellcmd.EnvArgs(args)
	viaEnv, eCode = run(ea, env)
	return
}

func TestEnvArgs_SameAsArgv(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, sh := range shells(t) {
		for _, c := range envArgsCommands {
			for _, p := range envArgsPaths {
				direct, viaEnv, dc, ec := runBoth(t, sh, dir, shellcmd.Args(c, "{}", p))
				if direct != viaEnv || dc != ec {
					t.Errorf("%s: command %q path %q:\n argv: %q (%d)\n env:  %q (%d)", filepath.Base(sh), c, p, direct, dc, viaEnv, ec)
				}
			}
			// no positional parameter (start-at-once, scheduler), and no argv0 (the daemon)
			for _, args := range [][]string{{"-c", strings.ReplaceAll(c, "{}", "$0"), shellcmd.Argv0}, {"-c", strings.ReplaceAll(c, "{}", "$0")}} {
				direct, viaEnv, dc, ec := runBoth(t, sh, dir, args)
				if direct != viaEnv || dc != ec {
					t.Errorf("%s: %q:\n argv: %q (%d)\n env:  %q (%d)", filepath.Base(sh), args, direct, dc, viaEnv, ec)
				}
			}
		}
	}
}

// With globbing off, the one word the launcher evaluates is never matched
// against file names: a file named like that word (with the script's * turned
// into a command) must not be evaluated instead (audit B-1).
func TestEnvArgs_LauncherDoesNotGlob(t *testing.T) {
	for _, sh := range shells(t) {
		dir := t.TempDir()
		args, env := shellcmd.EnvArgs([]string{"-c", "echo *.go", shellcmd.Argv0})
		script := strings.TrimPrefix(env[len(env)-1], shellcmd.ScriptEnv+"=")
		// the pattern's \X matches X; * matches the injected command
		name := strings.ReplaceAll(strings.ReplaceAll(script, `\`, ""), "*", "x; touch PWNED; : ")
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(sh, args...)
		cmd.Dir, cmd.Env = dir, append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", sh, err, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
			t.Fatalf("%s: the launcher evaluated a file name", filepath.Base(sh))
		}
	}
}

func FuzzEnvArgs(f *testing.F) {
	for _, p := range envArgsPaths {
		f.Add(p, uint8(0))
	}
	f.Fuzz(func(t *testing.T, path string, which uint8) {
		if strings.ContainsRune(path, 0) || !utf8.ValidString(path) {
			return // argv and the environment cannot carry NUL
		}
		c := envArgsCommands[int(which)%len(envArgsCommands)]
		direct, viaEnv, dc, ec := runBoth(t, "sh", t.TempDir(), shellcmd.Args(c, "{}", path))
		if direct != viaEnv || dc != ec {
			t.Fatalf("command %q path %q:\n argv: %q (%d)\n env:  %q (%d)", c, path, direct, dc, viaEnv, ec)
		}
	})
}
