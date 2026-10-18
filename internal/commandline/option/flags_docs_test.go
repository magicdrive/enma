package option_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

// The help texts and the shell completions name the flags the parsers define:
// no flag is missing from them, and none they offer is rejected by enma.

// definedFlags returns the flag names a parser file defines (fs.String("name", ...),
// fs.StringVar(p, "name", ...), ...).
func definedFlags(t *testing.T, file string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "fs" {
			return true
		}
		arg := 0
		switch sel.Sel.Name {
		case "String", "Bool", "Int", "Duration":
		case "StringVar", "BoolVar", "IntVar", "DurationVar":
			arg = 1
		default:
			return true
		}
		if lit, ok := call.Args[arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			name, _ := strconv.Unquote(lit.Value)
			names[name] = true
		}
		return true
	})
	if len(names) == 0 {
		t.Fatalf("%s: no flags found", file)
	}
	return names
}

// flagVars maps each flag name of a parser file to the variable it sets:
// x := fs.String("name", ...) and fs.StringVar(x, "n", ...) both give x.
func flagVars(t *testing.T, file string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	name := func(e ast.Expr) string {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s, _ := strconv.Unquote(lit.Value)
			return s
		}
		return ""
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt: // x := fs.String("name", ...)
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				if call, ok := n.Rhs[0].(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && !strings.HasSuffix(sel.Sel.Name, "Var") && len(call.Args) > 0 {
						if id, ok := n.Lhs[0].(*ast.Ident); ok && name(call.Args[0]) != "" {
							vars[name(call.Args[0])] = id.Name
						}
					}
				}
			}
		case *ast.CallExpr: // fs.StringVar(x, "n", ...)
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && strings.HasSuffix(sel.Sel.Name, "Var") && len(n.Args) > 1 {
				if id, ok := n.Args[0].(*ast.Ident); ok && name(n.Args[1]) != "" {
					vars[name(n.Args[1])] = id.Name
				}
			}
		}
		return true
	})
	return vars
}

// helpFlagLine matches the flags at the start of a help line: "  -g, --ignore-file-regex <regex>".
var helpFlagLine = regexp.MustCompile(`^\s+(-[^<]*?)(?:\s<|\s{2,}|$)`)

// checkPairs reports help lines that put flags of different variables together
// (a short flag next to the wrong long one).
func checkPairs(t *testing.T, help, text string, vars map[string]string) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		m := helpFlagLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		seen := map[string]bool{}
		var names []string
		for _, f := range regexp.MustCompile(`--?([a-zA-Z][a-zA-Z0-9-]*)`).FindAllStringSubmatch(m[1], -1) {
			if v, ok := vars[f[1]]; ok {
				seen[v] = true
				names = append(names, f[0])
			}
		}
		if len(seen) > 1 {
			t.Errorf("%s: %v on one line set different options: %q", help, names, strings.TrimSpace(line))
		}
	}
}

func long(defined map[string]bool) map[string]bool {
	out := map[string]bool{}
	for n := range defined {
		if len(n) > 1 {
			out[n] = true
		}
	}
	return out
}

// sameFlag maps the long spellings of one flag to each other: a completion
// offers one of them (the help names all).
var sameFlag = map[string]string{
	"abs": "absolute-path", "absolute-path": "abs", "cmd": "command", "command": "cmd",
	"pre-cmd": "pre-command", "pre-command": "pre-cmd", "post-cmd": "post-command", "post-command": "post-cmd",
}

var subcommandFiles = map[string]string{
	"init": "init.go", "hotload": "hotload.go", "watch": "watch.go", "scheduler": "scheduler.go", "config": "config.go",
}

var longFlagRE = regexp.MustCompile(`(?:^|[\s'"(=,\[])--([a-z][a-z0-9-]*)`)

func mentioned(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range longFlagRE.FindAllStringSubmatch(text, -1) {
		out[m[1]] = true
	}
	return out
}

