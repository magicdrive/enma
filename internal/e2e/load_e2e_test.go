//go:build !windows

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Load on a running enma (H3 2.3): a burst of file changes, new directories and
// deletions. enma keeps handling changes, and once the files are gone its open
// descriptors come back down (kqueue holds one per watched file on macOS, so
// they rise with the files), i.e. watches of deleted files are not leaked.
//
// The two concerns are separate tests: TestE2E_Load_ManyChangesDoNotLeak
// (existing directories: every change handled, descriptors released) and
// TestE2E_Load_NewDirectoriesDuringBurst (directories created during the burst).

func fdCount(t *testing.T, pid int) int {
	t.Helper()
	if entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		return len(entries)
	}
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("precondition: cannot count descriptors (no /proc, lsof failed): %v", err)
	}
	return len(strings.Split(strings.TrimSpace(string(out)), "\n")) - 1 // header
}

func rssKiB(pid int) int {
	out, _ := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// kqueueNewDirRace reports whether this platform has the known limitation
// G4-04 (accepted and monitored, not fixed; owner 2026-10-18; README "What is
// watched"): with the kqueue backend of fsnotify (macOS, BSD), enma's watch of
// a new directory can race with fsnotify's own registration of it, so that
// files created in it afterwards are not watched (reproduced with fsnotify
// alone) and, presumably from the same race, one descriptor is kept after it
// is deleted. Not observed on Linux (inotify): there the test stays strict.
func kqueueNewDirRace() bool {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		return true
	}
	return false
}

// g404Run follows the known limitation G4-04 in one run of a test that
// creates directories while enma runs. observe records one of the two known
// symptoms (a file in such a directory not handled; descriptors of such deleted
// directories kept open) on a kqueue platform, where the test goes on, and
// fails anywhere else. When the test ends and $ENMA_G404_LOG is set (CI sets
// it), one line is appended to that file, observed=0 or observed=1, so that
// the rate can be followed across runs (CI prints the counts).
type g404Run struct {
	t        *testing.T
	observed []string
}

