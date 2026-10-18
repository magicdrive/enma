package core_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

const pv = core.ShellPathVar

func TestShellScript_Contexts(t *testing.T) {
	pre := pv + `="$1"; `
	cases := []struct {
		name, command, placeholder, want string
	}{
		{"standalone", `echo {}`, "{}", pre + `echo "${` + pv + `}"`},
		{"embedded", `make test file={}`, "{}", pre + `make test file="${` + pv + `}"`},
		{"double quotes", `echo "a {} b"`, "{}", pre + `echo "a ${` + pv + `} b"`},
		{"single quotes", `echo 'a {} b'`, "{}", pre + `echo 'a '"${` + pv + `}"' b'`},
		{"ansi-c quotes", `echo $'a\'{}'`, "{}", pre + `echo $'a\''"${` + pv + `}"$''`},
		{"repeated", `cp {} {}.bak`, "{}", pre + `cp "${` + pv + `}" "${` + pv + `}".bak`},
		{"command substitution in dq", `echo "$(basename {})"`, "{}", pre + `echo "$(basename "${` + pv + `}")"`},
		{"nested parens", `echo "$(f() (echo {}); f)"`, "{}", pre + `echo "$(f() (echo "${` + pv + `}"); f)"`},
		{"case inside substitution", `echo "$(case x in x) echo {};; esac)"`, "{}", pre + `echo "$(case x in x) echo "${` + pv + `}";; esac)"`},
		{"after substitution back in dq", `echo "$(true) {}"`, "{}", pre + `echo "$(true) ${` + pv + `}"`},
		{"arithmetic", `echo $((1+2)) {}`, "{}", pre + `echo $((1+2)) "${` + pv + `}"`},
		{"backticks", "echo `basename {}`", "{}", pre + "echo `basename \"${" + pv + "}\"`"},
		{"backticks in dq", "echo \"`basename {}`\"", "{}", pre + "echo \"`basename \\\"${" + pv + "}\\\"`\""},
		{"escaped dq in backticks in dq", "echo \"`echo \\\"{}\\\"`\"", "{}", pre + "echo \"`echo \\\"${" + pv + "}\\\"`\""},
		{"escaped quote in dq", `echo "\"{}\""`, "{}", pre + `echo "\"${` + pv + `}\""`},
		{"escaped quote outside", `echo \'{}`, "{}", pre + `echo \'"${` + pv + `}"`},
		{"escaped placeholder only", `echo \{}`, "{}", `echo \{}`},
		{"escaped placeholder", `echo \{} {}`, "{}", pre + `echo \{} "${` + pv + `}"`},
		{"comment", "# don't {}\necho {}", "{}", pre + "# don't {}\necho \"${" + pv + "}\""},
		{"hash inside word", `echo a#{}`, "{}", pre + `echo a#"${` + pv + `}"`},
		{"heredoc", "cat <<EOF\nit's {}\nEOF\necho {}", "{}", pre + "cat <<EOF\nit's ${" + pv + "}\nEOF\necho \"${" + pv + "}\""},
		{"quoted heredoc", "cat <<'EOF'\nit's {}\nEOF\necho {}", "{}", pre + "cat <<'EOF'\nit's {}\nEOF\necho \"${" + pv + "}\""},
		{"tab heredoc", "cat <<-E\n\tx {}\n\tE\necho {}", "{}", pre + "cat <<-E\n\tx ${" + pv + "}\n\tE\necho \"${" + pv + "}\""},
		{"here-string", `cat <<< {}`, "{}", pre + `cat <<< "${` + pv + `}"`},
		{"here-string then next line", "cat <<< {}\necho {}", "{}", pre + "cat <<< \"${" + pv + "}\"\necho \"${" + pv + "}\""},
		{"word case in substitution", `echo "$(echo case) {}"`, "{}", pre + `echo "$(echo case) ${` + pv + `}"`},
		{"case words as arguments", `echo "$(echo case x in y) {}"`, "{}", pre + `echo "$(echo case x in y) ${` + pv + `}"`},
		{"multi-item case in substitution", `echo "$(case x in a) echo 1;; x) echo {};; esac) {}"`, "{}", pre + `echo "$(case x in a) echo 1;; x) echo "${` + pv + `}";; esac) ${` + pv + `}"`},
		{"case with paren pattern", `echo "$(case x in (x) echo {};; esac) {}"`, "{}", pre + `echo "$(case x in (x) echo "${` + pv + `}";; esac) ${` + pv + `}"`},
		{"arithmetic shift is not a heredoc", "x=$((1<<2))\necho {}", "{}", pre + "x=$((1<<2))\necho \"${" + pv + "}\""},
		{"arithmetic command", "((x = 1 << 2))\necho {}", "{}", pre + "((x = 1 << 2))\necho \"${" + pv + "}\""},
		{"substitution in heredoc", "cat <<EOF\n$(printf '%s' {})\nEOF", "{}", pre + "cat <<EOF\n$(printf '%s' \"${" + pv + "}\")\nEOF"},
		{"two heredocs", "cat <<A <<'B'\na {}\nA\nb {}\nB\necho {}", "{}", pre + "cat <<A <<'B'\na ${" + pv + "}\nA\nb {}\nB\necho \"${" + pv + "}\""},
		{"nested subshell", "((cd . && echo {}) 2>&1)", "{}", pre + "((cd . && echo \"${" + pv + "}\") 2>&1)"},
		{"arithmetic command with quotes", `(( "{}" > 1 ))`, "{}", pre + `(( "${` + pv + `}" > 1 ))`},
		{"esac as an argument", `echo "$(case y in x) echo esac;; y) echo {};; esac)"`, "{}", pre + `echo "$(case y in x) echo esac;; y) echo "${` + pv + `}";; esac)"`},
		{"reserved word as an argument", `echo "$(echo do case in x) {}"`, "{}", pre + `echo "$(echo do case in x) ${` + pv + `}"`},
		{"backslash in arithmetic command", `((printf '[%s]' \" {}) )`, "{}", pre + `((printf '[%s]' \" "${` + pv + `}") )`},
		{"empty case", `echo "$(case x in esac) {}"`, "{}", pre + `echo "$(case x in esac) ${` + pv + `}"`},
		{"bang inside a word", `echo "$(echo hi! case in x) {}"`, "{}", pre + `echo "$(echo hi! case in x) ${` + pv + `}"`},
		{"brace group", `{ case x in x) echo {};; esac; }`, "{}", pre + `{ case x in x) echo "${` + pv + `}";; esac; }`},
		{"negated case", `! case x in y) echo {};; esac`, "{}", pre + `! case x in y) echo "${` + pv + `}";; esac`},
		{"custom placeholder", `go test -run :path`, ":path", pre + `go test -run "${` + pv + `}"`},
		{"placeholder with quote chars", `echo <'f'>`, "<'f'>", pre + `echo "${` + pv + `}"`},
		{"no placeholder", `echo "a 'b'" $x`, "{}", `echo "a 'b'" $x`},
		{"empty placeholder", `echo hi`, "", `echo hi`},
		{"unterminated quote", `echo "{}`, "{}", pre + `echo "${` + pv + `}`},
		{"stray close paren", `case x in x) echo {};; esac`, "{}", pre + `case x in x) echo "${` + pv + `}";; esac`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := core.ShellScript(c.command, c.placeholder); got != c.want {
				t.Fatalf("ShellScript(%q, %q)\n got %s\nwant %s", c.command, c.placeholder, got, c.want)
			}
		})
	}
}

