//go:build !windows

package commandline_test

// The shell completions in misc/completions offer what enma accepts: the
// subcommands, the flags of each command (every spelling, no other), and for
// each flag what its value is (a list of values, a file, a directory, a
// command or free text). The flags and the value lists come from enma itself
// (the parsers' flag sets, the model package); the tests start bash, zsh and
// fish, ask them for completions and compare.
//
// A shell that is not installed is skipped, unless ENMA_COMPLETION_SHELLS
// names it (CI: "bash zsh fish"), then the test fails.

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/model"
)

var update = flag.Bool("update", false, "rewrite misc/completions/enma-completion.sh from the bash and zsh scripts")

var compDir = filepath.Join("..", "..", "misc", "completions")

// What the value of a flag is.
type kind string

const (
	kBool    kind = "bool"    // no value
	kText    kind = "text"    // free text: nothing to offer
	kCommand kind = "command" // a command line: command names
	kFile    kind = "file"
	kFiles   kind = "files" // comma-separated
	kDir     kind = "dir"
	kDirs    kind = "dirs" // comma-separated
	kSignal  kind = "signal"
	kSwitch  kind = "switch"
	kIgnores kind = "ignores"
	kMode    kind = "mode"
	kStyle   kind = "style" // comma-separated
)

// flagKinds says what each flag of a command takes. It must name exactly the
// flags enma defines (TestCompletionFlagKinds): a new flag fails the tests
// until it is added here and to each completion script.
var flagKinds = map[string]map[string]kind{
	"general": {"-c": kFile, "--config": kFile, "-h": kBool, "--help": kBool, "-v": kBool, "--version": kBool},
	"init":    {"-h": kBool, "--help": kBool, "-m": kMode, "--mode": kMode, "-f": kFile, "--file-name": kFile},
	"hotload": merge(watchKinds, map[string]kind{
		"-d": kCommand, "--daemon": kCommand, "-b": kCommand, "--build": kCommand,
		"-S": kSignal, "--signal": kSignal, "-p": kCommand, "--pre-build": kCommand,
		"-P": kCommand, "--post-build": kCommand, "-B": kSwitch, "--build-at-start": kSwitch,
	}),
	"watch": merge(watchKinds, map[string]kind{
		"-c": kCommand, "--cmd": kCommand, "--command": kCommand,
		"-p": kCommand, "--pre-cmd": kCommand, "--pre-command": kCommand,
		"-P": kCommand, "--post-cmd": kCommand, "--post-command": kCommand,
	}),
	"scheduler": {
		"-h": kBool, "--help": kBool, "--cron": kText, "--command": kCommand, "--name": kText,
		"--working-dir": kDir, "--timezone": kText, "--timeout": kText, "--retries": kText,
		"--retry-delay": kText, "--max-concurrent": kText, "--next": kText, "--logs": kFile, "--pid": kFile,
	},
	"validate": {"-h": kBool, "--help": kBool, "-c": kFile, "-f": kFile, "--config": kFile, "--next": kText},
}

// the flags hotload and watch share
var watchKinds = map[string]kind{
	"-h": kBool, "--help": kBool, "--start-at-once": kCommand, "-W": kDir, "--working-dir": kDir,
	"-I": kText, "--placeholder": kText, "-s": kStyle, "--args-path-style": kStyle,
	"-C": kSwitch, "--check-content-diff": kSwitch, "-A": kSwitch, "--abs": kSwitch, "--absolute-path": kSwitch,
	"-t": kText, "--timeout": kText, "-l": kText, "--delay": kText, "-r": kText, "--retry": kText,
	"-w": kDirs, "--watch-dir": kDirs, "-x": kText, "--pattern-regex": kText, "-i": kText, "--include-ext": kText,
	"-g": kText, "--ignore-file-regex": kText, "-G": kText, "--ignore-dir-regex": kText,
	"-e": kText, "--exclude-ext": kText, "-E": kDirs, "--exclude-dir": kDirs,
	"-D": kIgnores, "--default-ignores": kIgnores, "-n": kFiles, "--enmaignore": kFiles,
	"--logs": kFile, "--pid": kFile,
}