func newG404Run(t *testing.T) *g404Run {
	r := &g404Run{t: t}
	t.Cleanup(func() {
		path := os.Getenv("ENMA_G404_LOG")
		if path == "" {
			return // not recorded (a test does not write outside its temporary directories by default)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		n := 0
		if len(r.observed) > 0 {
			n = 1
		}
		fmt.Fprintf(f, "%s %s %s observed=%d %s\n", time.Now().Format(time.RFC3339), runtime.GOOS, t.Name(), n, strings.Join(r.observed, "; "))
	})
	return r
}

func (r *g404Run) observe(format string, args ...any) {
	r.t.Helper()
	msg := fmt.Sprintf(format, args...)
	if !kqueueNewDirRace() {
		r.t.Fatal(msg)
	}
	r.t.Logf("G4-04 (known limitation on kqueue, not a failure): %s", msg)
	r.observed = append(r.observed, msg)
}

// Bounds of the G4-04 allowance in one run of TestE2E_Load_NewDirectoriesDuringBurst
// (15 new directories): seen so far, at most 1 directory lost a file and 5
// descriptors were kept. Beyond these a regression in enma is more likely
// than the race, so the test fails on every platform.
const (
	g404MaxLostFiles = 3
	g404MaxKeptFDs   = 7
)

// A burst of changes in an existing directory (no directory is created): every
// change is handled, and the descriptors come back down once the files are gone.
func TestE2E_Load_ManyChangesDoNotLeak(t *testing.T) {
	proj, log := projectIn(t)
	p := startWatch(t, proj, "--watch-dir", "src", "--check-content-diff", "off")
	pid := p.cmd.Process.Pid
	writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "warmup.go"))
	fdStart, rssStart := fdCount(t, pid), rssKiB(pid)

	const files = 150
	src := filepath.Join(proj, "src")
	var paths []string
	for i := range files {
		paths = append(paths, filepath.Join(src, fmt.Sprintf("f%03d.go", i)))
	}
	for round := range 3 {
		for _, path := range paths {
			if err := os.WriteFile(path, []byte(fmt.Sprint(round)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := writeAndSync(t, p, log, nil, filepath.Join(src, "after-burst.go"))
	fdPeak, rssPeak := fdCount(t, pid), rssKiB(pid)
	for _, path := range paths {
		if !has(got, filepath.Base(path)) {
			t.Fatalf("the change of %s was not handled (%d handled)", path, len(got))
		}
	}

	fdEnd, rssEnd := removeAndCountFDs(t, src, pid, fdStart)
	t.Logf("changes handled=%d; fds start=%d after-burst=%d after-delete=%d; rss KiB start=%d after-burst=%d after-delete=%d",
		len(got), fdStart, fdPeak, fdEnd, rssStart, rssPeak, rssEnd)
	if fdEnd > fdStart+3 {
		t.Errorf("descriptors not released after the files were deleted: %d -> %d", fdStart, fdEnd)
	}
	stopUnderLoad(t, p)
}

// Directories created during a burst are watched, and the files in them are
// handled: the files written during the burst and, once the new directories'
// watches are reported, a further change of each. Their descriptors are
// released when they are deleted. On kqueue platforms the known limitation
// G4-04 can lose some of these files or keep a descriptor: it is recorded, and
// everything else is still checked.
func TestE2E_Load_NewDirectoriesDuringBurst(t *testing.T) {
	proj, log := projectIn(t)
	p := startWatch(t, proj, "--watch-dir", "src", "--check-content-diff", "off")
	pid := p.cmd.Process.Pid
	writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "warmup.go"))
	fdStart, filesStart := fdCount(t, pid), openFiles(t, pid)
	g404 := newG404Run(t)

	const files, dirs = 50, 15
	src := filepath.Join(proj, "src")
	var existing, inNew []string
	for i := range files {
		existing = append(existing, filepath.Join(src, fmt.Sprintf("f%03d.go", i)))
	}
	for d := range dirs {
		dir := filepath.Join(src, fmt.Sprintf("d%02d", d))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		inNew = append(inNew, filepath.Join(dir, fmt.Sprintf("x%02d.go", d))) // in a directory created while running
	}
	for round := range 3 {
		for _, path := range append(append([]string{}, existing...), inNew...) {
			if err := os.WriteFile(path, []byte(fmt.Sprint(round)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := writeAndSync(t, p, log, nil, filepath.Join(src, "after-burst.go"))
	for _, path := range existing { // the existing directory: no exception anywhere
		if !has(got, filepath.Base(path)) {
			t.Fatalf("the change of %s was not handled (%d handled)", path, len(got))
		}
	}
	lostInBurst := missing(got, inNew)
	if len(lostInBurst) > g404MaxLostFiles {
		t.Fatalf("changes in %d new directories lost during the burst (more than G4-04 explains): %v", len(lostInBurst), lostInBurst)
	} else if len(lostInBurst) > 0 {
		g404.observe("changes in new directories lost during the burst: %v", lostInBurst)
	}
	if !waitFor(10*time.Second, func() bool { return strings.Count(p.out.String(), "Watching new dir") >= dirs }) {
		t.Fatalf("not every new directory was watched:\n%s", p.out.String())
	}
	// only the changes handled from now on count (the change log keeps the
	// burst's lines, which would hide a file lost only now)
	before := len(changes(log))
	got = writeAndSync(t, p, log, inNew, filepath.Join(src, "after-dirs.go"))
	if lost := missing(got[before:], inNew); len(lost) > 0 {
		// G4-04 leaves a file unwatched for good: a file handled during the burst
		// and lost now is not that limitation
		if notG404 := lostOnlyLater(lostInBurst, lost); len(notG404) > 0 {
			t.Fatalf("handled during the burst but their later changes were not (not G4-04): %v (lost now %v, during the burst %v)", notG404, lost, lostInBurst)
		}
		g404.observe("changes in watched new directories not handled: %v", lost)
	}

	fdEnd, _ := removeAndCountFDs(t, src, pid, fdStart)
	if fdEnd > fdStart+3 {
		// only descriptors of the deleted new directories count as G4-04; any
		// other descriptor kept open is a leak and fails on every platform
		// (none identified, more than G4-04 explains, or any other descriptor: a leak)
		extra := keptDescriptors(t, pid, filesStart)
		if other := notNewDirDescriptors(extra); len(extra) == 0 || len(extra) > g404MaxKeptFDs || len(other) > 0 || !kqueueNewDirRace() {
			t.Fatalf("descriptors not released after the files were deleted: %d -> %d; kept: %v", fdStart, fdEnd, extra)
		}
		g404.observe("descriptors of deleted new directories kept open: %d -> %d: %v", fdStart, fdEnd, extra)
	}
	stopUnderLoad(t, p)
}

// newDirName matches a directory created during the burst (src/dNN), the only
// descriptors G4-04 can keep open.
var newDirName = regexp.MustCompile(`DIR .*/src/d[0-9]{2}$`)

// openFiles lists enma's open descriptors as "fd TYPE name" (lsof; on Linux
// /proc/<pid>/fd with the link target, type DIR for directories).
func openFiles(t *testing.T, pid int) map[string]string {
	t.Helper()
	files := map[string]string{}
	if entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		for _, e := range entries {
			target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, e.Name()))
			if err != nil {
				continue
			}
			kind := "OTHER"
			if fi, err := os.Stat(target); err == nil && fi.IsDir() {
				kind = "DIR"
			} else if strings.HasSuffix(target, " (deleted)") {
				kind, target = "DELETED", strings.TrimSuffix(target, " (deleted)") // a deleted file or directory
			}
			files[e.Name()] = kind + " " + target
		}
		return files
	}
	out, err := exec.Command("lsof", "-n", "-P", "-p", strconv.Itoa(pid), "-F", "ftn").Output()
	if err != nil {
		t.Fatalf("precondition: cannot list descriptors: %v", err)
	}
	var fd, kind string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'f':
			fd, kind = line[1:], ""
		case 't':
			kind = line[1:]
		case 'n':
			if fd != "" {
				files[fd] = kind + " " + line[1:]
			}
		}
	}
	return files
}