// The argv handed to ExecCommand must carry the path only as $1 (never in the script).
func TestShellCommandArgs_ArgvContract(t *testing.T) {
	path := "/src/x;rm -rf ~;.go"
	var gotName string
	var gotArgs []string
	capture := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.CommandContext(ctx, "true")
	}
	h := &core.HotloadRunner{Options: &option.HotloadOption{Build: "echo {}", Placeholder: "{}"}, BuildTimeout: 5e9, ExecCommand: capture}
	w := &core.WatchRunner{Options: &option.WatchOption{Cmd: "echo {}", Placeholder: "{}"}, CmdTimeout: 5e9, ExecCommand: capture}
	for name, run := range map[string]func(context.Context, string) error{"hotload": h.RunBuild, "watch": w.RunCmd} {
		gotName, gotArgs = "", nil
		if err := run(context.Background(), path); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{"-c", pv + `="$1"; echo "${` + pv + `}"`, core.ShellArgv0, path}
		if gotName != "sh" || strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("%s: argv = %q %q, want sh %q", name, gotName, gotArgs, want)
		}
		if strings.Contains(gotArgs[1], path) {
			t.Fatalf("%s: path leaked into the script", name)
		}
	}
}

func TestShellCommand_PositionalParamsAndArgv0(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	h := &core.HotloadRunner{
		Options:      &option.HotloadOption{Build: `printf '%s|%s|%s|%s' "$0" "$#" "$1" {} > ` + shellQuoteForTest(out), Placeholder: "{}"},
		BuildTimeout: 5e9, ExecCommand: exec.CommandContext,
	}
	if err := h.RunBuild(context.Background(), "/a b.go"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "enma|1|/a b.go|/a b.go" {
		t.Fatalf("got %q", got)
	}
}

// shells returns the POSIX shells available for cross-checking (sh first).
func shells() []string {
	list := []string{"sh"}
	for _, s := range []string{"/bin/dash", "/bin/bash"} {
		if _, err := os.Stat(s); err == nil {
			list = append(list, s)
		}
	}
	return list
}

func runShell(t *testing.T, shell, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(shell, args...)
	core.AdaptShellForTest(cmd) // as enma runs its commands
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s: %v", shell, err)
	}
	return out.String(), code
}

