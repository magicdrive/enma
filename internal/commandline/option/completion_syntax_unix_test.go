//go:build !windows

package option_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The completion scripts parse in the shells that source them: the combined
// script is read by bash and zsh alike, so zsh-only syntax in its zsh part must
// not break bash (it did: `[[ $OSTYPE == (a|b) ]]`).
func TestCompletionScriptsParse(t *testing.T) {
	root := filepath.Join("..", "..", "..", "misc", "completions")
	checks := map[string][]string{
		"bash": {filepath.Join(root, "bash", "enma-completion.bash"), filepath.Join(root, "enma-completion.sh")},
		"zsh":  {filepath.Join(root, "zsh", "_enma"), filepath.Join(root, "enma-completion.sh")},
	}
	for shell, files := range checks {
		bin, err := exec.LookPath(shell)
		if err != nil {
			if shell == "bash" {
				t.Fatalf("bash not found: %v", err)
			}
			t.Logf("NOT CHECKED: %s not installed", shell) // zsh is optional on Linux
			continue
		}
		for _, f := range files {
			if out, err := exec.Command(bin, "-n", f).CombinedOutput(); err != nil {
				t.Errorf("%s -n %s: %v\n%s", shell, f, err, out)
			}
		}
	}
}

// The bash completion offers what fits the position: `enma config <TAB>` offers
// validate, a file after --config / -c / -f, the flags after `config validate`
// (audit H5R2-03). bash-completion's _init_completion is stubbed.
func TestBashCompletionOfConfig(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs(filepath.Join("..", "..", "..", "misc", "completions"))
	script := `_init_completion() { cur=${COMP_WORDS[COMP_CWORD]}; prev=${COMP_WORDS[COMP_CWORD-1]}; words=("${COMP_WORDS[@]}"); cword=$COMP_CWORD; }
complete() { :; }
source "$1"
comp() { COMP_WORDS=("$@"); COMP_CWORD=$((${#COMP_WORDS[@]}-1)); COMPREPLY=(); _enma_bash; echo "${COMPREPLY[*]}"; }
comp enma config ""
comp enma config validate ""
comp enma config validate -c my
comp enma config validate --config my
comp enma ""
`
	for _, f := range []string{filepath.Join(root, "bash", "enma-completion.bash"), filepath.Join(root, "enma-completion.sh")} {
		cmd := exec.Command(bash, "-c", script, "bash", f)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", f, err, out)
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("%s: output %q", f, out)
		}
		has := func(line int, words ...string) {
			got := " " + lines[line] + " "
			for _, w := range words {
				if !strings.Contains(got, " "+w+" ") {
					t.Errorf("%s: case %d offers %q, missing %q", f, line, lines[line], w)
				}
			}
		}
		has(0, "validate")
		if strings.Contains(lines[0], "--config") {
			t.Errorf("%s: `enma config <TAB>` offers flags before the action: %q", f, lines[0])
		}
		has(1, "--config", "-c", "-f", "--next", "--help")
		has(2, "my.toml")
		has(3, "my.toml")
		has(4, "config", "scheduler", "init")
	}
}