// lostOnlyLater returns the files lost in the later check that were handled
// during the burst: G4-04 cannot explain them (a file it leaves unwatched stays
// so).
func lostOnlyLater(lostInBurst, lostLater []string) []string {
	var only []string
	for _, f := range lostLater {
		if !slices.Contains(lostInBurst, f) {
			only = append(only, f)
		}
	}
	return only
}

// revokedName matches a descriptor lsof shows without type and path: on macOS
// the descriptor of a deleted directory can be shown so.
var revokedName = regexp.MustCompile(`^[0-9]+ +\(revoked\)$`)

// notNewDirDescriptors returns the descriptors that are not of a directory
// created during the burst: they are never G4-04. A "(revoked)" descriptor is
// taken as such a directory only next to at least one identified one (src/dNN);
// alone it cannot be attributed.
func notNewDirDescriptors(extra []string) []string {
	identified := false
	for _, f := range extra {
		if newDirName.MatchString(f) {
			identified = true
		}
	}
	var other []string
	for _, f := range extra {
		if newDirName.MatchString(f) || (identified && revokedName.MatchString(f)) {
			continue
		}
		other = append(other, f)
	}
	return other
}

// keptDescriptors returns the descriptors of pid not in start that stay open
// over three listings about half a second apart: a command still finishing
// holds pipes or /dev/null for a moment, which are not kept descriptors.
func keptDescriptors(t *testing.T, pid int, start map[string]string) []string {
	t.Helper()
	kept := newDescriptors(start, openFiles(t, pid))
	for range 2 {
		time.Sleep(500 * time.Millisecond)
		now := newDescriptors(start, openFiles(t, pid))
		var still []string
		for _, f := range kept {
			if slices.Contains(now, f) {
				still = append(still, f)
			}
		}
		kept = still
	}
	return kept
}

// newDescriptors returns the descriptors of after that are not the same in before.
// A descriptor that was open before with the same type is the same one unless
// it names a file or directory and that name changed (the names lsof gives a
// kqueue or a pipe change with their state).
func newDescriptors(before, after map[string]string) []string {
	var extra []string
	for fd, f := range after {
		old, ok := before[fd]
		if ok && old == f {
			continue
		}
		kind, _, _ := strings.Cut(f, " ")
		oldKind, _, _ := strings.Cut(old, " ")
		if ok && kind == oldKind && kind != "DIR" && kind != "REG" && kind != "DELETED" {
			continue
		}
		extra = append(extra, fd+" "+f)
	}
	slices.Sort(extra)
	return extra
}

func missing(got, paths []string) []string {
	var lost []string
	for _, path := range paths {
		if !has(got, filepath.Base(path)) {
			lost = append(lost, filepath.Base(path))
		}
	}
	return lost
}

