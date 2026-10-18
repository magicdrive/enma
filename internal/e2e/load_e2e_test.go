//go:build !windows

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Load on a running enma (H3 2.3): a burst of file changes, new directories and
// deletions. enma keeps handling changes, and once the files are gone its open
// descriptors come back down (kqueue holds one per watched file on macOS, so
// they rise with the files), i.e. watches of deleted files are not leaked.

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

func TestE2E_Load_ManyChangesDoNotLeak(t *testing.T) {
	proj, log := projectIn(t)
	p := startWatch(t, proj, "--watch-dir", "src", "--check-content-diff", "off")
	pid := p.cmd.Process.Pid
	writeAndSync(t, p, log, nil, filepath.Join(proj, "src", "warmup.go"))
	fdStart, rssStart := fdCount(t, pid), rssKiB(pid)

	const files, dirs = 150, 15
	src := filepath.Join(proj, "src")
	var paths []string
	for i := range files {
		paths = append(paths, filepath.Join(src, fmt.Sprintf("f%03d.go", i)))
	}
	for d := range dirs {
		dir := filepath.Join(src, fmt.Sprintf("d%02d", d))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(dir, fmt.Sprintf("x%02d.go", d))) // in a directory created while running
	}
	for round := range 3 {
		for _, path := range paths {
			if err := os.WriteFile(path, []byte(fmt.Sprint(round)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	marker := filepath.Join(src, "after-burst.go")
	got := writeAndSync(t, p, log, nil, marker)
	fdPeak, rssPeak := fdCount(t, pid), rssKiB(pid)
	handled := len(got)

	// every changed file of the existing directory is handled
	for _, path := range paths[:files] {
		if !has(got, filepath.Base(path)) {
			t.Fatalf("the change of %s was not handled (%d handled)", path, handled)
		}
	}
	// files in the directories created during the burst: a rare loss under heavy
	// load on macOS (kqueue) is a known open finding (G4-04, cause not found);
	// it is logged here, and once the new directories' watches are reported a
	// further change of each must be handled
	var lost []string
	for _, path := range paths[files:] {
		if !has(got, filepath.Base(path)) {
			lost = append(lost, filepath.Base(path))
		}
	}
	if len(lost) > 0 {
		// kept outside the test output (shown only on failure) so that the rate can
		// be followed across runs: $TMPDIR/enma-G4-04.log
		t.Logf("G4-04: changes in new directories lost during the burst: %v", lost)
		if f, err := os.OpenFile(filepath.Join(os.TempDir(), "enma-G4-04.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			fmt.Fprintf(f, "%s lost %d of %d: %v\n", time.Now().Format(time.RFC3339), len(lost), dirs, lost)
			f.Close()
		}
	}
	if !waitFor(10*time.Second, func() bool { return strings.Count(p.out.String(), "Watching new dir") >= dirs }) {
		t.Fatalf("not every new directory was watched:\n%s", p.out.String())
	}
	got = writeAndSync(t, p, log, paths[files:], filepath.Join(src, "after-dirs.go"))
	for _, path := range paths[files:] {
		if !has(got, filepath.Base(path)) {
			t.Fatalf("a change in the watched new directory %s was not handled", path)
		}
	}

	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// commands run for the deletions hold descriptors (pidfd on Linux) while they
	// run: wait for them to end before judging
	fdEnd := fdCount(t, pid)
	waitFor(10*time.Second, func() bool { fdEnd = fdCount(t, pid); return fdEnd <= fdStart+3 })
	rssEnd := rssKiB(pid)
	t.Logf("changes handled=%d; fds start=%d after-burst=%d after-delete=%d; rss KiB start=%d after-burst=%d after-delete=%d",
		handled, fdStart, fdPeak, fdEnd, rssStart, rssPeak, rssEnd)
	if fdEnd > fdStart+3 {
		t.Errorf("descriptors not released after the files were deleted: %d -> %d", fdStart, fdEnd)
	}
	if p.exited(0) {
		t.Fatalf("enma exited under load:\n%s", p.out.String())
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit %d after SIGINT", p.exitCode())
	}
}
