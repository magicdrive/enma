//go:build !windows

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the watch modes watch (owner 2026-10-17: fix all of F02 F03 F09 F10 F13 F14):
//   W1 rules look at paths inside the project: a project below .enma/, vendor/
//      or tmp/ is watched like any other (F02), while the rules still apply inside it;
//   W2 .enmaignore follows .gitignore: character classes, anchors (F03); only the
//      project's own .enmaignore is read, not one in a parent directory;
//   W3 --working-dir without --watch-dir watches the working directory, not the
//      directory enma was started in (F09);
//   W4 a watch config without watch_dir watches the working directory (F10);
//   W5 an unreadable subdirectory is reported and left out; the rest is watched (F13);
//   W6 a symlink created at runtime that points outside the watch directories is
//      not followed (F14).

// changes returns the base names of the files the command was run for.
func changes(log string) []string {
	var out []string
	for _, l := range lines(log) {
		out = append(out, filepath.Base(l))
	}
	return out
}

func has(list []string, name string) bool {
	for _, x := range list {
		if x == name {
			return true
		}
	}
	return false
}

// startWatch runs `enma watch` in dir with the command logging each changed
// path, and waits until monitoring has started.
func startWatch(t *testing.T, dir string, extra ...string) *enmaProc {
	t.Helper()
	args := append([]string{"watch", "--command", "echo {} >> '" + filepath.Join(dir, "..", "changes.log") + "'", "--delay", "0sec"}, extra...)
	p := startEnma(t, dir, nil, args...)
	if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), "Start file monitor daemon") }) {
		t.Fatalf("monitoring did not start:\n%s", p.out.String())
	}
	return p
}

