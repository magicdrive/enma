package ignorerule_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/ignorerule"
)

// On Windows enma passes paths with "\\" separators: Matches turns them into
// "/" before matching (W6).
func TestGitIgnore_WindowsSeparators(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines("a/**", "!a/keep/", "!a/keep/**")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{`a\x`: true, `a\keep`: false, `a\keep\f.go`: false, `b\a\x`: false} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}
