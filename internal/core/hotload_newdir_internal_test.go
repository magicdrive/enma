package core

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/enma/internal/commandline/option"
)

// PRA-02: the queue side of new-directory batches, without the timer. The
// end-to-end behaviour (builds and restarts) is in e2e/hotload_newdir_e2e_test.go.

func newBatchRunner(t *testing.T, build string) (*HotloadRunner, string) {
	t.Helper()
	old := hotloadDebounce
	hotloadDebounce = time.Hour // nothing is handled while the test looks at the queue
	pendingMuHotload.Lock()
	pendingHotload = make(map[string]pendingChangeHotload)
	pendingBatchDirs = nil
	pendingMuHotload.Unlock()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	opt := &option.HotloadOption{Build: build, Placeholder: "{}", WatchDirList: []string{root}, ExcludeExt: ".tmp", ExcludeExtList: []string{".tmp"}}
	if err := opt.CheckContentDiff.Set("on"); err != nil {
		t.Fatal(err)
	}
	r := &HotloadRunner{Options: opt}
	t.Cleanup(func() {
		if r.debounceTimer != nil {
			r.debounceTimer.Stop()
		}
		hotloadDebounce = old
		pendingMuHotload.Lock()
		pendingHotload = make(map[string]pendingChangeHotload)
		pendingBatchDirs = nil
		pendingMuHotload.Unlock()
	})
	return r, root
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pending returns a copy of the queue: path -> sorted files (nil for a single change).
func pending() map[string][]string {
	pendingMuHotload.Lock()
	defer pendingMuHotload.Unlock()
	out := map[string][]string{}
	for k, c := range pendingHotload {
		f := slices.Clone(c.files)
		slices.Sort(f)
		if c.batch && f == nil {
			f = []string{}
		}
		out[k] = f
	}
	return out
}

func create(r *HotloadRunner, path string) {
	r.enqueue(fsnotify.Event{Name: path, Op: fsnotify.Create})
}

func TestHotloadNewDirBatch_Queue(t *testing.T) {
	t.Run("files of a new directory are one change; ignored files are left out", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "a.go", "b.go", "sub/c.go", "x.tmp")
		r.enqueueNewDir(d)
		got := pending()
		want := []string{filepath.Join(d, "a.go"), filepath.Join(d, "b.go"), filepath.Join(d, "sub/c.go")}
		if len(got) != 1 || !slices.Equal(got[d], want) {
			t.Fatalf("queue %v, want one batch %s with %v", got, d, want)
		}
	})
	t.Run("a change below a pending batch joins it; one elsewhere does not", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "a.go")
		r.enqueueNewDir(d)
		create(r, filepath.Join(d, "late.go"))
		create(r, filepath.Join(root, "pkg2.go")) // a sibling name with the same prefix
		got := pending()
		if len(got) != 2 || !slices.Equal(got[d], []string{filepath.Join(d, "a.go"), filepath.Join(d, "late.go")}) || got[filepath.Join(root, "pkg2.go")] != nil {
			t.Fatalf("queue %v", got)
		}
	})
	t.Run("a nested directory picked up later joins its parent's batch", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "a.go", "inner/b.go")
		r.enqueueNewDir(d)
		r.enqueueNewDir(filepath.Join(d, "inner"))
		if got := pending(); len(got) != 1 || len(got[d]) != 3 {
			t.Fatalf("queue %v, want the one batch %s", got, d)
		}
	})
	t.Run("a parent picked up later takes in the pending changes below it", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "a.go", "inner/b.go")
		create(r, filepath.Join(d, "a.go"))
		r.enqueueNewDir(filepath.Join(d, "inner"))
		r.enqueueNewDir(d)
		got := pending()
		if len(got) != 1 || got[d] == nil {
			t.Fatalf("queue %v, want only the batch %s", got, d)
		}
		if f := slices.Compact(got[d]); !slices.Equal(f, []string{filepath.Join(d, "a.go"), filepath.Join(d, "inner/b.go")}) {
			t.Fatalf("batch files %v", got[d])
		}
	})
	t.Run("a directory moved away and replaced by an empty one is still a change (A1)", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, root, "pkg/.keep.tmp") // only an ignored file: no files of its own
		r.enqueue(fsnotify.Event{Name: d, Op: fsnotify.Rename})
		r.enqueueNewDir(d)
		got := pending()
		if len(got) != 1 || !slices.Equal(got[d], []string{d}) {
			t.Fatalf("queue %v, want the batch %s holding the change of %s", got, d, d)
		}
		pendingMuHotload.Lock()
		c := pendingHotload[d]
		pendingMuHotload.Unlock()
		if !r.batchHasChanges(c) {
			t.Fatal("the replaced directory is not built")
		}
	})
	t.Run("batches are found after others were absorbed", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "a.go", "inner/b.go")
		r.enqueueNewDir(filepath.Join(d, "inner"))
		r.enqueueNewDir(d) // absorbs the inner batch
		create(r, filepath.Join(d, "inner", "late.go"))
		if got := pending(); len(got) != 1 || !slices.Contains(got[d], filepath.Join(d, "inner", "late.go")) {
			t.Fatalf("queue %v", got)
		}
		pendingMuHotload.Lock()
		dirs := slices.Clone(pendingBatchDirs)
		pendingMuHotload.Unlock()
		if !slices.Equal(dirs, []string{d}) {
			t.Fatalf("pending batch list %v, want [%s]", dirs, d)
		}
	})
	t.Run("an absorbed batch keeps its changes that the walk does not find (B-01)", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		d := filepath.Join(root, "pkg")
		writeFiles(t, d, "inner/b.go")
		inner := filepath.Join(d, "inner")
		r.enqueueNewDir(inner)
		gone := filepath.Join(inner, "gone.go") // removed: only its event knows it
		r.enqueue(fsnotify.Event{Name: gone, Op: fsnotify.Remove})
		r.enqueueNewDir(d)
		if got := pending(); len(got) != 1 || !slices.Contains(got[d], gone) {
			t.Fatalf("queue %v, want the batch %s holding %s", got, d, gone)
		}
	})
	t.Run("directories side by side stay separate changes", func(t *testing.T) {
		r, root := newBatchRunner(t, "make")
		one, two := filepath.Join(root, "one"), filepath.Join(root, "one2")
		writeFiles(t, one, "a.go")
		writeFiles(t, two, "a.go")
		r.enqueueNewDir(one)
		r.enqueueNewDir(two)
		if got := pending(); len(got) != 2 || len(got[one]) != 1 || len(got[two]) != 1 {
			t.Fatalf("queue %v, want two batches", got)
		}
	})
}