// write changes path and, as a marker that everything before it was handled,
// waits for the file marker (written after it) to be reported.
func writeAndSync(t *testing.T, p *enmaProc, log string, paths []string, marker string) []string {
	t.Helper()
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(time.Now().String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(400 * time.Millisecond) // past the debounce: the marker is handled after them
	if err := os.WriteFile(marker, []byte(time.Now().String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, func() bool { return has(changes(log), filepath.Base(marker)) }) {
		t.Fatalf("the marker change was not handled; changes %v\n%s", changes(log), p.out.String())
	}
	return changes(log)
}

// projectIn makes dir/<parents...>/proj and returns proj and the change log.
func projectIn(t *testing.T, parents ...string) (proj, log string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	proj = filepath.Join(append(append([]string{root}, parents...), "proj")...)
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, filepath.Join(proj, "..", "changes.log")
}

func TestE2E_WatchScope_ProjectBelowIgnoredNames(t *testing.T) {
	for name, tc := range map[string]struct {
		parents []string
		extra   []string
	}{
		"below .enma (minimal ignores)": {[]string{".enma"}, nil},
		"below .git":                    {[]string{".git", "x"}, nil},
		"below vendor with -E vendor":   {[]string{"vendor"}, []string{"-E", "vendor"}},
		"below tmp with -D max":         {[]string{"tmp"}, []string{"-D", "max"}},
	} {
		t.Run(name, func(t *testing.T) {
			proj, log := projectIn(t, tc.parents...)
			if err := os.MkdirAll(filepath.Join(proj, ".enma"), 0o755); err != nil {
				t.Fatal(err)
			}
			p := startWatch(t, proj, tc.extra...)
			got := writeAndSync(t, p, log, []string{
				filepath.Join(proj, "src", "a.go"),
				filepath.Join(proj, ".enma", "inside.go"), // the defaults still apply inside the project
			}, filepath.Join(proj, "src", "marker.go"))
			if !has(got, "a.go") {
				t.Errorf("W1 violated: a change in the project was not handled: %v", got)
			}
			if has(got, "inside.go") {
				t.Errorf("W1 violated: .enma/ inside the project was not ignored: %v", got)
			}
			p.interrupt(t)
		})
	}
}

func TestE2E_WatchScope_EnmaignoreLikeGitignore(t *testing.T) {
	proj, log := projectIn(t)
	ignore := "*.py[cod]\n/top.txt\n"
	if err := os.WriteFile(filepath.Join(proj, ".enmaignore"), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	// a parent's .enmaignore is not the project's
	if err := os.WriteFile(filepath.Join(proj, "..", ".enmaignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startWatch(t, proj)
	got := writeAndSync(t, p, log, []string{
		filepath.Join(proj, "src", "m.pyc"),
		filepath.Join(proj, "src", "m.py"),
		filepath.Join(proj, "top.txt"),
		filepath.Join(proj, "src", "top.txt"),
		filepath.Join(proj, "src", "a.go"),
	}, filepath.Join(proj, "src", "marker.txt"))
	for name, want := range map[string]bool{"m.pyc": false, "m.py": true, "a.go": true} {
		if has(got, name) != want {
			t.Errorf("W2 violated: %s handled=%v, want %v (%v)", name, !want, want, got)
		}
	}
	n := 0
	for _, c := range got {
		if c == "top.txt" {
			n++
		}
	}
	if n != 1 { // /top.txt ignores the project's top.txt only, not src/top.txt
		t.Errorf("W2 violated: top.txt handled %d times, want 1 (src/top.txt only): %v", n, got)
	}
	p.interrupt(t)
}

func TestE2E_WatchScope_WorkingDirIsTheDefaultWatchDir(t *testing.T) {
	proj, log := projectIn(t)
	sub := filepath.Join(proj, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// started in proj, working in proj/sub: only sub is watched
	args := []string{"watch", "--working-dir", "sub", "--command", "echo {} >> '" + log + "'", "--delay", "0sec"}
	p := startEnma(t, proj, nil, args...)
	if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), "Start file monitor daemon") }) {
		t.Fatalf("monitoring did not start:\n%s", p.out.String())
	}
	got := writeAndSync(t, p, log, []string{filepath.Join(proj, "src", "outside.go")}, filepath.Join(sub, "marker.go"))
	if has(got, "outside.go") {
		t.Errorf("W3 violated: the directory enma was started in is watched: %v", got)
	}
	p.interrupt(t)
}

func TestE2E_WatchScope_TomlWithoutWatchDir(t *testing.T) {
	proj, log := projectIn(t)
	conf := "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"echo {} >> '" + log + "'\"\ndelay = \"0sec\"\n"
	if err := os.WriteFile(filepath.Join(proj, "Enma.toml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, proj, nil)
	if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), "Start file monitor daemon") }) {
		t.Fatalf("monitoring did not start:\n%s", p.out.String())
	}
	got := writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "marker.go"))
	if !has(got, "marker.go") {
		t.Errorf("W4 violated: %v", got)
	}
	p.interrupt(t)
}

func TestE2E_WatchScope_UnreadableSubdirIsSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("precondition: run the tests as a non-root user (root can read a directory with mode 000)")
	}
	proj, log := projectIn(t)
	locked := filepath.Join(proj, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	p := startWatch(t, proj)
	if !strings.Contains(p.out.String(), "Cannot read "+locked) {
		t.Errorf("W5 violated: the unreadable directory was not reported:\n%s", p.out.String())
	}
	if strings.Contains(p.out.String(), "Failed to watch "+locked) {
		t.Errorf("W5 violated: the unreadable directory was still handed to the watcher:\n%s", p.out.String())
	}
	got := writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "marker.go"))
	if !has(got, "marker.go") {
		t.Errorf("W5 violated: %v", got)
	}
	p.interrupt(t)
}

