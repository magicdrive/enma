//go:build !windows

package ignorerule_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/ignorerule"
)

// File names with a backslash (possible outside Windows only): "\\" in a
// pattern escapes the next character, as in gitignore.
func TestGitIgnore_AuditRound2_BackslashNames(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines(`bs\\\ `, `two\\ `)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{`bs\ `: true, `two\`: true, `two\ `: false} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}