// removeAndCountFDs deletes and recreates dir, then returns enma's descriptor
// count once it is back near start (or after 10s) and its RSS.
func removeAndCountFDs(t *testing.T, dir string, pid, start int) (fds, rss int) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// commands run for the deletions hold descriptors (pidfd on Linux) while they
	// run: wait for them to end before judging
	fds = fdCount(t, pid)
	waitFor(10*time.Second, func() bool { fds = fdCount(t, pid); return fds <= start+3 })
	return fds, rssKiB(pid)
}

func stopUnderLoad(t *testing.T, p *enmaProc) {
	t.Helper()
	if p.exited(0) {
		t.Fatalf("enma exited under load:\n%s", p.out.String())
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit %d after SIGINT", p.exitCode())
	}
}

// The G4-04 allowance covers only descriptors of directories created during the
// burst; anything else kept open is a leak.
func TestE2E_Load_G404AllowanceIsNarrow(t *testing.T) {
	newDir := "12 DIR /tmp/x/proj/src/d03"
	others := []string{
		"13 DIR /tmp/x/proj/src",            // the watch directory itself
		"14 DIR /tmp/x/proj/src/d03/sub",    // below a new directory
		"15 REG /tmp/x/proj/src/d03/x03.go", // a file
		"16 DIR /tmp/x/proj/src/dist",       // another directory
		"17 KQUEUE ",                        // not a path
		"18 DIR /tmp/x/proj/src/d3",         // not dNN
	}
	if got := notNewDirDescriptors([]string{newDir}); len(got) != 0 {
		t.Fatalf("a new directory's descriptor is the known symptom: %v", got)
	}
	if got := notNewDirDescriptors(append([]string{newDir}, others...)); len(got) != len(others) {
		t.Fatalf("only new directories may be allowed: got %v", got)
	}
	// a kqueue or pipe open before whose lsof name changed is not a new descriptor;
	// a directory or file whose name changed is
	before := map[string]string{"5": "KQUEUE count=0, state=0xa", "6": "PIPE ->0x1", "8": "DIR /p/src"}
	after := map[string]string{"5": "KQUEUE count=3, state=0x12", "6": "PIPE ->0x2", "8": "DIR /p/src/d01", "9": "DIR /p/src/d02"}
	if got := newDescriptors(before, after); !slices.Equal(got, []string{"8 DIR /p/src/d01", "9 DIR /p/src/d02"}) {
		t.Fatalf("newDescriptors: %v", got)
	}
	// a "(revoked)" descriptor (a deleted directory on macOS) counts only next to
	// an identified new directory
	if got := notNewDirDescriptors([]string{newDir, "27  (revoked)"}); len(got) != 0 {
		t.Fatalf("revoked next to a new directory: %v", got)
	}
	if got := notNewDirDescriptors([]string{"27  (revoked)"}); len(got) != 1 {
		t.Fatalf("a revoked descriptor alone cannot be attributed: %v", got)
	}
	// later losses: only files already lost during the burst can be G4-04
	if got := lostOnlyLater([]string{"x01.go"}, []string{"x01.go"}); len(got) != 0 {
		t.Fatalf("lost in both checks is G4-04: %v", got)
	}
	if got := lostOnlyLater([]string{"x01.go"}, []string{"x01.go", "x05.go"}); !slices.Equal(got, []string{"x05.go"}) {
		t.Fatalf("lost only later is not G4-04: %v", got)
	}
}

// openFiles reports a directory the process holds open as DIR with its path,
// on Linux (/proc) and on macOS / BSD (lsof).
func TestE2E_Load_OpenFilesSeesDirectories(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	d := filepath.Join(dir, "src", "d07")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	before := openFiles(t, os.Getpid())
	f, err := os.Open(d)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// (the test process itself also gains pipes to the lsof it runs: only the
	// directory is checked)
	extra := newDescriptors(before, openFiles(t, os.Getpid()))
	var found []string
	for _, e := range extra {
		if strings.HasSuffix(e, "DIR "+d) {
			found = append(found, e)
		}
	}
	if len(found) != 1 || len(notNewDirDescriptors(found)) != 0 {
		t.Fatalf("the open directory %s: extra %v", d, extra)
	}
}
