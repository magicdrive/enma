package core

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/fsnotify/fsnotify"
)

// newDirWatches adds the directories created in watched directories to the
// watcher. With the kqueue backend of fsnotify (macOS, BSD) it waits until
// fsnotify's own registration of each new directory is over.
//
// When kqueue reports a change of a watched directory, fsnotify's reader
// goroutine sends a Create for each new entry and, right after the send,
// registers the entry itself (for NOTE_DELETE and NOTE_RENAME). fsnotify
// v1.10.1 does not serialize that registration with Watcher.Add
// (fsnotify/fsnotify#783): an Add of the new directory made as soon as its
// Create arrives runs while the reader registers it, and both can open the
// directory. fsnotify then keeps only one of the two descriptors: the other
// stays open after the directory is deleted, and its last event carries no
// name, which fsnotify reports as a Create of "." (the working directory).
//
// The reader finishes what follows a send before it sends again, so any event
// received after a Create proves that the registration following the Create is
// over: the queued directories are added when the next event (or error)
// arrives. The fence makes sure there is a next event: a directory of enma's
// own, watched for this only, whose times enma updates after queuing a
// directory. Its event (a Chmod) is not a change.
//
// Every watch of enma has an absolute path, so does every event of it; an
// event with another name ("" or ".") is one of a descriptor fsnotify lost
// track of, and is not a change either.
//
// All methods are called from the watcher goroutine only.
type newDirWatches struct {
	watcher *fsnotify.Watcher
	fence   string   // the fence directory; "" adds new directories at once
	pending []string // created directories waiting for the next event
}

// kqueueBackend reports whether fsnotify watches with kqueue on this platform.
func kqueueBackend() bool {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		return true
	}
	return false
}

// newNewDirWatches sets up the fence on kqueue platforms; env removes it when
// enma exits. Without a fence (another backend, or no fence could be made),
// new directories are added as soon as their Create arrives.
func newNewDirWatches(watcher *fsnotify.Watcher, env *startEnv) *newDirWatches {
	n := &newDirWatches{watcher: watcher}
	if !kqueueBackend() {
		return n
	}
	fence, err := os.MkdirTemp("", "enma-fence-")
	if err == nil {
		if err = watcher.Add(fence); err != nil {
			_ = os.Remove(fence)
		}
	}
	if err != nil {
		log.Printf("⚠️  New directories are added without waiting for the watcher (no fence: %v)", err)
		return n
	}
	n.fence = filepath.Clean(fence)
	env.atExit(func() { _ = os.Remove(fence) })
	return n
}

// received is called first for every event: it adds the queued directories
// (their registration by fsnotify is over) and reports whether event is to be
// ignored, not being a change: the fence's, or one without an absolute path.
func (n *newDirWatches) received(event fsnotify.Event, add func(dir string)) (ignore bool) {
	n.flush(add)
	return !filepath.IsAbs(event.Name) || (n.fence != "" && filepath.Clean(event.Name) == n.fence)
}

// flush adds the queued directories. It is called for every event and error
// the watcher reports (see received).
func (n *newDirWatches) flush(add func(dir string)) {
	queued := n.pending
	n.pending = nil
	for _, dir := range queued {
		add(dir)
	}
}

// created queues dir, created in a watched directory, for add; without a fence
// add runs at once.
func (n *newDirWatches) created(dir string, add func(dir string)) {
	if n.fence == "" {
		add(dir)
		return
	}
	n.pending = append(n.pending, dir)
	now := time.Now()
	if err := os.Chtimes(n.fence, now, now); err != nil {
		// no fence event will come: from now on, add at once
		log.Printf("⚠️  New directories are added without waiting for the watcher (fence: %v)", err)
		n.fence = ""
		n.flush(add)
	}
}
