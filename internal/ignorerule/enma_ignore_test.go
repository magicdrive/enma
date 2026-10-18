package ignorerule_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/enma/internal/ignorerule"
	"github.com/magicdrive/enma/internal/model"
)

func TestGitIgnore_Matches(t *testing.T) {
	patterns := []string{
		"*.log",
		"/build/",
		"temp/**",
		"docs/**/*.md",
		"!docs/README.md",
		`\!special.txt`,
		"dir with space/",
		"*.tmp",
		"node_modules/",
		"**/generated/*",
		"a/**/b/*.txt",
	}

	gi, err := ignorerule.CompileIgnoreLines(patterns...)
	if err != nil {
		t.Fatalf("failed to compile patterns: %v", err)
	}

	tests := []struct {
		path   string
		expect bool
		match  string
	}{
		{"debug.log", true, "*.log"},
		{"build/main.o", true, "/build/"},
		{"temp/cache/file.txt", true, "temp/**"},
		{"docs/chapter1/intro.md", true, "docs/**/*.md"},
		{"docs/README.md", false, "!docs/README.md"},
		{"!special.txt", true, "\\!special.txt"},
		{"dir with space/file.txt", true, "dir with space/"},
		{"src/main.go", false, ""},
		{"foo.tmp", true, "*.tmp"},
		{"node_modules/pkg/index.js", true, "node_modules/"},
		{"src/generated/code.go", true, "**/generated/*"},
		{"a/b/file.txt", true, "a/**/b/*.txt"},
		{"a/x/b/file.txt", true, "a/**/b/*.txt"},
		{"a/x/y/b/file.txt", true, "a/**/b/*.txt"},
		{"a/b/file.md", false, ""},
		{"docs/image.png", false, ""},
	}

	for _, tt := range tests {
		matched, pat := gi.MatchesPathHow(tt.path)
		if matched != tt.expect {
			t.Errorf("Matches(%q) = %v; want %v", tt.path, matched, tt.expect)
		} else if matched && pat != nil && pat.Raw != tt.match {
			t.Errorf("Matches(%q) matched pattern %q; want %q", tt.path, pat.Raw, tt.match)
		}
	}
}

func TestGitIgnore_ExtraCases(t *testing.T) {
	patterns := []string{
		"*.log",
		"temp-?.txt",
		"/secrets.txt",
		"logs/*.log",
		"!keep.log",
		"docs/**/README.md",
		"build/",
		"*.swp",
		"**/*.bak",
		"lib/**/test/*.go",
	}
	gi, err := ignorerule.CompileIgnoreLines(patterns...)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]bool{
		"debug.log":           true,
		"temp-a.txt":          true,
		"secrets.txt":         true,
		"src/secrets.txt":     false,
		"logs/error.log":      true,
		"keep.log":            false,
		"docs/README.md":      true,
		"docs/api/README.md":  true,
		"build/index.html":    true,
		"build/js/app.js":     true,
		"main.go.swp":         true,
		".vimrc.swp":          true,
		"tmp/old.bak":         true,
		"src/foo/bar.bak":     true,
		"lib/test/foo.go":     true,
		"lib/x/test/bar.go":   true,
		"lib/x/y/test/baz.go": true,
	}

	for path, want := range tests {
		got := gi.Matches(path)
		if got != want {
			t.Errorf("Matches(%q) = %v; want %v", path, got, want)
		}
	}
}

// F03: the pattern language follows .gitignore where it used to differ:
// character classes, escapes, and anchoring by a leading or middle slash.
// Paths are relative to the project directory.
func TestGitIgnore_GitSemantics(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines(
		"*.py[cod]",
		"[!a]x.txt",
		"/gen.go",
		"doc/frotz",
		`\#hash`,
		`\!bang`,
		`star\*`,
		"cache/",
		"**/deep/*.out",
		"v?.bin",
		"[abc",
		"x[]y.md",
	)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"a.pyc": true, "pkg/a.pyo": true, "a.pyd": true, "a.py": false, "a.pyx": false,
		"bx.txt": true, "ax.txt": false, "d/bx.txt": true,
		"gen.go": true, "sub/gen.go": false, // leading slash: the project directory only
		"doc/frotz": true, "a/doc/frotz": false, // a middle slash anchors too
		"#hash": true, "!bang": true, "star*": true, "starx": false,
		"cache": true, "cache/x/y": true, "src/cache/z": true, "cachex": false,
		"deep/a.out": true, "x/y/deep/a.out": true, "deep/z/a.out": false,
		"v1.bin": true, "v12.bin": false, "v/.bin": false,
		"[abc": true, "a": false,
		"x[]y.md": true,
	} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

