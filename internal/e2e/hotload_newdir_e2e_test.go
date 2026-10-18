//go:build !windows

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// PRA-02: in hotload, a directory that appears while enma runs (moved in,
// unpacked, checked out) with files already in it is one change. Without a
// placeholder in the build commands the build and the daemon restart run once
// for it, not once per file; with a placeholder each file keeps its own build
// (the command's meaning depends on the path). Files ignored by the watch rules
// do not count, and changes after the directory was picked up are built as
// before.
//
// Synchronisation, no sleeps: changes are handled by one callback at a time,
// and while it runs no new change can join it. enma logs "Change confirmed in:
// <path>" (--check-content-diff on, the default) when it handles a change; once
// a line for the new directory is there, a marker file written into src is a
// later change, and when the marker's daemon restart has happened everything
// before it was handled. The daemon appends its pid to daemon.pids when its
// shell runs; the build command appends a line to build.log. A daemon can be
// stopped by the next restart before its shell wrote its pid, so starts are
// counted from enma's log ("Start daemon"), not from daemon.pids.

type hotloadNewDir struct {
	t                 *testing.T
	dir, src, stage   string
	buildLog, daemons string
	p                 *enmaProc
	markers           int
}

func startHotloadNewDir(t *testing.T, build, enmaignore string, extra ...string) *hotloadNewDir {
	t.Helper()
	return startHotloadNewDirDaemon(t, "", build, enmaignore, extra...)
}

