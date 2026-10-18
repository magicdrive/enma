package common_test

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/common"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

func TestPidFile_HoldsExactlyThePid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "deep", "enma.pid")
	if err := common.CreatePidFile(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != strconv.Itoa(os.Getpid()) { // format unchanged: the decimal pid, nothing else
		t.Fatalf("pid file holds %q, want %q", b, strconv.Itoa(os.Getpid()))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary files left beside the pid file: %v", entries)
	}
	if st, _ := os.Stat(path); st.Mode().Perm()&0o022 != 0 {
		t.Fatalf("pid file is group/world writable: %v", st.Mode())
	}
}

func TestPidFile_DeleteOnlyOwnPid(t *testing.T) {
	own := strconv.Itoa(os.Getpid())
	for name, tc := range map[string]struct {
		content string
		removed bool
	}{
		"own":                {own, true},
		"own with newline":   {own + "\n", true},
		"own padded":         {"  " + own + " \n", true},
		"longer pid":         {own + "7", false}, // held our pid as a substring before the fix
		"prefixed pid":       {"1" + own, false},
		"another pid":        {"1", false},
		"pid among text":     {"enma " + own, false},
		"empty":              {"", false},
		"not a number":       {"abc", false},
		"two pids":           {own + "\n" + own, false},
		"negative":           {"-" + own, false},
		"own pid, then junk": {own + "x", false},
	} {
		path := filepath.Join(t.TempDir(), "enma.pid")
		if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := common.DeletePidFile(path); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, err := os.Stat(path)
		if removed := os.IsNotExist(err); removed != tc.removed {
			t.Errorf("%s: removed=%v, want %v", name, removed, tc.removed)
		}
	}
}

func TestPidFile_DeleteMissingIsNoError(t *testing.T) {
	if err := common.DeletePidFile(filepath.Join(t.TempDir(), "none.pid")); err != nil {
		t.Fatal(err)
	}
}

func TestPidFile_ReplacesStaleAndWarnsAboutLiveOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enma.pid")
	buf := captureLog(t)

	// a stale file (a pid nobody has: beyond the usual pid range) is replaced silently
	if err := os.WriteFile(path, []byte("2147483646"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := common.CreatePidFile(path); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "still running") {
		t.Fatalf("warned about a dead pid: %s", buf.String())
	}

	// a file naming a live process (our parent) is replaced with a warning
	parent := os.Getppid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(parent)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := common.CreatePidFile(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "names process "+strconv.Itoa(parent)+", which is still running") {
		t.Fatalf("no warning about the live owner: %q", buf.String())
	}
	if b, _ := os.ReadFile(path); string(b) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("pid file holds %q", b)
	}
	// recreating our own file does not warn about ourselves
	buf.Reset()
	if err := common.CreatePidFile(path); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("warned about our own pid: %q", buf.String())
	}
}

func TestPidFile_ErrorsNameThePath(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "enma.pid") // parent is a regular file
	err := common.CreatePidFile(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("got %v, want an error naming %s", err, path)
	}
}
