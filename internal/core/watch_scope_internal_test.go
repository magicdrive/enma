package core

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep, "x", "Proj")
	for _, tc := range []struct {
		path, rel string
		ok        bool
	}{
		{root, ".", true},
		{filepath.Join(root, "src", "a.go"), "src/a.go", true},
		{filepath.Join(sep, "x", "Projects", "a.go"), "", false}, // a sibling with the same prefix
		{filepath.Join(sep, "x"), "", false},
	} {
		rel, ok := within(root, tc.path)
		if rel != tc.rel || ok != tc.ok {
			t.Errorf("within(%q, %q) = %q, %v; want %q, %v", root, tc.path, rel, ok, tc.rel, tc.ok)
		}
	}
	// A3-04: macOS and Windows file systems usually ignore case
	rel, ok := within(root, filepath.Join(sep, "x", "PROJ", "Src", "a.go"))
	if insensitive := runtime.GOOS == "darwin" || runtime.GOOS == "windows"; ok != insensitive || (ok && rel != "Src/a.go") {
		t.Errorf("case-different path: %q, %v (case-insensitive platform: %v)", rel, ok, insensitive)
	}
}