func TestE2E_WatchScope_SymlinkOutsideIsNotFollowed(t *testing.T) {
	proj, log := projectIn(t)
	outside := filepath.Join(proj, "..", "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	p := startWatch(t, proj, "--watch-dir", "src")
	if err := os.Symlink(outside, filepath.Join(proj, "src", "link")); err != nil {
		t.Fatal(err)
	}
	if !waitFor(5*time.Second, func() bool { return strings.Contains(p.out.String(), "points outside the watch directories") }) {
		t.Errorf("W6 violated: no report about the outside symlink:\n%s", p.out.String())
	}
	got := writeAndSync(t, p, log, []string{filepath.Join(outside, "o.go")}, filepath.Join(proj, "src", "marker.go"))
	if has(got, "o.go") {
		t.Errorf("W6 violated: a directory outside the watch directories is watched: %v", got)
	}
	p.interrupt(t)
}

// A directory created while enma runs is watched from then on, in both modes.
func TestE2E_WatchScope_NewDirectoriesAreWatched(t *testing.T) {
	for _, mode := range []string{"watch", "hotload"} {
		t.Run(mode, func(t *testing.T) {
			proj, log := projectIn(t)
			cmd := "echo {} >> '" + log + "'"
			args := []string{"watch", "--command", cmd, "--delay", "0sec", "--watch-dir", "src"}
			ready := "Start file monitor daemon"
			if mode == "hotload" {
				args = []string{"hotload", "--build", cmd, "--daemon", "sleep 30", "--build-at-start", "off", "--delay", "0sec", "--watch-dir", "src"}
				ready = "👀 Watching " + filepath.Join(proj, "src")
			}
			p := startEnma(t, proj, nil, args...)
			if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), ready) }) {
				t.Fatalf("monitoring did not start:\n%s", p.out.String())
			}
			nested := filepath.Join(proj, "src", "new", "deeper")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			if !waitFor(5*time.Second, func() bool { return strings.Contains(p.out.String(), "Watching new dir") }) {
				t.Fatalf("the new directory was not reported:\n%s", p.out.String())
			}
			got := writeAndSync(t, p, log, nil, filepath.Join(nested, "inside-new.go"))
			if !has(got, "inside-new.go") {
				t.Fatalf("a change in a new directory was not handled: %v", got)
			}
			// a file written at once, before enma has added the new directory's watch
			// (its event cannot reach enma): still handled
			quick := filepath.Join(proj, "src", "quick")
			if err := os.Mkdir(quick, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(quick, "early.go"), []byte("1"), 0o644); err != nil {
				t.Fatal(err)
			}
			if !waitFor(10*time.Second, func() bool { return has(changes(log), "early.go") }) {
				t.Fatalf("a file written into a new directory before its watch was missed: %v", changes(log))
			}
			p.interrupt(t)
		})
	}
}

// A new directory that the watch rules reject (.enmaignore `dist` without a
// slash, --ignore-dir-regex) is neither watched nor are its files taken as
// changes (audit A4R2-01: a build recreating its output must not loop), in both
// modes (audit H4R3-01).
func TestE2E_WatchScope_NewIgnoredDirectoryIsLeftAlone(t *testing.T) {
	for _, mode := range []string{"watch", "hotload"} {
		t.Run(mode, func(t *testing.T) {
			proj, log := projectIn(t)
			if err := os.WriteFile(filepath.Join(proj, ".enmaignore"), []byte("dist\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := "echo {} >> '" + log + "'"
			args := []string{"watch", "--command", cmd, "--delay", "0sec", "--watch-dir", "src", "--ignore-dir-regex", "/gen$"}
			ready := "Start file monitor daemon"
			if mode == "hotload" {
				args = []string{"hotload", "--build", cmd, "--daemon", "sleep 30", "--build-at-start", "off", "--delay", "0sec", "--watch-dir", "src", "--ignore-dir-regex", "/gen$"}
				ready = "👀 Watching " + filepath.Join(proj, "src")
			}
			p := startEnma(t, proj, nil, args...)
			if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), ready) }) {
				t.Fatalf("monitoring did not start:\n%s", p.out.String())
			}
			for _, d := range []string{"dist", "gen", filepath.Join("ok", "dist")} {
				dir := filepath.Join(proj, "src", d)
				if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
				for _, f := range []string{filepath.Join(dir, "in-"+filepath.Base(d)+".go"), filepath.Join(dir, "sub", "deep-"+filepath.Base(d)+".go")} {
					if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "marker.go"))
			for _, name := range []string{"in-dist.go", "deep-dist.go", "in-gen.go", "deep-gen.go"} {
				if has(got, name) {
					t.Errorf("a file in an ignored new directory was taken as a change: %s (%v)", name, got)
				}
			}
			p.interrupt(t)
		})
	}
}