// The defaults match inside the project, not on the path leading to it (F02):
// patterns see paths relative to the project directory.
func TestGitIgnore_DefaultsOnRelativePaths(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines(".enma/", ".git/", "*.log", "tmp/")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"src/main.go": false, "main.go": false,
		".enma/enma.log": true, ".enma/run/x": true, ".git/HEAD": true, "sub/.git/config": true,
		"tmp/a": true, "a.log": true,
	} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

// Only the working directory's .enmaignore is read: one in a parent directory
// (another project, the home directory) is not, even when the project has none.
func TestGenerateIntegratedEnmaIgnore_NoParentSearch(t *testing.T) {
	parent := t.TempDir()
	proj := filepath.Join(parent, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".enmaignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)
	gi := ignorerule.GenerateIntegratedEnmaIgnore(proj, model.IgnoreType(model.Nothing), nil)
	if gi.Matches("main.go") {
		t.Fatal("the parent directory's .enmaignore was read")
	}
	if err := os.WriteFile(filepath.Join(proj, ".enmaignore"), []byte("*.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gi = ignorerule.GenerateIntegratedEnmaIgnore(proj, model.IgnoreType(model.Nothing), nil)
	if !gi.Matches("notes.txt") || gi.Matches("main.go") {
		t.Fatal("the working directory's .enmaignore was not read (alone)")
	}
}

// Audit H3 (A3-01, A3-02, A3-03): characters in a class are literal, an invalid
// line is skipped alone, an escaped trailing space is kept, and the last
// matching pattern decides even when a line repeats.
func TestGitIgnore_AuditCases(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines(
		`[\d]x`,
		"[z-a]bad",
		"after-bad.txt",
		`[.]dot`,
		`sp\ `,
		"rep",
		"!rep",
		"rep",
	)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"dx": true, "5x": false, // \d is the letter d, not a digit class
		"after-bad.txt": true, // lines after an invalid one still apply
		".dot":          true, "adot": false,
		"sp ": true, "sp": false,
		"rep": true, // ignore, un-ignore, ignore: the last one wins
	} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

// Audit H3 round 2 (A3R2-01, A3R2-04): `x/**` matches what is inside x, not x
// itself, so a directory below it can be un-ignored; trailing backslashes are
// counted to decide whether the trailing space is escaped.
func TestGitIgnore_AuditRound2(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines("a/**", "!a/keep/", "!a/keep/**", `bs\\\ `, `two\\ `)
	if err != nil {
		t.Fatal(err)
	}
	// names holding a backslash exist only outside Windows, where "\\" is a
	// path separator: enma_ignore_unix_test.go and enma_ignore_internal_test.go
	for path, want := range map[string]bool{
		"a": false, "a/x": true, "a/keep": false, "a/keep/f.go": false,
	} {
		if got := gi.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

// `!dir/` re-includes dir and everything below it, as the README says (audit H3R3-01).
func TestNegatedDirReincludesItsContents(t *testing.T) {
	gi, err := ignorerule.CompileIgnoreLines("*.log", "!logs/")
	if err != nil {
		t.Fatal(err)
	}
	for path, ignored := range map[string]bool{"a.log": true, "logs": false, "logs/a.log": false, "logs/sub/b.log": false, "other/c.log": true} {
		if got := gi.Matches(path); got != ignored {
			t.Errorf("%s: ignored=%v, want %v", path, got, ignored)
		}
	}
}