// startHotloadNewDirDaemon starts enma with daemonPrefix run by the daemon's
// shell before it records its pid (e.g. a delay).
func startHotloadNewDirDaemon(t *testing.T, daemonPrefix, build, enmaignore string, extra ...string) *hotloadNewDir {
	t.Helper()
	dir := isolated(t, "")
	h := &hotloadNewDir{t: t, dir: dir, src: filepath.Join(dir, "src"), stage: filepath.Join(dir, "stage"),
		buildLog: filepath.Join(dir, "build.log"), daemons: filepath.Join(dir, "daemon.pids")}
	for _, d := range []string{h.src, h.stage} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if enmaignore != "" {
		if err := os.WriteFile(filepath.Join(dir, ".enmaignore"), []byte(enmaignore), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := append([]string{"hotload", "--watch-dir", "src",
		"--build", strings.ReplaceAll(build, "LOG", "'"+h.buildLog+"'"),
		"--daemon", daemonPrefix + "echo $$ >> '" + h.daemons + "'; exec sleep 600",
		"--delay", "0sec", "--timeout", "60sec", "--check-content-diff", "on"}, extra...)
	h.p = startEnma(t, dir, nil, args...)
	if !waitFor(15*time.Second, func() bool { return len(lines(h.daemons)) == 1 }) {
		t.Fatalf("the daemon did not start:\n%s", h.p.out.String())
	}
	return h
}

// stageDir makes a directory with files outside the watch directory.
func (h *hotloadNewDir) stageDir(name string, files ...string) {
	h.t.Helper()
	root := filepath.Join(h.stage, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		h.t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
}

// moveIn renames a staged directory into src: one Create event for the
// directory, none for the files already in it. It returns the new path.
func (h *hotloadNewDir) moveIn(name string) string {
	h.t.Helper()
	dst := filepath.Join(h.src, name)
	if err := os.Rename(filepath.Join(h.stage, name), dst); err != nil {
		h.t.Fatal(err)
	}
	return dst
}

// handled reports whether enma's log has a line handling a change at or below
// path ("Change confirmed in: <path>..." or "Skipped: ... <path>...").
func (h *hotloadNewDir) handled(path string) bool {
	rel, _ := filepath.Rel(h.dir, path)
	for _, l := range strings.Split(h.p.out.String(), "\n") {
		i := strings.Index(l, "Change confirmed in: ")
		j := strings.Index(l, "Skipped: ")
		if i < 0 && j < 0 {
			continue
		}
		for _, p := range []string{path, rel} {
			if strings.Contains(l, " "+p+" ") || strings.HasSuffix(l, " "+p) || strings.Contains(l, " "+p+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// waitHandled waits until enma handles a change of each path (or below it).
func (h *hotloadNewDir) waitHandled(paths ...string) {
	h.t.Helper()
	for _, p := range paths {
		if !waitFor(60*time.Second, func() bool { return h.handled(p) }) {
			h.t.Fatalf("no change at %s was handled:\n%s", p, h.p.out.String())
		}
	}
}

// settle waits for the changes of paths to be handled, then writes a marker
// into src and waits for its restart. It returns the builds and daemon starts
// up to then, the marker's own excluded.
func (h *hotloadNewDir) settle(paths ...string) (builds, starts int) {
	h.t.Helper()
	h.waitHandled(paths...)
	h.markers++
	marker := filepath.Join(h.src, fmt.Sprintf("marker%d.go", h.markers))
	if err := os.WriteFile(marker, []byte("m"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.waitHandled(marker)
	// The marker's daemon is the newest: enma starts a daemon only after the
	// previous one's group is gone, so the last recorded pid being alive means
	// the marker's daemon runs (and wrote its pid).
	if !waitFor(180*time.Second, func() bool {
		out := h.p.out.String()
		i := strings.LastIndex(out, "Change confirmed in: ")
		if i < 0 || !strings.Contains(out[i:], "Start daemon") {
			return false
		}
		pids := lines(h.daemons)
		if len(pids) == 0 {
			return false
		}
		var pid int
		fmt.Sscan(pids[len(pids)-1], &pid)
		return alive(pid)
	}) {
		out := h.p.out.String()
		h.t.Fatalf("no restart after the marker: %d \"Start daemon\" lines, daemon.pids %q, builds %q\n%s",
			strings.Count(out, "Start daemon"), lines(h.daemons), lines(h.buildLog), out)
	}
	return len(lines(h.buildLog)) - 1, strings.Count(h.p.out.String(), "Start daemon") - 1
}

func (h *hotloadNewDir) stop() {
	h.t.Helper()
	h.p.interrupt(h.t)
	if code := h.p.exitCode(); code != 0 {
		h.t.Fatalf("exit %d after SIGINT:\n%s", code, h.p.out.String())
	}
}

func goFiles(n int) []string {
	var fs []string
	for i := range n {
		fs = append(fs, fmt.Sprintf("f%03d.go", i))
	}
	return fs
}

// T1: a directory with 120 files: one build and one restart (PRA-02 was 121).
func TestE2E_HotloadNewDir_ManyFilesBuildOnce(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "")
	h.stageDir("pkg", goFiles(120)...)
	dst := h.moveIn("pkg")
	builds, starts := h.settle(dst)
	if builds != 2 || starts != 2 { // the build at start + one for the directory
		t.Fatalf("PRA-02: a new directory with 120 files gave %d builds and %d daemon starts in all, want 2 and 2 (start + one for the directory)", builds, starts)
	}
	h.stop()
}

// T2: with a placeholder in the build command each file keeps its own build,
// with its own path.
func TestE2E_HotloadNewDir_PlaceholderKeepsPerFileBuilds(t *testing.T) {
	h := startHotloadNewDir(t, "echo {} >> LOG", "")
	names := []string{"a.go", "b.go", "sub/c.go"}
	h.stageDir("pkg", names...)
	dst := h.moveIn("pkg")
	var want []string
	for _, f := range names {
		want = append(want, filepath.Join(dst, f))
	}
	h.settle(want...)
	got := lines(h.buildLog)
	for _, p := range want {
		rel, _ := filepath.Rel(h.dir, p)
		if n := countOf(got, p) + countOf(got, rel); n != 1 {
			t.Fatalf("placeholder: %s built %d times, want 1; builds: %v", p, n, got)
		}
	}
	h.stop()
}

// macOS shuffle failure (run 39125047156): restarts that follow each other
// stop a daemon before its shell recorded its pid. The delay makes that happen
// every time; the test must still settle and count every start.
func TestE2E_HotloadNewDir_DaemonStoppedBeforeItsPidIsWritten(t *testing.T) {
	h := startHotloadNewDirDaemon(t, "sleep 0.3; ", "echo {} >> LOG", "")
	h.stageDir("pkg", "a.go", "b.go", "sub/c.go")
	dst := h.moveIn("pkg")
	_, starts := h.settle(filepath.Join(dst, "a.go"), filepath.Join(dst, "b.go"), filepath.Join(dst, "sub", "c.go"))
	if starts != 5 { // the first, then a.go, b.go, c.go and the directory itself
		t.Fatalf("%d daemon starts before the marker, want 5", starts)
	}
	if n := len(lines(h.daemons)); n >= starts+1 {
		t.Logf("every daemon recorded its pid (%d): the delay did not take effect", n)
	}
	h.stop()
}

func countOf(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

// T3: a directory holding only ignored files (an excluded extension, an
// .enmaignore pattern, an ignored subdirectory) builds nothing.
func TestE2E_HotloadNewDir_OnlyIgnoredFilesBuildNothing(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "*.log\nskip/\n", "--exclude-ext", ".tmp")
	h.stageDir("junk", "a.tmp", "b.log", "deep/c.tmp", "skip/d.go")
	dst := h.moveIn("junk")
	builds, starts := h.settle(dst)
	if builds != 1 || starts != 1 {
		t.Fatalf("a new directory of ignored files: %d builds and %d daemon starts in all, want 1 and 1 (the build at start only)", builds, starts)
	}
	h.stop()
}

// T4: after the directory was handled, a change of one of its files is built
// as usual: the batch does not swallow later changes.
func TestE2E_HotloadNewDir_LaterChangesAreBuilt(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "")
	h.stageDir("pkg", goFiles(20)...)
	dst := h.moveIn("pkg")
	h.settle(dst)
	changed := filepath.Join(dst, "f005.go")
	if err := os.WriteFile(changed, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	builds, starts := h.settle(changed)
	if builds != 4 || starts != 4 { // start, directory, first marker, the change
		t.Fatalf("a later change in the new directory: %d builds and %d daemon starts in all, want 4 and 4", builds, starts)
	}
	h.stop()
}

// T5: nested directories with files at every level: one build.
func TestE2E_HotloadNewDir_NestedOnce(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "")
	var fs []string
	for _, d := range []string{"", "a/", "a/b/", "a/b/c/", "x/"} {
		for i := range 6 {
			fs = append(fs, fmt.Sprintf("%sf%d.go", d, i))
		}
	}
	h.stageDir("tree", fs...)
	dst := h.moveIn("tree")
	builds, starts := h.settle(dst)
	if builds != 2 || starts != 2 {
		t.Fatalf("nested new directories: %d builds and %d daemon starts in all, want 2 and 2", builds, starts)
	}
	h.stop()
}

// T6: an empty directory builds nothing.
func TestE2E_HotloadNewDir_EmptyBuildsNothing(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "")
	h.stageDir("empty")
	dst := h.moveIn("empty")
	builds, starts := h.settle(dst)
	if builds != 1 || starts != 1 {
		t.Fatalf("an empty new directory: %d builds and %d daemon starts in all, want 1 and 1 (the build at start only)", builds, starts)
	}
	h.stop()
}

// T7: a shutdown while a large directory is being handled stops enma cleanly:
// it exits 0, starts no daemon afterwards and leaves none running.
func TestE2E_HotloadNewDir_ShutdownWhileHandling(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG; sleep 0.2", "")
	h.stageDir("pkg", goFiles(150)...)
	dst := h.moveIn("pkg")
	h.waitHandled(dst)
	h.stop()
	pids := lines(h.daemons)
	if len(pids) > 2 {
		t.Fatalf("%d daemon starts: more than the first one and one for the directory", len(pids))
	}
	for _, s := range pids {
		var pid int
		fmt.Sscan(s, &pid)
		if alive(pid) {
			t.Fatalf("daemon %d survived the shutdown", pid)
		}
	}
	if b := len(lines(h.buildLog)); b > 2 {
		t.Fatalf("%d builds: the shutdown did not stop the handling of the directory", b)
	}
}

// T8: two different directories moved in together are two changes: one build
// each, neither swallowed by the other.
func TestE2E_HotloadNewDir_TwoDirectoriesTwoBuilds(t *testing.T) {
	h := startHotloadNewDir(t, "echo build >> LOG", "")
	h.stageDir("one", goFiles(10)...)
	h.stageDir("two", goFiles(10)...)
	one := h.moveIn("one")
	two := h.moveIn("two")
	builds, starts := h.settle(one, two)
	if builds != 3 || starts != 3 { // start + one per directory
		t.Fatalf("two new directories: %d builds and %d daemon starts in all, want 3 and 3", builds, starts)
	}
	h.stop()
}

// B-02: without a placeholder, the one build of a new directory gets the
// directory as $1 (README), not one of its files.
func TestE2E_HotloadNewDir_DollarOneIsTheDirectory(t *testing.T) {
	h := startHotloadNewDir(t, `echo "[$1]" >> LOG`, "")
	h.stageDir("pkg", goFiles(5)...)
	dst := h.moveIn("pkg")
	h.settle(dst)
	rel, _ := filepath.Rel(h.dir, dst)
	got := lines(h.buildLog) // "[]" (start), the directory, the marker
	if len(got) != 3 || (got[1] != "["+dst+"]" && got[1] != "["+rel+"]") {
		t.Fatalf("builds %v: want the start, [%s] and the marker", got, rel)
	}
	h.stop()
}
