//go:build !windows

package e2e_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The README's commands work with the real binary:
//   R1 every `enma ...` line of a bash block uses only flags enma accepts
//      (checked with --help appended: the parser reads every flag first);
//   R2 the Quick Start commands run as written, in order, in an empty
//      directory: each exits 0, or (the modes that keep running) starts and
//      stops cleanly on Ctrl-C.
// R1 checks flag names only: a wrong value outside the Quick Start (e.g. an
// unknown --args-path-style) is not caught, as those commands are not run
// (they would start builds and daemons). go install / brew are not run (they
// need the network). Both: NOT_VERIFIED here.

type readmeCommand struct {
	line       int
	args       []string
	quickStart bool
}

// shellWords splits a command line like sh does for the quoting the README
// uses: '...', "..." (with \" \$ \\ \` escapes) and backslash escapes.
func shellWords(t *testing.T, s string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				words, inWord = append(words, cur.String()), false
				cur.Reset()
			}
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				t.Fatalf("unterminated ' in %q", s)
			}
			cur.WriteString(s[i+1 : i+1+j])
			i, inWord = i+1+j, true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"$\\`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				t.Fatalf("unterminated \" in %q", s)
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

var trailingComment = regexp.MustCompile(`\s{2,}#.*$|^\s*#.*$`)

func readmeCommands(t *testing.T) []readmeCommand {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	qsStart, qsEnd := strings.Index(text, "## 🚀 Quick Start"), strings.Index(text, "## 🔥 Hotload Mode")
	if qsStart < 0 || qsEnd < qsStart {
		t.Fatal("README: Quick Start section not found")
	}
	var cmds []readmeCommand
	for _, m := range regexp.MustCompile("(?s)```(?:bash|sh|shell)\n(.*?)```").FindAllStringSubmatchIndex(text, -1) {
		block := strings.ReplaceAll(text[m[2]:m[3]], "\\\n", " ") // continuation lines
		line := strings.Count(text[:m[2]], "\n") + 1
		for _, l := range strings.Split(block, "\n") {
			l = strings.TrimSpace(trailingComment.ReplaceAllString(l, ""))
			if strings.HasPrefix(l, "enma ") || l == "enma" {
				cmds = append(cmds, readmeCommand{line: line, args: shellWords(t, l)[1:], quickStart: m[0] < qsEnd && m[0] > qsStart})
			}
		}
	}
	if len(cmds) < 10 {
		t.Fatalf("README: only %d enma commands found", len(cmds))
	}
	return cmds
}

func TestE2E_Readme_CommandsUseValidFlags(t *testing.T) {
	dir := isolated(t, "")
	for _, c := range readmeCommands(t) {
		r := runEnma(t, dir, append(append([]string{}, c.args...), "--help")...)
		if r.code != 0 || !strings.Contains(r.stdout, "Usage:") {
			t.Errorf("R1: README block at line %d: enma %q rejected (exit %d):\n%s", c.line, c.args, r.code, r.stderr)
		}
	}
}

func TestE2E_Readme_QuickStartRuns(t *testing.T) {
	dir := isolated(t, "")
	ran := 0
	for _, c := range readmeCommands(t) {
		if !c.quickStart {
			continue
		}
		ran++
		mode := c.args[0]
		keepsRunning := (mode == "watch" || mode == "hotload" || mode == "scheduler") && !contains(c.args, "--next")
		if !keepsRunning {
			r := runEnma(t, dir, c.args...)
			if r.code != 0 {
				t.Fatalf("R2: line %d: enma %q: exit %d\nstdout %s\nstderr %s", c.line, c.args, r.code, r.stdout, r.stderr)
			}
			continue
		}
		p := startEnma(t, dir, nil, c.args...)
		ready := map[string]string{"watch": "Start file monitor daemon", "hotload": "Watching", "scheduler": "scheduler_start"}[mode]
		if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), ready) }) {
			t.Fatalf("R2: line %d: enma %q did not start:\n%s", c.line, c.args, p.out.String())
		}
		p.interrupt(t)
		if code := p.exitCode(); code != 0 {
			t.Fatalf("R2: line %d: enma %q: exit %d after Ctrl-C:\n%s", c.line, c.args, code, p.out.String())
		}
	}
	if ran < 7 {
		t.Fatalf("R2: only %d Quick Start commands found", ran)
	}
}

// R3: every complete TOML example of the README (one with a [subcommand]
// table) passes `enma config validate` without warnings. The directories the
// examples name are created first, as a user's project would have them.
func TestE2E_Readme_ConfigExamplesAreValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	n := 0
	for _, m := range regexp.MustCompile("(?s)```toml\n(.*?)```").FindAllStringSubmatchIndex(text, -1) {
		body := text[m[2]:m[3]]
		if !strings.Contains(body, "[subcommand]") {
			continue
		}
		n++
		line := strings.Count(text[:m[2]], "\n") + 1
		dir := isolated(t, "")
		for _, d := range []string{"cmd", "internal", "pkg", "lib", "project"} {
			if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "example.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := runEnma(t, dir, "config", "validate", "--config", "example.toml")
		if r.code != 0 || r.stderr != "" {
			t.Errorf("R3: README toml block at line %d: exit %d\nstdout %s\nstderr %s", line, r.code, r.stdout, r.stderr)
		}
	}
	if n < 4 {
		t.Fatalf("R3: only %d complete TOML examples found", n)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