func TestHotloadNewDirBatch_OnlyWithoutPlaceholder(t *testing.T) {
	for _, c := range []struct {
		name          string
		pre, bld, pst bool
		want          bool
	}{
		{"none", false, false, false, true},
		{"pre-build", true, false, false, false},
		{"build", false, true, false, false},
		{"post-build", false, false, true, false},
	} {
		r := &HotloadRunner{Options: &option.HotloadOption{HasPlaceholderPreBuild: c.pre, HasPlaceholderBuild: c.bld, HasPlaceholderPostBuild: c.pst}}
		if got := r.batchNewDirs(); got != c.want {
			t.Errorf("%s: batchNewDirs %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHotloadNewDirBatch_HasChanges(t *testing.T) {
	r, root := newBatchRunner(t, "make")
	d := filepath.Join(root, "pkg")
	writeFiles(t, d, "a.go", "b.go")
	a, b := filepath.Join(d, "a.go"), filepath.Join(d, "b.go")
	if r.batchHasChanges(pendingChangeHotload{path: d, batch: true}) {
		t.Fatal("an empty batch is built")
	}
	if !r.batchHasChanges(pendingChangeHotload{path: d, batch: true, files: []string{a, b, a}}) {
		t.Fatal("new files are not built")
	}
	if r.batchHasChanges(pendingChangeHotload{path: d, batch: true, files: []string{a, b}}) {
		t.Fatal("unchanged files are built again")
	}
	// every file's hash was recorded, not only the first changed one
	if r.hasChanged(b) {
		t.Fatal("the hash of the second file was not recorded")
	}
	writeFiles(t, d, "a.go") // same content
	if err := os.WriteFile(b, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !r.batchHasChanges(pendingChangeHotload{path: d, batch: true, files: []string{a, b}}) {
		t.Fatal("a changed file is not built")
	}
}