func sortedDiff(a, b map[string]bool) []string { // in a, not in b
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// caseBranches returns the text of each case branch `name)` up to its `;;` line.
func caseBranches(t *testing.T, text, name string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?s)\n\s*` + regexp.QuoteMeta(name) + `\)\n(.*?)\n\s*;;`) // a branch ends at a ";;" line
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatalf("no %q branch", name)
	}
	return out
}

// offerSets returns the flags a completion branch offers, one set per zsh
// `_values` call (the hotload branch has one per OS family, each complete on
// its own: audit H5B-03), or for bash the words of all opts= lines of the
// branch (they offer different words by context).
func offerSets(branch string) []map[string]bool {
	calls := regexp.MustCompile(`(?s)_values '[^']*'(?:[^\n]*\\\n)*[^\n]*`).FindAllString(branch, -1)
	if len(calls) == 0 { // bash: the words of the opts= lines (not the conditions around them: H5R2-03)
		words := map[string]bool{}
		for _, m := range regexp.MustCompile(`opts="([^"]*)"`).FindAllStringSubmatch(branch, -1) {
			for k := range mentioned(" " + m[1]) {
				words[k] = true
			}
		}
		return []map[string]bool{words}
	}
	var out []map[string]bool
	for _, c := range calls {
		out = append(out, mentioned(c))
	}
	return out
}

func TestCompletionsOfferTheDefinedFlags(t *testing.T) {
	root := filepath.Join("..", "..", "..", "misc", "completions")
	files := []string{
		filepath.Join(root, "bash", "enma-completion.bash"),
		filepath.Join(root, "zsh", "_enma"),
		filepath.Join(root, "enma-completion.sh"), // both shells
	}
	for _, file := range files {
		text := readFile(t, file)
		for sub, src := range subcommandFiles {
			def := long(definedFlags(t, src))
			for _, branch := range caseBranches(t, text, sub) {
				for _, offered := range offerSets(branch) {
					if bad := sortedDiff(offered, def); len(bad) > 0 {
						t.Errorf("%s %s: offers flags enma rejects: %v", file, sub, bad)
					}
					var missing []string
					for _, n := range sortedDiff(def, offered) {
						if !offered[sameFlag[n]] {
							missing = append(missing, n)
						}
					}
					if len(missing) > 0 {
						t.Errorf("%s %s: does not offer %v", file, sub, missing)
					}
				}
			}
		}
		for _, sub := range []string{"init", "hotload", "watch", "scheduler", "config"} {
			if !strings.Contains(text, sub) {
				t.Errorf("%s: subcommand %s not offered", file, sub)
			}
		}
	}
}