func merge(a, b map[string]kind) map[string]kind {
	out := map[string]kind{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// the words before a command's flags
var commandWords = map[string][]string{
	"general": nil, "init": {"init"}, "hotload": {"hotload"}, "watch": {"watch"},
	"scheduler": {"scheduler"}, "validate": {"config", "validate"},
}

// flagSets returns the flag set each command parses its arguments with.
func flagSets(t *testing.T) map[string]*flag.FlagSet {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	g, err := option.ParseGeneral(nil)
	must(err)
	i, err := option.ParseInit([]string{"--file-name", filepath.Join(t.TempDir(), "Enma.toml")})
	must(err)
	h, err := option.ParseHotload([]string{"--daemon", "x", "--build", "y"})
	must(err)
	w, err := option.ParseWatch([]string{"--command", "x"})
	must(err)
	s, err := option.ParseScheduler([]string{"--cron", "0 0 * * * *", "--command", "x"})
	must(err)
	v, err := option.ParseConfigValidate(nil)
	must(err)
	return map[string]*flag.FlagSet{
		"general": g.FlagSet, "init": i.FlagSet, "hotload": h.FlagSet, "watch": w.FlagSet,
		"scheduler": s.FlagSet, "validate": v.FlagSet,
	}
}

// spelling is how a completion offers a flag: -x, --name.
func spelling(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// The kinds above name exactly the flags enma defines; a bool flag is a bool
// kind, and the spellings of one option (flags setting one variable) have one
// kind.
func TestCompletionFlagKinds(t *testing.T) {
	for cmd, fs := range flagSets(t) {
		kinds := flagKinds[cmd]
		defined := map[string]bool{}
		byVar := map[uintptr][]string{}
		fs.VisitAll(func(f *flag.Flag) {
			name := spelling(f.Name)
			defined[name] = true
			k, ok := kinds[name]
			if !ok {
				t.Errorf("%s %s: no kind in flagKinds (add it, and complete it in each script)", cmd, name)
				return
			}
			if (k == kBool) != isBool(f) {
				t.Errorf("%s %s: kind %s, but bool flag is %v", cmd, name, k, isBool(f))
			}
			p := reflect.ValueOf(f.Value).Pointer()
			byVar[p] = append(byVar[p], name)
		})
		for name := range kinds {
			if !defined[name] {
				t.Errorf("%s %s: in flagKinds, not defined by enma", cmd, name)
			}
		}
		for _, names := range byVar {
			for _, n := range names[1:] {
				if kinds[n] != kinds[names[0]] {
					t.Errorf("%s: %s (%s) and %s (%s) set one option", cmd, names[0], kinds[names[0]], n, kinds[n])
				}
			}
		}
	}
}

// --- what enma accepts, from its sources -------------------------------------

func parseGo(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func strLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// caseStrings returns the string literals of the case clauses in function fn.
func caseStrings(t *testing.T, path, fn string) []string {
	t.Helper()
	var out []string
	for _, d := range parseGo(t, path).Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			if cc, ok := n.(*ast.CaseClause); ok {
				for _, e := range cc.List {
					if s, ok := strLit(e); ok {
						out = append(out, s)
					}
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatalf("%s: no case strings in %s", path, fn)
	}
	return out
}

// subcommands are the non-empty string constants of commandline (the names
// Execute dispatches on).
func subcommands(t *testing.T) []string {
	var out []string
	for _, d := range parseGo(t, "mod.go").Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			for _, v := range s.(*ast.ValueSpec).Values {
				if name, ok := strLit(v); ok && name != "" {
					out = append(out, name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// configWords splits the words `enma config` takes into actions and flags
// ("help" is another spelling of --help: not offered).
func configWords(t *testing.T) (actions, flags []string) {
	for _, w := range caseStrings(t, filepath.Join("subcmd", "config.go"), "Config") {
		switch {
		case strings.HasPrefix(w, "-"):
			flags = append(flags, w)
		case w != "help":
			actions = append(actions, w)
		}
	}
	return actions, flags
}

// windowsSignals are the keys of the Windows signal map.
func windowsSignals(t *testing.T) []string {
	var out []string
	ast.Inspect(parseGo(t, filepath.Join("..", "model", "signal_map_windows.go")), func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if s, ok := strLit(kv.Key); ok {
				out = append(out, s)
			}
		}
		return true
	})
	sort.Strings(out)
	return out
}

// checkValues reports what is wrong with the values a completion offers for
// a flag of kind k: each must be accepted by enma, and they must name each
// setting enma has once (min and minimal would be one setting twice).
func checkValues(t *testing.T, k kind, offered []string, windows bool) string {
	var want []string
	canon := func(v string) (string, error) { return v, nil }
	switch k {
	case kSignal:
		want = model.AllowedSignals()
		if windows {
			want = windowsSignals(t)
		}
		if !windows {
			canon = func(v string) (string, error) { var s model.SignalName; return v, s.Set(v) }
		}
	case kSwitch:
		want = []string{model.SwitchOn, model.SwitchOff}
		canon = func(v string) (string, error) { var s model.OnOffSwitch; err := s.Set(v); return s.String(), err }
	case kIgnores:
		want = []string{model.Minimal, model.Maximum, model.Nothing}
		canon = func(v string) (string, error) { var s model.IgnoreType; err := s.Set(v); return s.String(), err }
	case kStyle:
		want = []string{model.DIRNAME, model.BASENAME, model.EXTENSION}
		canon = func(v string) (string, error) {
			var s model.ArgsPathStyleString
			if err := s.Set(v); err != nil {
				return "", err
			}
			o, _ := s.ArgsPathStyleObj()
			switch {
			case o.DirNameFlag && !o.BaseNameFlag && !o.ExtensionFlag:
				return model.DIRNAME, nil
			case o.BaseNameFlag && !o.DirNameFlag && !o.ExtensionFlag:
				return model.BASENAME, nil
			case o.ExtensionFlag && !o.DirNameFlag && !o.BaseNameFlag:
				return model.EXTENSION, nil
			}
			return "", fmt.Errorf("%q is not one part", v)
		}
	case kMode:
		want = caseStrings(t, filepath.Join("subcmd", "init.go"), "RunInit")
	default:
		return fmt.Sprintf("kind %s has no value list", k)
	}
	got := map[string]string{}
	for _, v := range offered {
		c, err := canon(v)
		if err != nil {
			return fmt.Sprintf("%q is offered, enma rejects it: %v", v, err)
		}
		if prev, dup := got[c]; dup {
			return fmt.Sprintf("%q and %q are one setting (%s)", prev, v, c)
		}
		got[c] = v
	}
	var have []string
	for c := range got {
		have = append(have, c)
	}
	sort.Strings(have)
	want = slices.Clone(want)
	sort.Strings(want)
	if !slices.Equal(have, want) {
		return fmt.Sprintf("offers %v (settings %v), enma has %v", offered, have, want)
	}
	return ""
}

// --- running the shells --------------------------------------------------------

// A completion request: the words after "enma"; the last is the one being
// completed (often "").
type request struct {
	words []string
	// windows: complete as on Windows (MSYS2, Git Bash, Cygwin)
	windows bool
}

func (r request) String() string {
	s := "enma " + strings.Join(r.words, " ") + "<TAB>"
	if r.windows {
		s += " (windows)"
	}
	return s
}

// A shell runs a batch of requests and returns the candidates of each.
// Candidates are unquoted ("my file.toml", not my\ file.toml); a directory
// may end with "/". Bash results also hold "COMPOPT <options>" when the
// completion asks readline to complete a path itself.
type shell struct {
	name string
	bin  string
	// bash: compopt exists (bash 4 and later); without it readline completes paths
	compopt bool
	run     func(t *testing.T, sh *shell, script string, reqs []request) [][]string
}

type fixture struct {
	dir, home, sideEffect string
	env                   []string
}

var fx *fixture

// newFixture makes the directory the shells complete in: files and
// directories with spaces and non-ASCII names, and an `enma` in PATH that
// records being run (a completion must not run it).
func newFixture(t *testing.T) *fixture {
	if fx != nil {
		return fx
	}
	root, err := os.MkdirTemp("", "enma-completion-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: filepath.Join(root, "work"), home: filepath.Join(root, "home"), sideEffect: filepath.Join(root, "enma-ran")}
	bin := filepath.Join(root, "bin")
	for _, d := range []string{f.dir, f.home, bin, filepath.Join(f.dir, "sub"), filepath.Join(f.dir, "sub dir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a.toml", "my file.toml", "日本語.txt", "x:y.toml"} {
		if err := os.WriteFile(filepath.Join(f.dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "enma"), []byte("#!/bin/sh\necho ran >>\"$ENMA_SIDE_EFFECT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	locale := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		locale = "en_US.UTF-8"
	}
	f.env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + f.home, "XDG_CONFIG_HOME=" + filepath.Join(f.home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(f.home, ".local", "share"), "XDG_CACHE_HOME=" + filepath.Join(f.home, ".cache"),
		"LANG=" + locale, "LC_ALL=" + locale, "TERM=dumb", "ENMA_SIDE_EFFECT=" + f.sideEffect,
	}
	fx = f
	return f
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fx != nil {
		os.RemoveAll(filepath.Dir(fx.dir))
	}
	os.Exit(code)
}

func runIn(t *testing.T, f *fixture, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = f.dir, f.env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\nstdout:\n%s\nstderr:\n%s", name, args, err, out.String(), errb.String())
	}
	if s := errb.String(); s != "" {
		t.Errorf("%s wrote to stderr:\n%s", name, s)
	}
	return out.String()
}

// splitCases splits output marked with <<CASE n>> lines into candidates.
func splitCases(t *testing.T, out string, n int, clean func(string) string) [][]string {
	t.Helper()
	res := make([][]string, n)
	cur := -1
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		if rest, ok := strings.CutPrefix(line, "<<CASE "); ok {
			i, err := strconv.Atoi(strings.TrimSuffix(rest, ">>"))
			if err != nil {
				t.Fatalf("bad marker %q", line)
			}
			cur = i
			continue
		}
		if cur < 0 || line == "" {
			continue
		}
		if c := clean(line); c != "" {
			res[cur] = append(res[cur], c)
		}
	}
	return res
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// bash: the completion function is called as bash calls it, with COMP_LINE
// and COMP_WORDS as readline fills them (TestBashReadline checks a few cases
// through readline itself).
// compWords splits shell words as bash 4 and later fill COMP_WORDS: a word
// is also split at each "=" and ":" (COMP_WORDBREAKS) outside quotes, each
// run of them a word of its own. bash 3.2 (split false) does not.
func compWords(words []string, split bool) []string {
	var out []string
	for _, w := range words {
		if !split {
			out = append(out, w)
			continue
		}
		var cur strings.Builder
		breaks := false // cur holds "=" and ":"
		var quote byte
		for i := 0; i < len(w); i++ {
			c := w[i]
			isBreak := quote == 0 && (c == '=' || c == ':')
			if cur.Len() > 0 && isBreak != breaks {
				out = append(out, cur.String())
				cur.Reset()
			}
			breaks = isBreak
			cur.WriteByte(c)
			switch {
			case quote != 0 && c == quote:
				quote = 0
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case c == '\\' && quote != '\'' && i+1 < len(w):
				i++
				cur.WriteByte(w[i])
			}
		}
		out = append(out, cur.String())
	}
	return out
}

func runBash(t *testing.T, sh *shell, script string, reqs []request) [][]string {
	f := newFixture(t)
	var b strings.Builder
	if sh.compopt {
		b.WriteString("compopt() { printf 'COMPOPT %s\\n' \"$*\"; }\n") // outside readline the builtin fails
	}
	fmt.Fprintf(&b, "source %s\n", shQuote(script))
	b.WriteString(`spec=$(complete -p enma)
run() {
  COMP_LINE=$1; COMP_POINT=${#COMP_LINE}; shift
  COMP_WORDS=("$@"); COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
  COMPREPLY=()
  _enma_bash enma "${COMP_WORDS[COMP_CWORD]}" "${COMP_WORDS[COMP_CWORD-1]}"
  [[ ${#COMPREPLY[@]} -eq 0 ]] || printf '%s\n' "${COMPREPLY[@]}"
}
echo "<<CASE 0>>"; echo "SPEC $spec"
set -u # a user's shell may have it
`)
	for i, r := range reqs {
		words := append([]string{"enma"}, r.words...)
		q := []string{shQuote(strings.Join(words, " "))}
		for _, w := range compWords(words, sh.compopt) {
			q = append(q, shQuote(w))
		}
		env := ""
		if r.windows {
			env = "OSTYPE=msys "
		}
		fmt.Fprintf(&b, "echo '<<CASE %d>>'; %srun %s\n", i+1, env, strings.Join(q, " "))
	}
	path := filepath.Join(t.TempDir(), "run.bash")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res := splitCases(t, runIn(t, f, sh.bin, "--noprofile", "--norc", path), len(reqs)+1, func(s string) string { return s })
	spec := strings.Join(res[0], "")
	if wantDefault := !sh.compopt; strings.Contains(spec, "-o default") != wantDefault {
		t.Errorf("%s: registered as %q (bash 3.2 needs -o default, later bash must not have it)", sh.name, spec)
	}
	return res[1:]
}

// zsh: an interactive zsh in a pseudo terminal (zpty) types each request and
// presses TAB; a compadd wrapper prints what the completion adds.
const zshDriver = `zmodload zsh/zpty || exit 2
setup=$1 reqs=$2
zpty z zsh -f -i
zpty -w z 'PS1= PROMPT= RPROMPT=; setopt no_beep; unsetopt zle_bracketed_paste 2>/dev/null'
zpty -w z "$setup"
zpty -w z 'compadd() {
  if [[ ${@[1,(i)(-|--)]} == *-(O|A|D)\ * ]]; then builtin compadd "$@"; return $?; fi
  local -a __hits; local -A __o
  builtin compadd -A __hits "$@"; local ret=$?
  zparseopts -E -A __o P: p: 2>/dev/null
  local h; for h in $__hits; do print -r -- "<<HIT $IPREFIX${__o[-P]}${__o[-p]}$h>>"; done
  return ret
}'
zpty -w z 'w_complete() { print -r -- "<<BE""GIN>>"; zle complete-word; print -r -- "<<E""ND>>" }; zle -N w_complete; bindkey "^I" w_complete'
zpty -w z "print -r -- '<<RE''ADY>>'"
zpty -r z o '*<<READY>>*' || exit 3
integer i=0
while IFS= read -r line; do
  (( i++ ))
  win=
  [[ $line == WIN* ]] && { win=1; zpty -w z 'OSTYPE=msys'; line=${line#WIN} }
  zpty -n -w z "$line"$'\t'
  out=; integer n=0
  while [[ $out != *'<<END>>'* ]] && (( n++ < 1500 )); do zpty -r -t z chunk && out+=$chunk || sleep 0.01; done
  [[ $out == *'<<END>>'* ]] || { print -r -- "TIMEOUT $i"; exit 4; }
  print -r -- "<<CASE $((i - 1))>>"
  print -r -- "${${out##*<<BEGIN>>}%%<<END>>*}" | tr -d '\r' | grep -o '<<HIT [^>]*>>' | sed 's/^<<HIT //; s/>>$//'
  zpty -n -w z $'\C-e\C-u'
  [[ -n $win ]] && zpty -w z 'OSTYPE=$__ostype'
done < "$reqs"
zpty -d z
`

func runZsh(t *testing.T, sh *shell, script string, reqs []request) [][]string {
	f := newFixture(t)
	dir := t.TempDir()
	driver := filepath.Join(dir, "driver.zsh")
	if err := os.WriteFile(driver, []byte(zshDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for _, r := range reqs {
		if r.windows {
			lines.WriteString("WIN")
		}
		lines.WriteString("enma " + strings.Join(r.words, " ") + "\n")
	}
	reqFile := filepath.Join(dir, "requests")
	if err := os.WriteFile(reqFile, []byte(lines.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var setup string
	if filepath.Base(script) == "_enma" {
		setup = fmt.Sprintf("__ostype=$OSTYPE; fpath=(%s $fpath); autoload -Uz compinit; compinit -u -D", shQuote(filepath.Dir(script)))
	} else { // a script to source after compinit
		setup = fmt.Sprintf("__ostype=$OSTYPE; autoload -Uz compinit; compinit -u -D; source %s", shQuote(script))
	}
	out := runIn(t, f, sh.bin, "-f", driver, setup, reqFile)
	return splitCases(t, out, len(reqs), unescapeZsh)
}

// unescapeZsh removes the backslashes zsh quotes special characters with.
func unescapeZsh(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		if r == '\\' && !esc {
			esc = true
			continue
		}
		esc = false
		b.WriteRune(r)
	}
	return b.String()
}

// fish: `complete -C` returns what fish offers for a command line.
func runFish(t *testing.T, sh *shell, script string, reqs []request) [][]string {
	f := newFixture(t)
	fq := func(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }
	var b strings.Builder
	fmt.Fprintf(&b, "source %s\n", fq(script))
	for i, r := range reqs {
		fmt.Fprintf(&b, "echo '<<CASE %d>>'\n", i)
		line := fq("enma " + strings.Join(r.words, " "))
		if r.windows {
			fmt.Fprintf(&b, "function uname; echo MINGW64_NT-10.0; end; complete -C %s; functions -e uname\n", line)
		} else {
			fmt.Fprintf(&b, "complete -C %s\n", line)
		}
	}
	path := filepath.Join(t.TempDir(), "run.fish")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runIn(t, f, sh.bin, "--no-config", path)
	return splitCases(t, out, len(reqs), func(s string) string { c, _, _ := strings.Cut(s, "\t"); return c })
}

// shells returns the shells to test, with the script each one loads.
func shells(t *testing.T) []*shell {
	t.Helper()
	required := strings.Fields(os.Getenv("ENMA_COMPLETION_SHELLS"))
	for _, name := range required {
		if name != "bash" && name != "zsh" && name != "fish" {
			t.Fatalf("ENMA_COMPLETION_SHELLS=%q: %q is not bash, zsh or fish (separate the names with spaces)", os.Getenv("ENMA_COMPLETION_SHELLS"), name)
		}
	}
	var out []*shell
	add := func(name, bin string, compopt bool, run func(*testing.T, *shell, string, []request) [][]string) {
		out = append(out, &shell{name: name, bin: bin, compopt: compopt, run: run})
	}
	for _, name := range []string{"bash", "zsh", "fish"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			if slices.Contains(required, name) {
				t.Errorf("%s not found, but ENMA_COMPLETION_SHELLS requires it", name)
			} else {
				t.Logf("NOT_VERIFIED: %s not installed", name)
			}
			continue
		}
		switch name {
		case "bash":
			add("bash", bin, bashHasCompopt(t, bin), runBash)
			if bin32 := "/bin/bash"; bin32 != bin { // macOS: bash 3.2
				if _, err := os.Stat(bin32); err == nil && !bashHasCompopt(t, bin32) {
					add("bash-3.2", bin32, false, runBash)
				}
			}
		case "zsh":
			add("zsh", bin, false, runZsh)
		case "fish":
			add("fish", bin, false, runFish)
		}
	}
	return out
}

func bashHasCompopt(t *testing.T, bin string) bool {
	out, err := exec.Command(bin, "--noprofile", "--norc", "-c", "type -t compopt").Output()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.TrimSpace(string(out)) == "builtin"
}

func (sh *shell) script() string {
	switch sh.name {
	case "zsh":
		return filepath.Join(compDir, "zsh", "_enma")
	case "fish":
		return filepath.Join(compDir, "fish", "enma.fish")
	}
	return filepath.Join(compDir, "bash", "enma-completion.bash")
}

// --- the expectations ----------------------------------------------------------

// An expectation checks the candidates of a request; it returns what is wrong.
type expectation struct {
	req   request
	check func(sh *shell, got []string) string
}

func exactly(want ...string) func(*shell, []string) string {
	return func(_ *shell, got []string) string {
		g, w := slices.Clone(got), slices.Clone(want)
		sort.Strings(g) // a candidate offered twice is a fault too (a script loaded twice)
		sort.Strings(w)
		if !slices.Equal(g, w) {
			return fmt.Sprintf("offers %q, want %q", g, w)
		}
		return ""
	}
}

func trimSlash(c []string) []string {
	var out []string
	for _, s := range c {
		out = append(out, strings.TrimSuffix(s, "/"))
	}
	return out
}

// pathCheck: the candidates of a file (dirsOnly: directory) value typed as
// word + list + "": word is "--flag=" for a value in the flag's word, list
// the items before the last comma of a comma-separated value.
func pathCheck(dirsOnly bool, word, list string) func(*shell, []string) string {
	return func(sh *shell, got []string) string {
		bash := strings.HasPrefix(sh.name, "bash")
		if bash && list == "" {
			// readline completes the path (quoting it); bash 3.2 offers files and directories
			want := []string{"COMPOPT -o default"}
			if dirsOnly {
				want = []string{"COMPOPT -o dirnames"}
			}
			if !sh.compopt {
				want = nil
			}
			return exactly(want...)(sh, got)
		}
		if sh.name == "bash-3.2" { // no compopt: no path after a comma
			return exactly()(sh, got)
		}
		prefix := word + list
		if bash {
			prefix = list // readline keeps what is before the "="
		}
		want := []string{prefix + "sub", prefix + "sub dir"}
		if !dirsOnly {
			want = append(want, prefix+"a.toml", prefix+"my file.toml", prefix+"日本語.txt", prefix+"x:y.toml")
		}
		if bash {
			want = append(want, "COMPOPT -o filenames -o nospace") // readline quotes the names
		}
		// the item already in the list may be offered again (zsh does not)
		again := func(c string) bool { return list != "" && c == prefix+strings.TrimSuffix(list, ",") }
		got = slices.DeleteFunc(trimSlash(got), again)
		want = slices.DeleteFunc(want, again)
		return exactly(want...)(sh, got)
	}
}

func valuesCheck(t *testing.T, k kind, windows bool, prefix string) func(*shell, []string) string {
	return func(_ *shell, got []string) string {
		var vals []string
		for _, g := range got {
			v, ok := strings.CutPrefix(g, prefix)
			if !ok {
				return fmt.Sprintf("%q does not start with %q", g, prefix)
			}
			vals = append(vals, v)
		}
		return checkValues(t, k, vals, windows)
	}
}

// valueRequests are the ways to type the value of flag: "--flag <TAB>" and
// "--flag=<TAB>" (Go's flag package takes -x=value too).
func valueRequests(sh *shell, words []string, name string) [][]string {
	return [][]string{append(slices.Clone(words), name, ""), append(slices.Clone(words), name+"=")}
}

func expectations(t *testing.T, sh *shell) []expectation {
	var out []expectation
	add := func(check func(*shell, []string) string, windows bool, words ...string) {
		out = append(out, expectation{request{words: words, windows: windows}, check})
	}
	// enma <TAB>: the subcommands; "enma s<TAB>": scheduler
	add(exactly(subcommands(t)...), false, "")
	add(exactly("scheduler"), false, "s")
	add(exactly("config"), false, "co")
	add(exactly(), false, "bogus", "-")
	actions, cflags := configWords(t)
	add(exactly(actions...), false, "config", "")
	add(exactly(cflags...), false, "config", "-")

	sets := flagSets(t)
	for cmd, fs := range sets {
		words := commandWords[cmd]
		var all []string
		fs.VisitAll(func(f *flag.Flag) { all = append(all, spelling(f.Name)) })
		// every spelling of every flag, and nothing else
		add(exactly(all...), false, append(slices.Clone(words), "-")...)
		if cmd == "general" {
			for _, name := range []string{"-v", "--version", "-h", "--help"} {
				add(func(sh *shell, got []string) string {
					if sh.name == "zsh" && len(got) == 0 {
						return ""
					}
					return exactly(all...)(sh, got)
				}, false, name, "-")
			}
			continue
		}
		// no subcommand takes an argument: nothing to offer (zsh lists the
		// flags where nothing else can go, as zsh completions do)
		add(func(sh *shell, got []string) string {
			if sh.name == "zsh" && len(got) > 0 {
				return exactly(all...)(sh, got)
			}
			return exactly()(sh, got)
		}, false, append(slices.Clone(words), "")...)
		// no flags after "--" or after an argument: enma does not parse them
		add(exactly(), false, append(slices.Clone(words), "--", "-")...)
		add(exactly(), false, append(slices.Clone(words), "arg", "-")...)
		for name, k := range flagKinds[cmd] {
			switch k {
			case kBool:
				// a bool flag takes no value: the next word is a flag again
				// (zsh offers nothing after --help or --version, which end the run)
				add(func(sh *shell, got []string) string {
					if sh.name == "zsh" && len(got) == 0 {
						return ""
					}
					return exactly(all...)(sh, got)
				}, false, append(slices.Clone(words), name, "-")...)
				continue
			case kText:
				add(exactly(), false, append(slices.Clone(words), name, "")...)
			case kCommand:
				// a command line starts with a command name
				add(func(_ *shell, got []string) string {
					if !slices.Contains(got, "echo") {
						return fmt.Sprintf("offers %q, want echo among them", got)
					}
					return ""
				}, false, append(slices.Clone(words), name, "ech")...)
			case kFile, kDir:
				add(pathCheck(k == kDir, "", ""), false, append(slices.Clone(words), name, "")...)
				if strings.HasPrefix(name, "--") {
					add(pathCheck(k == kDir, name+"=", ""), false, append(slices.Clone(words), name+"=")...)
				}
			case kFiles, kDirs:
				add(pathCheck(k == kDirs, "", ""), false, append(slices.Clone(words), name, "")...)
				add(pathCheck(k == kDirs, "", "a.toml,"), false, append(slices.Clone(words), name, "a.toml,")...)
				add(pathCheck(k == kDirs, name+"=", "a.toml,"), false, append(slices.Clone(words), name+"=a.toml,")...)
				// an item with quotes or a space before the comma
				add(pathCheck(k == kDirs, "", "sub dir,"), false, append(slices.Clone(words), name, `sub\ dir,`)...)
				add(pathCheck(k == kDirs, "", "sub dir,"), false, append(slices.Clone(words), name, `"sub dir,`)...)
			default: // a value list
				for _, req := range valueRequests(sh, words, name) {
					prefix := ""
					if cur := req[len(req)-1]; strings.HasSuffix(cur, "=") && sh.name != "bash" && sh.name != "bash-3.2" {
						prefix = cur // zsh and fish offer the whole word
					}
					add(valuesCheck(t, k, false, prefix), false, req...)
				}
				if k == kSignal {
					add(valuesCheck(t, k, true, ""), true, append(slices.Clone(words), name, "")...)
				}
			}
		}
	}
	// a flag's value is the next word, whatever it is (Go's flag package)
	add(exactly("--signal"), false, "hotload", "--daemon", "x", "--sig")
	add(exactly("SIGINT"), false, "hotload", "--daemon", "--build", "--signal", "SIGI")
	// a value holding "=" or ":" is one word (bash splits it in COMP_WORDS)
	add(exactly("--signal"), false, "hotload", "-x", "a=b", "--sig")
	add(exactly("--signal"), false, "hotload", "-W", "C:/x", "--sig")
	add(exactly("--signal"), false, "hotload", "-I={}", "--sig")
	// "-..." where a value goes is that value, not a flag
	add(exactly(), false, "hotload", "--pattern-regex", "-")
	// a short flag takes its value in the next word or after "=", never
	// attached (-SSIGTERM is an unknown flag to Go)
	add(func(_ *shell, got []string) string {
		for _, g := range got {
			if g != "-S" && g != "-s" { // fish 3 also offers -s (it ignores case)
				return fmt.Sprintf("offers %q, want -S", got)
			}
		}
		if !slices.Contains(got, "-S") {
			return fmt.Sprintf("offers %q, want -S", got)
		}
		return ""
	}, false, "hotload", "-S")
	// a path holding ":" (bash completes the part after it)
	add(func(sh *shell, got []string) string {
		switch sh.name {
		case "bash":
			return exactly("y.toml", "COMPOPT -o filenames -o nospace")(sh, got)
		case "bash-3.2":
			return exactly()(sh, got)
		}
		if sh.name == "fish" && slices.Contains(got, "x:y.toml") {
			// fish 3 also completes a new path after the ":" (as in $PATH)
			for _, g := range got {
				if !strings.HasPrefix(g, "x:") {
					return fmt.Sprintf("offers %q, want x:y.toml", got)
				}
			}
			return ""
		}
		return exactly("x:y.toml")(sh, got)
	}, false, "-c", "x:")
	// a directory typed up to a space (quoted or escaped), alone or after a comma
	for _, w := range []string{`sub\ d`, `"sub d`} {
		add(func(sh *shell, got []string) string {
			switch sh.name {
			case "bash":
				return exactly("COMPOPT -o dirnames")(sh, got)
			case "bash-3.2":
				return exactly()(sh, got)
			}
			return exactly("sub dir")(sh, trimSlash(got))
		}, false, "hotload", "-w", w)
		add(func(sh *shell, got []string) string {
			switch sh.name {
			case "bash":
				want := "a.toml,sub dir"
				if strings.HasPrefix(w, `"`) {
					want = "sub dir" // readline replaces what follows the open quote
				}
				return exactly(want, "COMPOPT -o filenames -o nospace")(sh, trimSlash(got))
			case "bash-3.2":
				return exactly()(sh, got)
			}
			return exactly("a.toml,sub dir")(sh, trimSlash(got))
		}, false, "hotload", "-E", "a.toml,"+w)
	}
	// a comma-separated list offers each part once
	add(exactly("dir,base", "dir,ext"), false, "watch", "-s", "dir,")
	add(func(sh *shell, got []string) string {
		w := "-s=" // zsh and fish offer the whole word
		if strings.HasPrefix(sh.name, "bash") {
			w = ""
		}
		return exactly(w+"dir,base", w+"dir,ext")(sh, got)
	}, false, "watch", "-s=dir,")
	return out
}

func runExpectations(t *testing.T, sh *shell, script string, exps []expectation) {
	t.Helper()
	reqs := make([]request, len(exps))
	for i, e := range exps {
		reqs[i] = e.req
	}
	results := sh.run(t, sh, mustAbs(t, script), reqs) // the shells run in the fixture directory
	for i, e := range exps {
		if msg := e.check(sh, results[i]); msg != "" {
			t.Errorf("%s %s: %s", sh.name, e.req, msg)
		}
	}
}

func TestShellCompletions(t *testing.T) {
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			runExpectations(t, sh, sh.script(), expectations(t, sh))
		})
	}
	if fx != nil {
		if _, err := os.Stat(fx.sideEffect); err == nil {
			t.Error("a completion ran enma")
		}
	}
}

// The combined script (for users who source one file in bash or zsh) works
// like the separate ones; loading a script twice changes nothing.
func TestCombinedAndReloadedCompletions(t *testing.T) {
	combined := filepath.Join(compDir, "enma-completion.sh")
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			exps := expectations(t, sh)
			var reqs []expectation
			for i, e := range exps {
				if i%7 == 0 { // a sample keeps it quick; TestShellCompletions runs all
					reqs = append(reqs, e)
				}
			}
			script := sh.script()
			if sh.name != "fish" {
				script = combined
			}
			runExpectations(t, sh, script, reqs)
			twice := filepath.Join(t.TempDir(), "twice")
			src := shQuote(mustAbs(t, sh.script()))
			if sh.name == "fish" {
				src = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(mustAbs(t, sh.script())) + "'"
			}
			body := "source " + src + "\nsource " + src + "\n"
			if sh.name == "fish" {
				// fish keeps completions across loads: a flag an older version
				// of the script offered must be gone after loading this one
				body = "complete -c enma -l stale-flag\n" + body
				reqs = exps
			}
			if err := os.WriteFile(twice, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			runExpectations(t, sh, twice, reqs)
		})
	}
}

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// combinedScript builds misc/completions/enma-completion.sh: the zsh script
// (without its #compdef line) for zsh, then the bash script.
func combinedScript(t *testing.T) string {
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(compDir, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	_, zshBody, ok := strings.Cut(read(filepath.Join("zsh", "_enma")), "\n")
	if !ok {
		t.Fatal("zsh/_enma: one line")
	}
	return `# Bash and zsh completion for enma: source this file in either shell.
# Built from zsh/_enma and bash/enma-completion.bash; edit those, then run
#   go test ./internal/commandline -run TestCombinedCompletionScript -update

# -------- Zsh Section --------
if [[ -n ${ZSH_VERSION-} ]]; then
` + zshBody + `  return 0
fi

# -------- Bash Section --------
` + read(filepath.Join("bash", "enma-completion.bash"))
}

func TestCombinedCompletionScript(t *testing.T) {
	path := filepath.Join(compDir, "enma-completion.sh")
	want := combinedScript(t)
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s differs from zsh/_enma + bash/enma-completion.bash; run: go test ./internal/commandline -run TestCombinedCompletionScript -update", path)
	}
}

// Each script parses in its shell (the combined one in bash and zsh; zsh/_enma
// in bash too, as the combined script carries it).
func TestCompletionScriptsParse(t *testing.T) {
	for _, sh := range shells(t) {
		files := []string{sh.script()}
		switch sh.name {
		case "bash", "bash-3.2":
			files = append(files, filepath.Join(compDir, "enma-completion.sh"), filepath.Join(compDir, "zsh", "_enma"))
		case "zsh":
			files = append(files, filepath.Join(compDir, "enma-completion.sh"))
		}
		for _, f := range files {
			if out, err := exec.Command(sh.bin, "-n", f).CombinedOutput(); err != nil || len(out) > 0 {
				t.Errorf("%s -n %s: %v\n%s", sh.name, f, err, out)
			}
		}
		if strings.HasPrefix(sh.name, "bash") { // also loaded by a bash in POSIX mode
			for _, f := range files[:2] {
				if out, err := exec.Command(sh.bin, "--posix", "--norc", "-c", `source "$1"`, "bash", f).CombinedOutput(); err != nil || len(out) > 0 {
					t.Errorf("%s --posix: source %s: %v\n%s", sh.name, f, err, out)
				}
			}
		}
	}
}

// Every script in misc/completions is in the release archives, checked by
// scripts/verify-dist.sh and installed by the Homebrew formula in the
// directory of its shell.
func TestCompletionsAreDistributed(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	goreleaser, verify := read(".goreleaser.yml"), read(filepath.Join("scripts", "verify-dist.sh"))
	var scripts []string
	err := filepath.WalkDir(compDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(compDir, p)
			scripts = append(scripts, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 4 {
		t.Fatalf("misc/completions holds %v; update this test (and the distribution) for a new script", scripts)
	}
	for _, s := range scripts {
		entry := "- src: misc/completions/" + s + "\n        dst: completions/" + s + "\n"
		if !strings.Contains(goreleaser, entry) {
			t.Errorf(".goreleaser.yml: archives do not hold %s (want %q)", s, entry)
		}
		if !strings.Contains(verify, "completions/"+s) {
			t.Errorf("scripts/verify-dist.sh does not check completions/%s", s)
		}
	}
	for _, line := range []string{
		`bash_completion.install "completions/bash/enma-completion.bash" => "enma"`,
		`zsh_completion.install "completions/zsh/_enma" => "_enma"`,
		`fish_completion.install "completions/fish/enma.fish"`,
	} {
		if !strings.Contains(goreleaser, line) {
			t.Errorf(".goreleaser.yml: the formula does not run %s", line)
		}
	}
}

// bashDriver types each request into an interactive bash (a zsh zpty drives
// it), pressing TAB at each @T, then Enter; `enma` is a shell function there
// that prints the words it gets. This is what the user gets after readline
// has inserted (and quoted) the completion.
const bashDriver = `zmodload zsh/zpty || exit 2
bin=$1 script=$2 reqs=$3
zpty b "$bin" --noprofile --norc -i
sleep 1
zpty -w b "PS1='\$ '; bind 'set bell-style none'; source ${(q)script}; set -u; enma() { printf '<<ARG %s>>\n' \"\$@\"; echo '<<DO''NE>>'; }; echo '<<RE''ADY>>'"
zpty -r b o '*<<READY>>*' || exit 3
float slow=${ENMA_PTY_SLOW:-1} # a slow machine: ENMA_PTY_SLOW=3
sleep $(( 0.5 * slow ))
integer i=0 k
while IFS= read -r keys; do
  local -a parts; parts=("${(@ps:@T:)keys}")
  for (( k = 1; k <= $#parts; k++ )); do
    zpty -n -w b "$parts[k]"; sleep $(( 0.3 * slow ))
    (( k < $#parts )) && { zpty -n -w b $'\t'; sleep $(( 0.7 * slow )) }
  done
  zpty -n -w b $'\n'
  out=; integer n=0
  while [[ $out != *'<<DONE>>'* ]] && (( n++ < 1000 )); do zpty -r -t b chunk && out+=$chunk || sleep 0.01; done
  print -r -- "<<CASE $i>>"
  [[ $out == *'<<DONE>>'* ]] || { print -r -- "NO RESULT"; zpty -n -w b $'\C-c'; sleep 0.5 }
  print -r -- "${out//$'\r'/}" | grep -o '<<ARG [^>]*>>' | sed 's/^<<ARG //; s/>>$//'
  (( i++ ))
done < "$reqs"
zpty -d b
`

// Through readline, bash completes paths with spaces, quotes and non-ASCII
// names, values after "=" and flags after a value holding "=" or ":".
func TestBashReadline(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("NOT_VERIFIED: needs zsh (zpty) to drive bash")
	}
	type tc struct {
		keys  string
		want  []string // the words enma gets
		bash4 bool     // bash 3.2 does not do it
	}
	cases := []tc{
		{"enma sch@T--ne@T3", []string{"scheduler", "--next", "3"}, false},
		{"enma init -f my@T", []string{"init", "-f", "my file.toml"}, false},
		{`enma init -f "my@T`, []string{"init", "-f", "my file.toml"}, false},
		{"enma init -f 日@T", []string{"init", "-f", "日本語.txt"}, false},
		{"enma config validate --config=my@T", []string{"config", "validate", "--config=my file.toml"}, false},
		{"enma hotload --signal=SIGK@T", []string{"hotload", "--signal=SIGKILL"}, false},
		{"enma hotload -x a=b --sig@T", []string{"hotload", "-x", "a=b", "--signal"}, false},
		{"enma hotload -W C:/x --sig@T", []string{"hotload", "-W", "C:/x", "--signal"}, false},
		{`enma hotload -W sub\ @T`, []string{"hotload", "-W", "sub dir/"}, true},
		{`enma hotload -w sub\ dir,su@T`, []string{"hotload", "-w", "sub dir,sub"}, true},
		{"enma hotload -n a.toml,my@T", []string{"hotload", "-n", "a.toml,my file.toml"}, true},
		{"enma -c x:@T", []string{"-c", "x:y.toml"}, true},
		// quotes and backslashes after "=" or a comma: readline replaces only
		// what follows them, the flag must not be inserted again
		{`enma config validate --config=my\ f@T`, []string{"config", "validate", "--config=my file.toml"}, false},
		{`enma --config="my f@T`, []string{"--config=my file.toml"}, false},
		{`enma hotload --signal="SIGT@T`, []string{"hotload", "--signal=SIGTERM"}, false},
		{`enma hotload -s=dir,"b@T`, []string{"hotload", "-s=dir,base"}, false},
		{`enma -c=x\:@T`, []string{"-c=x:y.toml"}, false},
		{`enma hotload -n a.toml,"my f@T`, []string{"hotload", "-n", "a.toml,my file.toml"}, true},
		// the cursor inside a word (four Ctrl-B): only what is before it counts
		{"enma hotload -s=dir,b,ext\x02\x02\x02\x02@T", []string{"hotload", "-s=dir,base,ext"}, false},
	}
	f := newFixture(t)
	for _, sh := range shells(t) {
		if !strings.HasPrefix(sh.name, "bash") {
			continue
		}
		t.Run(sh.name, func(t *testing.T) {
			var run []tc
			var lines strings.Builder
			for _, c := range cases {
				if c.bash4 && !sh.compopt {
					continue
				}
				run = append(run, c)
				lines.WriteString(c.keys + "\n")
			}
			dir := t.TempDir()
			driver, reqs := filepath.Join(dir, "driver.zsh"), filepath.Join(dir, "requests")
			if err := os.WriteFile(driver, []byte(bashDriver), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(reqs, []byte(lines.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := runIn(t, f, zsh, "-f", driver, sh.bin, mustAbs(t, sh.script()), reqs)
			got := splitCases(t, out, len(run), func(s string) string { return s })
			for i, c := range run {
				if !slices.Equal(got[i], c.want) {
					t.Errorf("%s: %q gives %q, want %q", sh.name, c.keys, got[i], c.want)
				}
			}
		})
	}
}