// diffFragments are side-effect-free command lines. For a path without shell
// metacharacters, the old text splice and the new $1 form must print the same.
var diffFragments = []string{
	`printf '[%s]\n' {}`,
	`printf '[%s]\n' "{}" '{}' file={} x{}y`,
	`printf '[%s]\n' "a {} b" 'c {} d'`,
	"# don't {} \"x",
	"cat <<EOF\nit's {} \"q\" $(printf x)\nEOF",
	"cat <<-EOF\n\tbody {} 'q\n\tEOF",
	`printf '[%s]\n' "$(printf '%s' {})"`,
	"printf '[%s]\\n' \"`printf '%s' {}`\"",
	"printf '[%s]\\n' \"`printf '%s' \\\"{}\\\"`\"",
	`printf '[%s]\n' "$(case x in x) printf '%s' {};; esac)"`,
	`printf '[%s]\n' ${ENMA_UNSET_X:-{}} "${ENMA_UNSET_X:-{}}"`,
	`printf '[%s]\n' $((1+2)) {}`,
	`f() { printf '[%s]\n' "$1" {}; }; f arg`,
	`set -- a b; printf '[%s]\n' {} "$#"`,
	`for x in 1 2; do printf '[%s]\n' "$x:{}"; done`,
	`case {} in */*) printf '[slash]\n';; *) printf '[none]\n';; esac`,
	`printf '[%s]\n' "$(echo case) {}" "$(echo esac; echo case) {}"`,
	`printf '[%s]\n' "$(case x in (x) printf '%s' {};; esac)"`,
	`printf '[%s]\n' "$(case x in (a) echo 1;; (x) printf '%s' {};; esac) {}" "$(echo case x in y) {}"`,
	"x=$((1<<2))\nprintf '[%s]\\n' {} \"$x\"",
	"cat <<EOF\n{} $(printf '%s' {}) `printf '%s' {}`\nEOF",
	`sh -c 'printf "[%s]\n" "$1"' sh {}`,
	"if true; then printf '[%s]\\n' {}; fi",
	"((cd . && printf '[%s]\\n' {}) 2>&1) | cat",
	`printf '[%s]\n' "$(case y in x) echo esac;; y) printf '%s' {};; esac)"`,
	`printf '[%s]\n' "$(echo do case in x) {}"`,
	"((printf '[%s]\\n' \\\" {}) )",
	`printf '[%s]\n' "$(case x in esac) {}" "$(echo hi! case in x) {}"`,
	`{ case x in x) printf '[%s]\n' {};; esac; }`,
	`while false; do :; done; until true; do :; done; printf '[%s]\n' {}`,
	`test -n {} && printf '[ok]\n' || printf '[ng]\n'`,
	`printf '[%s]\n' "it's {}" 'say "{}"'`,
}

