//go:build !windows

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// eventually polls cond until it holds or d has passed.
func eventually(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

type inode struct{ dev, ino uint64 }

func inodeOf(t *testing.T, path string) inode {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatal(err)
	}
	return inode{uint64(st.Dev), uint64(st.Ino)}
}

// openOn returns this process's descriptors open on one of the inodes.
func openOn(inodes map[inode]string) []string {
	var open []string
	for fd := 0; fd < 4096; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) != nil {
			continue
		}
		if name, ok := inodes[inode{uint64(st.Dev), uint64(st.Ino)}]; ok {
			open = append(open, fmt.Sprintf("%d:%s", fd, name))
		}
	}
	return open
}

// The way the runners use the watcher (Start's event loop), on a real watcher:
// directories created in a burst, with files written into them, are watched
// (a later write in each is reported), and once they are deleted no descriptor
// of the watcher is left on them or on the working directory. On kqueue, an
// Add of a new directory made as soon as its Create arrives races with
// fsnotify's own registration of it and kept descriptors in nearly every round
// (fsnotify v1.10.1, fsnotify/fsnotify#783); see newDirWatches.
func TestNewDirWatches_BurstOfNewDirsKeepsNoDescriptor(t *testing.T) {
	const rounds, dirs = 5, 15
	for round := range rounds {
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(base, "src")
		if err := os.Mkdir(src, 0o755); err != nil {
			t.Fatal(err)
		}
		// the working directory: where a lost descriptor's Create of "." leads
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		cwdBefore := len(openOn(map[inode]string{inodeOf(t, cwd): "."}))

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		if err := watcher.Add(src); err != nil {
			t.Fatal(err)
		}
		env := &startEnv{}
		newDirs := newNewDirWatches(watcher, env)
		if kqueueBackend() && newDirs.fence == "" {
			t.Fatal("no fence on a kqueue platform")
		}

		var mu sync.Mutex
		added := map[string]bool{}
		written := map[string]bool{} // directories whose x.go write was reported
		ignored := 0
		add := func(dir string) {
			if err := watcher.Add(dir); err != nil {
				t.Errorf("Add %s: %v", dir, err)
				return
			}
			mu.Lock()
			added[dir] = true
			mu.Unlock()
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}
					if newDirs.received(event, add) {
						mu.Lock()
						ignored++
						mu.Unlock()
						continue
					}
					if event.Has(fsnotify.Create) {
						if fi, err := os.Lstat(event.Name); err == nil && fi.IsDir() {
							newDirs.created(event.Name, add)
						}
					}
					if filepath.Base(event.Name) == "x.go" && event.Has(fsnotify.Write) {
						mu.Lock()
						written[filepath.Dir(event.Name)] = true
						mu.Unlock()
					}
				case _, ok := <-watcher.Errors:
					if !ok {
						return
					}
					newDirs.flush(add)
				}
			}
		}()

		var newDirPaths []string
		inodes := map[inode]string{}
		for d := range dirs {
			dir := filepath.Join(src, fmt.Sprintf("d%02d", d))
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			newDirPaths = append(newDirPaths, dir)
			inodes[inodeOf(t, dir)] = filepath.Base(dir)
		}
		for i := range 3 {
			for _, dir := range newDirPaths {
				if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(fmt.Sprint(i)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !eventually(10*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(added) == dirs
		}) {
			mu.Lock()
			t.Fatalf("round %d: %d of %d new directories were added", round, len(added), dirs)
		}
		// each new directory is watched: a write after its watch is reported
		mu.Lock()
		clear(written)
		mu.Unlock()
		for _, dir := range newDirPaths {
			if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("later"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if !eventually(5*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(written) == dirs
		}) {
			mu.Lock()
			var missed []string
			for _, dir := range newDirPaths {
				if !written[dir] {
					missed = append(missed, filepath.Base(dir))
				}
			}
			mu.Unlock()
			// the other symptom of the race, not prevented by newDirWatches (a
			// later registration of a seen directory can still drop NOTE_WRITE):
			// logged, not failed, see README "What is watched"
			t.Logf("round %d: a later write was not reported in %v", round, missed)
		}

		if err := os.RemoveAll(src); err != nil {
			t.Fatal(err)
		}
		// fsnotify closes a watch's descriptor when its Remove arrives
		var kept []string
		if !eventually(5*time.Second, func() bool { kept = openOn(inodes); return len(kept) == 0 }) {
			t.Errorf("round %d: descriptors kept on deleted new directories: %v", round, kept)
		}
		if n := len(openOn(map[inode]string{inodeOf(t, cwd): "."})); n > cwdBefore {
			t.Errorf("round %d: %d descriptors opened on the working directory", round, n-cwdBefore)
		}

		if err := watcher.Close(); err != nil {
			t.Fatal(err)
		}
		<-done
		env.runAtExit()
		if newDirs.fence != "" {
			if _, err := os.Stat(newDirs.fence); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the fence %s is left after exit: %v", newDirs.fence, err)
			}
		}
		if t.Failed() {
			return
		}
	}
}

