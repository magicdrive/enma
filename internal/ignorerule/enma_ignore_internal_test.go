package ignorerule

import "testing"

// The escape rules of patterns on every OS, Windows included: the patterns are
// checked against slash paths directly, without Matches (which turns the OS
// separator into "/").
func TestPatternBackslashEscapes(t *testing.T) {
	gi, err := CompileIgnoreLines(`bs\\\ `, `two\\ `, `a\*b`)
	if err != nil {
		t.Fatal(err)
	}
	match := func(p string) bool {
		m := false
		for _, pat := range gi.patterns {
			if pat.Regexp.MatchString(p) {
				m = !pat.Negate
			}
		}
		return m
	}
	for path, want := range map[string]bool{`bs\ `: true, `bs\`: false, `two\`: true, `two\ `: false, `a*b`: true, `axb`: false} {
		if got := match(path); got != want {
			t.Errorf("pattern match %q = %v, want %v", path, got, want)
		}
	}
}