func TestSubcommandHelpNamesTheDefinedFlags(t *testing.T) {
	dir := filepath.Join("..", "..", "textbank", "individual_help")
	helps := map[string]string{
		"enma_init_help.txt": "init.go", "enma_hotload_help.txt": "hotload.go", "enma_watch_help.txt": "watch.go",
		"enma_scheduler_help.txt": "scheduler.go", "enma_config_help.txt": "config.go",
	}
	for help, src := range helps {
		text := readFile(t, filepath.Join(dir, help))
		def := definedFlags(t, src)
		checkPairs(t, help, text, flagVars(t, src))
		if bad := sortedDiff(mentioned(text), long(def)); len(bad) > 0 {
			t.Errorf("%s names flags enma rejects: %v", help, bad)
		}
		for _, m := range regexp.MustCompile(`(?:^|[\s,])-([a-zA-Z])(?:[\s,]|$)`).FindAllStringSubmatch(text, -1) {
			if !def[m[1]] {
				t.Errorf("%s names -%s, which enma rejects", help, m[1])
			}
		}
		var missing []string
		for n := range def {
			form := "--" + n
			if len(n) == 1 {
				form = "-" + n
			}
			if !regexp.MustCompile(`(^|[\s,])` + regexp.QuoteMeta(form) + `([\s,]|$)`).MatchString(text) {
				missing = append(missing, form)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s does not name %v", help, missing)
		}
	}
}

// The command reference tables of the README list the defined flags, each row
// one option (a short flag next to its own long one).
func TestReadmeReferenceMatchesFlags(t *testing.T) {
	text := readFile(t, filepath.Join("..", "..", "..", "README.md"))
	sections := map[string]string{
		"### `enma init`": "init.go", "### `enma hotload`": "hotload.go", "### `enma watch`": "watch.go",
		"### `enma scheduler`": "scheduler.go", "### `enma config validate`": "config.go",
		"### Global Options": "general.go",
	}
	cell := regexp.MustCompile("`(--?[a-zA-Z][a-zA-Z0-9-]*)")
	for heading, src := range sections {
		i := strings.Index(text, "\n"+heading+"\n")
		if i < 0 {
			t.Errorf("README: no %q section", heading)
			continue
		}
		body := text[i+len(heading)+2:]
		if j := strings.Index(body, "\n---"); j >= 0 {
			body = body[:j]
		}
		def, vars := definedFlags(t, src), flagVars(t, src)
		listed := map[string]bool{}
		for _, row := range strings.Split(body, "\n") {
			if !strings.HasPrefix(row, "| `") {
				continue
			}
			first := strings.Split(row, "|")[1]
			seen := map[string]bool{}
			for _, m := range cell.FindAllStringSubmatch(first, -1) {
				name := strings.TrimLeft(m[1], "-")
				if !def[name] {
					t.Errorf("README %s: %s is not a flag", heading, m[1])
					continue
				}
				listed[name] = true
				seen[vars[name]] = true
			}
			if len(seen) > 1 {
				t.Errorf("README %s: one row sets different options: %s", heading, strings.TrimSpace(first))
			}
		}
		for n := range long(def) {
			if !listed[n] && !listed[sameFlag[n]] {
				t.Errorf("README %s: --%s is not listed", heading, n)
			}
		}
	}
}

// -C, documented for --check-content-diff, is accepted (it used to be rejected
// with exit 2).
func TestCheckContentDiffShortFlag(t *testing.T) {
	w, err := option.ParseWatch([]string{"--command", "true", "-C", "off"})
	if err != nil || w.CheckContentDiffValue != "off" {
		t.Fatalf("watch -C off: %v, value %q", err, w.CheckContentDiffValue)
	}
	h, err := option.ParseHotload([]string{"--daemon", "x", "--build", "y", "-C", "off"})
	if err != nil || h.CheckContentDiffValue != "off" {
		t.Fatalf("hotload -C off: %v, value %q", err, h.CheckContentDiffValue)
	}
}

// `enma --help` (help.txt) has one section per subcommand; each names the
// flags of that subcommand, with short flags next to their own long ones.
func TestGlobalHelpSectionsMatchFlags(t *testing.T) {
	text := readFile(t, filepath.Join("..", "..", "textbank", "help.txt"))
	sections := map[string]string{
		"init options:": "init.go", "hotload options:": "hotload.go", "watch options:": "watch.go",
		"scheduler options": "scheduler.go", "config validate options:": "config.go", "General options:": "general.go",
	}
	for heading, src := range sections {
		i := strings.Index(text, "\n"+heading)
		if i < 0 {
			t.Errorf("help.txt: no %q section", heading)
			continue
		}
		body := text[i+1:]
		body = body[strings.Index(body, "\n")+1:]
		if j := strings.Index(body, "\n\n"); j >= 0 {
			body = body[:j]
		}
		def := definedFlags(t, src)
		checkPairs(t, "help.txt "+heading, body, flagVars(t, src))
		if bad := sortedDiff(mentioned(body), long(def)); len(bad) > 0 {
			t.Errorf("help.txt %s names flags enma rejects: %v", heading, bad)
		}
		for n := range def {
			form := "--" + n
			if len(n) == 1 {
				form = "-" + n
			}
			if !regexp.MustCompile(`(^|[\s,])` + regexp.QuoteMeta(form) + `([\s,]|$)`).MatchString(body) {
				t.Errorf("help.txt %s does not name %s", heading, form)
			}
		}
	}
}

// The value lists zsh offers (--signal, --args-path-style, --default-ignores,
// on/off switches) are accepted by enma (audit H5B-06: `full` was offered).
func TestCompletionValuesAreAccepted(t *testing.T) {
	root := filepath.Join("..", "..", "..", "misc", "completions")
	value := regexp.MustCompile(`'--([a-z-]+)\[[^\]]*\]:[^:']*:\(([^)]*)\)'`)
	parsers := map[string]func(flag, v string) error{
		"hotload": func(flag, v string) error {
			_, err := option.ParseHotload([]string{"--daemon", "x", "--build", "y", "--" + flag, v})
			return err
		},
		"watch": func(flag, v string) error {
			_, err := option.ParseWatch([]string{"--command", "x", "--" + flag, v})
			return err
		},
	}
	for _, file := range []string{filepath.Join(root, "zsh", "_enma"), filepath.Join(root, "enma-completion.sh")} {
		text := readFile(t, file)
		checked := 0
		for sub, parse := range parsers {
			for _, branch := range caseBranches(t, text, sub) {
				for _, m := range value.FindAllStringSubmatch(branch, -1) {
					for _, v := range strings.Fields(m[2]) {
						if v == "SIGUSR1" || v == "SIGUSR2" || v == "SIGHUP" { // offered on Unix only
							if _, err := os.Stat("/bin/sh"); err != nil {
								continue
							}
						}
						checked++
						if err := parse(m[1], v); err != nil {
							t.Errorf("%s %s: --%s %s is offered but rejected: %v", file, sub, m[1], v, err)
						}
					}
				}
			}
		}
		if checked < 10 {
			t.Fatalf("%s: only %d offered values found", file, checked)
		}
	}
}
