package shellcmd_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/shellcmd"
)

// A-08: HasPlaceholder must agree with what Script actually substitutes.
var placeholderCases = []struct {
	name, command, placeholder string
	want                       bool
}{
	{"unquoted", "go build {}", "{}", true},
	{"double quoted", `echo "{}"`, "{}", true},
	{"single quoted", `echo '{}'`, "{}", true},
	{"ansi quoted", `echo $'{}'`, "{}", true},
	{"command substitution", `echo $(basename {})`, "{}", true},
	{"unquoted heredoc", "cat <<EOF\n{}\nEOF", "{}", true},
	{"custom placeholder", "go build @@", "@@", true},
	{"one real among ignored", "echo \\{} {} # {}", "{}", true},
	{"no placeholder", "go build ./...", "{}", false},
	{"escaped", `echo \{}`, "{}", false},
	{"comment only", "go build . # rebuild {}", "{}", false},
	{"quoted heredoc", "cat <<'EOF'\n{}\nEOF", "{}", false},
	{"double-quoted heredoc delimiter", "cat <<\"EOF\"\n{}\nEOF", "{}", false},
	{"empty placeholder", "go build {}", "", false},
	{"empty command", "", "{}", false},
}

func TestHasPlaceholder(t *testing.T) {
	for _, c := range placeholderCases {
		t.Run(c.name, func(t *testing.T) {
			if got := shellcmd.HasPlaceholder(c.command, c.placeholder); got != c.want {
				t.Fatalf("HasPlaceholder(%q, %q) = %v, want %v", c.command, c.placeholder, got, c.want)
			}
			if got := shellcmd.Script(c.command, c.placeholder) != c.command; got != c.want {
				t.Fatalf("Script(%q, %q) substituted = %v, want %v", c.command, c.placeholder, got, c.want)
			}
		})
	}
}

func FuzzHasPlaceholderMatchesScript(f *testing.F) {
	for _, c := range placeholderCases {
		f.Add(c.command, c.placeholder)
	}
	f.Fuzz(func(t *testing.T, command, placeholder string) {
		has := shellcmd.HasPlaceholder(command, placeholder)
		substituted := shellcmd.Script(command, placeholder) != command
		if has != substituted {
			t.Fatalf("HasPlaceholder=%v but Script substituted=%v for %q / %q", has, substituted, command, placeholder)
		}
	})
}
