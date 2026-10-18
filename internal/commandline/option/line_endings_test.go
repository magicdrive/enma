package option_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// W1: the files that are embedded into enma or shipped with it keep LF line
// endings on every OS (.gitattributes). A CR breaks the completion scripts
// (bash/zsh syntax errors) and the help and README checks of this package.
func TestShippedTextIsLF(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	checked := 0
	check := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if i := bytes.IndexByte(b, '\r'); i >= 0 {
			t.Errorf("%s has a CR at byte %d: check out with LF (.gitattributes: * text=auto eol=lf)", path, i)
		}
	}
	check(filepath.Join(root, "README.md"))
	for _, dir := range []string{filepath.Join(root, "misc", "completions"), filepath.Join(root, "internal", "textbank")} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && filepath.Ext(path) != ".go" {
				check(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 5 {
		t.Fatalf("only %d files checked", checked)
	}
}