var plainPaths = []string{"/src/app/main.go", "rel/b_c.go", "x.go", "/a/b-c/d.e.f"}

func oldSplice(command, path string) string { return strings.ReplaceAll(command, "{}", path) }

func checkDifferential(t *testing.T, command, path string) {
	t.Helper()
	dir := t.TempDir()
	for _, sh := range shells() {
		// $0 is "enma" on both sides: the approved $0 change is tested separately.
		oldOut, oldCode := runShell(t, sh, dir, "-c", oldSplice(command, path), core.ShellArgv0)
		newOut, newCode := runShell(t, sh, dir, core.ShellCommandArgs(command, "{}", path)...)
		if oldOut != newOut || oldCode != newCode {
			t.Fatalf("%s: behaviour changed for a plain path %q\ncommand:\n%s\nold (%d): %q\nnew (%d): %q\nscript:\n%s",
				sh, path, command, oldCode, oldOut, newCode, newOut, core.ShellScript(command, "{}"))
		}
	}
}

func TestShellCommand_DifferentialCompat(t *testing.T) {
	for _, frag := range diffOnly {
		for _, p := range plainPaths {
			checkDifferential(t, frag, p)
		}
	}
	for i, frag := range diffFragments {
		for _, p := range plainPaths {
			t.Run(strings.ReplaceAll(frag, "/", "_")[:min(20, len(frag))]+"/"+p, func(t *testing.T) {
				checkDifferential(t, frag, p)
			})
		}
		// pairs exercise context carried across lines
		next := diffFragments[(i+1)%len(diffFragments)]
		checkDifferential(t, frag+"\n"+next, plainPaths[i%len(plainPaths)])
	}
}

// riskyPaths are file names that must reach the command as one verbatim word:
// spaces, a glob that matches a file in the working directory, quotes, command
// substitution, a leading dash and a backslash.
var riskyPaths = []string{
	"./*.go  b",
	"/a b/c'd\"e$(touch PWN)`touch PWN2`;touch PWN3;x.go",
	"/-n x",
	`/a\b\\c`,
}

const metamorphicPlain = "/PLAIN/x.go"

// metamorphicOnly are fragments whose output is not comparable with the old splice.
// diffOnly fragments split an unquoted substitution result by design, so only the
// plain-path comparison with the old splice applies to them.
var diffOnly = []string{
	`printf '[%s]\n' $(printf '%s' {}) ` + "`printf '%s' {}`",
}

var metamorphicOnly = []string{
	"cat <<'EOF'\nraw {}\nEOF\nprintf '[%s]\\n' {}",
}

// checkMetamorphic: running with a risky path must print exactly what running with a
// plain path prints, with the plain path replaced by the risky one, and run nothing.
func checkMetamorphic(t *testing.T, command string) {
	t.Helper()
	for _, sh := range shells() {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.go"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		base, baseCode := runShell(t, sh, dir, core.ShellCommandArgs(command, "{}", metamorphicPlain)...)
		if strings.Contains(base, "syntax error") {
			continue // this shell cannot parse the command itself (e.g. bash 3.2 and case in $( ))
		}
		for _, risky := range riskyPaths {
			got, code := runShell(t, sh, dir, core.ShellCommandArgs(command, "{}", risky)...)
			want := strings.ReplaceAll(base, metamorphicPlain, risky)
			if got != want || code != baseCode {
				t.Fatalf("%s: path %q not passed verbatim\ncommand:\n%s\ngot (%d):  %q\nwant (%d): %q\nscript:\n%s",
					sh, risky, command, code, got, baseCode, want, core.ShellScript(command, "{}"))
			}
		}
		for _, f := range []string{"PWN", "PWN2", "PWN3"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				t.Fatalf("%s: injection created %s for command:\n%s", sh, f, command)
			}
		}
	}
}

func TestShellCommand_Metamorphic(t *testing.T) {
	all := append(append([]string{}, diffFragments...), metamorphicOnly...)
	for i, frag := range all {
		checkMetamorphic(t, frag)
		checkMetamorphic(t, frag+"\n"+all[(i+1)%len(all)])
	}
}

