package core_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// H1a invariants (F01):
//   I1 a file path is never evaluated as shell syntax;
//   I2 the command receives the path text unchanged (no splitting / globbing / quote damage);
//   I3 placeholder positions keep their meaning (standalone, embedded, quoted, repeated);
//   I4 hotload and watch behave the same.

// adversarialPaths returns file names that must reach the command verbatim.
// "MARK" is replaced by a marker file that only exists if the name was executed.
func adversarialPaths(mark string) map[string]string {
	return map[string]string{
		"plain":         "/src/app/main.go",
		"relative":      "./sub dir/main.go",
		"space":         "/src/my file.go",
		"semicolon":     "/src/a;touch " + mark + ";.go",
		"and":           "/src/a&&touch " + mark + "&&.go",
		"pipe":          "/src/a|touch " + mark + "|.go",
		"dollar-paren":  "/src/a$(touch " + mark + ").go",
		"backtick":      "/src/a`touch " + mark + "`.go",
		"single-quote":  "/src/it's';touch " + mark + ";'.go",
		"double-quote":  "/src/say\"hi\";touch " + mark + ";\".go",
		"newline":       "/src/a\ntouch " + mark + "\n.go",
		"backslash":     `/src/a\b\;touch ` + mark + `.go`,
		"glob":          "/src/*.go",
		"glob-question": "/src/?.go",
		"variable":      "/src/$HOME${IFS}x.go",
		"unicode":       "/src/日本語 ファイル✓.go",
		"leading-dash":  "-n",
		"redirect":      "/src/a>" + mark + ".go",
		"empty":         "",
	}
}

type placeholderCase struct {
	name    string
	command string // OUT is replaced by the output file path
	want    func(path string) string
}

func placeholderCases() []placeholderCase {
	return []placeholderCase{
		{"standalone", `printf '[%s]\n' {} > OUT`, func(p string) string { return "[" + p + "]\n" }},
		{"double-quoted", `printf '[%s]\n' "{}" > OUT`, func(p string) string { return "[" + p + "]\n" }},
		{"single-quoted", `printf '[%s]\n' '{}' > OUT`, func(p string) string { return "[" + p + "]\n" }},
		{"embedded", `printf '[%s]\n' file={} > OUT`, func(p string) string { return "[file=" + p + "]\n" }},
		{"surrounded", `printf '[%s]\n' x{}y > OUT`, func(p string) string { return "[x" + p + "y]\n" }},
		{"in-dq-text", `printf '[%s]\n' "changed: {} done" > OUT`, func(p string) string { return "[changed: " + p + " done]\n" }},
		{"in-sq-text", `printf '[%s]\n' 'changed: {} done' > OUT`, func(p string) string { return "[changed: " + p + " done]\n" }},
		{"repeated", `printf '[%s]\n' {} "{}" > OUT`, func(p string) string { return "[" + p + "]\n[" + p + "]\n" }},
	}
}

// runners exposes every command-running entry point of both runners.
func commandRunners(command string, timeout time.Duration) map[string]func(ctx context.Context, path string) error {
	h := &core.HotloadRunner{
		Options:      &option.HotloadOption{PreBuild: command, Build: command, PostBuild: command, Placeholder: "{}"},
		BuildTimeout: timeout,
		ExecCommand:  exec.CommandContext,
	}
	w := &core.WatchRunner{
		Options:     &option.WatchOption{PreCmd: command, Cmd: command, PostCmd: command, Placeholder: "{}"},
		CmdTimeout:  timeout,
		ExecCommand: exec.CommandContext,
	}
	return map[string]func(ctx context.Context, path string) error{
		"hotload/pre-build":  h.RunPreBuild,
		"hotload/build":      h.RunBuild,
		"hotload/post-build": h.RunPostBuild,
		"watch/pre-command":  w.RunPreCmd,
		"watch/command":      w.RunCmd,
		"watch/post-command": w.RunPostCmd,
	}
}

func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestCommandSafety_PathIsNeverShellSyntax(t *testing.T) {
	for _, pc := range placeholderCases() {
		for pathName := range adversarialPaths("M") {
			for runnerName := range commandRunners("", time.Second) {
				pc, pathName, runnerName := pc, pathName, runnerName
				t.Run(pc.name+"/"+pathName+"/"+runnerName, func(t *testing.T) {
					dir := t.TempDir()
					mark := filepath.Join(dir, "INJECTED")
					path := adversarialPaths(mark)[pathName]
					out := filepath.Join(dir, "out.txt")
					command := strings.ReplaceAll(pc.command, "OUT", shellQuoteForTest(out))
					run := commandRunners(command, 10*time.Second)[runnerName]

					err := run(context.Background(), path)

					if _, statErr := os.Stat(mark); statErr == nil {
						t.Fatalf("I1 violated: path %q was executed as shell syntax (marker created)", path)
					}
					if err != nil {
						t.Fatalf("command failed: %v", err)
					}
					got, readErr := os.ReadFile(out)
					if readErr != nil {
						t.Fatalf("no output: %v", readErr)
					}
					if want := pc.want(path); string(got) != want {
						t.Fatalf("I2/I3 violated:\n got %q\nwant %q", got, want)
					}
				})
			}
		}
	}
}

func TestCommandSafety_NoPlaceholderUnchanged(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	command := `a="x y"; printf '[%s]' "$a" | tr x z > ` + shellQuoteForTest(out)
	for name, run := range commandRunners(command, 10*time.Second) {
		_ = os.Remove(out)
		if err := run(context.Background(), "/src/a;b.go"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := os.ReadFile(out)
		if string(got) != "[z y]" {
			t.Fatalf("%s: command without placeholder changed behaviour: %q", name, got)
		}
	}
}

func TestCommandSafety_ExitStatusPropagates(t *testing.T) {
	for name, run := range commandRunners(`test -e {} && exit 0; exit 3`, 10*time.Second) {
		err := run(context.Background(), "/nonexistent/a;exit 0;.go")
		var ee *exec.ExitError
		if err == nil || !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Fatalf("%s: want exit status 3, got %v", name, err)
		}
	}
}
