package model_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/model"
)

// Windows paths as enma passes them (filepath.Abs / filepath.Rel): drive
// roots, UNC shares, relative paths and mixed separators.
func TestArgsPathString_WindowsPaths(t *testing.T) {
	for _, c := range []struct{ style, path, want string }{
		{"d", `C:\file.txt`, `C:\`},
		{"d,b,e", `C:\file.txt`, `C:\file.txt`},
		{"d", `\\srv\share\a.txt`, `\\srv\share\`},
		{"d", `src\a.go`, `src\`},
		{"d,b,e", `C:/Users/a.txt`, `C:\Users\a.txt`},
		{"b,e", `C:\Users\a b.txt`, `a b.txt`},
	} {
		var s model.ArgsPathStyleString
		if err := s.Set(c.style); err != nil {
			t.Fatal(err)
		}
		obj, err := s.ArgsPathStyleObj()
		if err != nil {
			t.Fatal(err)
		}
		if got := obj.ArgsPathString(c.path); got != c.want {
			t.Errorf("style=%q path=%q: got %q, want %q", c.style, c.path, got, c.want)
		}
	}
}