// received adds the queued directories before anything else, and ignores the
// fence's event and events without an absolute path (a lost descriptor's).
func TestNewDirWatches_Received(t *testing.T) {
	fence := t.TempDir()
	n := &newDirWatches{fence: fence}
	var added []string
	add := func(dir string) { added = append(added, dir) }

	n.created("/w/src/d00", add)
	n.created("/w/src/d01", add)
	if len(added) != 0 {
		t.Fatalf("added before the next event: %v", added)
	}
	if n.received(fsnotify.Event{Name: "/w/src/f.go", Op: fsnotify.Write}, add) {
		t.Fatal("an event of a watch is not ignored")
	}
	if !slices.Equal(added, []string{"/w/src/d00", "/w/src/d01"}) {
		t.Fatalf("queued directories added on the next event: %v", added)
	}
	for _, e := range []fsnotify.Event{
		{Name: fence, Op: fsnotify.Chmod},
		{Name: fence + "/", Op: fsnotify.Chmod},
		{Name: ".", Op: fsnotify.Create},
		{Name: "", Op: fsnotify.Remove},
		{Name: "src/d00", Op: fsnotify.Create},
	} {
		if !n.received(e, add) {
			t.Errorf("%v is not ignored", e)
		}
	}
	if len(added) != 2 {
		t.Fatalf("added twice: %v", added)
	}

	// an error from the watcher is a later send as well
	n.created("/w/src/d02", add)
	n.flush(add)
	if !slices.Equal(added, []string{"/w/src/d00", "/w/src/d01", "/w/src/d02"}) {
		t.Fatalf("flush: %v", added)
	}
}

// Without a fence, a new directory is added at once (the other backends; and
// kqueue when no fence could be made). A fence that cannot be touched any more
// (removed) turns into that: what is queued is added, then everything at once.
func TestNewDirWatches_WithoutFenceAddsAtOnce(t *testing.T) {
	var added []string
	add := func(dir string) { added = append(added, dir) }

	n := &newDirWatches{}
	n.created("/w/src/d00", add)
	if !slices.Equal(added, []string{"/w/src/d00"}) {
		t.Fatalf("no fence: %v", added)
	}

	added = nil
	fence := filepath.Join(t.TempDir(), "fence")
	if err := os.Mkdir(fence, 0o755); err != nil {
		t.Fatal(err)
	}
	n = &newDirWatches{fence: fence}
	n.created("/w/src/d00", add)
	if err := os.Remove(fence); err != nil {
		t.Fatal(err)
	}
	n.created("/w/src/d01", add)
	if !slices.Equal(added, []string{"/w/src/d00", "/w/src/d01"}) || n.fence != "" {
		t.Fatalf("fence gone: added %v, fence %q", added, n.fence)
	}
	n.created("/w/src/d02", add)
	if len(added) != 3 {
		t.Fatalf("after the fence is gone: %v", added)
	}
}

// What atExit registers runs once, when Start fails or at exit.
func TestStartEnv_AtExitRunsOnce(t *testing.T) {
	e := &startEnv{}
	runs := 0
	e.atExit(func() { runs++ })
	var ok error
	e.failed(&ok)
	if runs != 0 {
		t.Fatal("ran although Start did not fail")
	}
	failed := errors.New("start failed")
	e.failed(&failed)
	e.runAtExit()
	if runs != 1 {
		t.Fatalf("ran %d times", runs)
	}
}