func TestShellCommand_QuotedHeredocKeepsPlaceholder(t *testing.T) {
	dir := t.TempDir()
	cmd := "cat <<'EOF'\nraw {}\nEOF\nprintf '[%s]' {}"
	for _, sh := range shells() {
		out, code := runShell(t, sh, dir, core.ShellCommandArgs(cmd, "{}", "/x;touch PWN.go")...)
		if code != 0 || out != "raw {}\n[/x;touch PWN.go]" {
			t.Fatalf("%s: got %d %q", sh, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "PWN.go")); err == nil {
		t.Fatal("injection through quoted here-document")
	}
}

// FuzzShellCommandArgs runs the real shell: whatever the path, it must arrive verbatim
// in every quoting context and never execute anything.
func FuzzShellCommandArgs(f *testing.F) {
	for _, p := range adversarialPaths("MARK") {
		f.Add(p, uint8(0))
	}
	f.Add("a'b\"c`d$(e)f\\g", uint8(3))
	f.Fuzz(func(t *testing.T, path string, which uint8) {
		if strings.ContainsRune(path, 0) || !utf8.ValidString(path) {
			return // argv cannot carry NUL; non-UTF-8 is out of scope
		}
		dir := t.TempDir()
		out := filepath.Join(dir, "out")
		forms := []struct{ arg, prefix, suffix string }{
			{`{}`, "", ""}, {`"{}"`, "", ""}, {`'{}'`, "", ""}, {`x{}y`, "x", "y"},
			{`"a {} b"`, "a ", " b"}, {`'a {} b'`, "a ", " b"}, {`$'a {} b'`, "a ", " b"},
			{"\"`printf '%s' \\\"{}\\\"`\"", "", ""},
		}
		form := forms[int(which)%len(forms)]
		cmd := exec.Command("bash", core.ShellCommandArgs(`printf '[%s]' `+form.arg+` > `+shellQuoteForTest(out), "{}", path)...)
		if form.arg[0] != '$' && !strings.HasPrefix(form.arg, "\"`") {
			cmd = exec.Command("sh", cmd.Args[1:]...)
		}
		core.AdaptShellForTest(cmd) // as enma runs its commands
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shell failed for %q: %v %s", path, err, b)
		}
		got, _ := os.ReadFile(out)
		want := "[" + form.prefix + path + form.suffix + "]"
		if strings.HasPrefix(form.arg, "\"`") {
			want = "[" + strings.TrimRight(path, "\n") + "]" // command substitution drops trailing newlines
		}
		if string(got) != want {
			t.Fatalf("form %s: got %q want %q", form.arg, got, want)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("path %q created extra files: %v", path, entries)
		}
	})
}

// FuzzShellCommandDifferential composes command lines from fragments and checks
// that, for a plain path, the new form behaves exactly like the old splice.
func FuzzShellCommandDifferential(f *testing.F) {
	f.Add([]byte{0, 3, 4})
	f.Add([]byte{3, 8, 12, 4, 5})
	f.Fuzz(func(t *testing.T, pick []byte) {
		if len(pick) == 0 || len(pick) > 6 {
			return
		}
		var lines []string
		for _, p := range pick {
			lines = append(lines, diffFragments[int(p)%len(diffFragments)])
		}
		command := strings.Join(lines, "\n")
		checkDifferential(t, command, plainPaths[int(pick[0])%len(plainPaths)])
		checkMetamorphic(t, command)
	})
}

// FuzzShellScript checks lexer robustness: no panic, identity without a placeholder.
func FuzzShellScript(f *testing.F) {
	for _, s := range []string{`echo {}`, `echo "$(a '{}')"`, "`{}`", `\`, `"`, `'`, `$(`, `)`, `echo \{}`, "<<", "<<'", "cat <<E\n{}\\", "# {}", "$'\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, command string) {
		got := core.ShellScript(command, "{}")
		if !strings.Contains(command, "{}") && got != command {
			t.Fatalf("changed command without placeholder: %q -> %q", command, got)
		}
		if got != command && !strings.HasPrefix(got, pv+`="$1"; `) {
			t.Fatalf("rewritten script lacks the path variable: %q", got)
		}
	})
}
