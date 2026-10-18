//go:build !windows

package core_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// Long-running use does not pile up resources (H3 2.3). Each test records
// goroutines, open file descriptors and the live heap at the start, the most
// seen while it runs, and after it ends, and fails when the after value has
// grown beyond a small allowance (a leak) — a temporary rise while work runs
// is expected and only logged.
//
// The default sizes keep `go test ./...` short. ENMA_SOAK=1 multiplies them
// (manual soak run, e.g. `ENMA_SOAK=1 go test -run Soak -v ./internal/core/`).

func soakScale(n int) int {
	if os.Getenv("ENMA_SOAK") != "" {
		return n * 20
	}
	return n
}

type resources struct {
	goroutines int
	fds        int
	heap       uint64 // live heap after GC, bytes
}

func openFDs() int {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

func measure() resources {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return resources{goroutines: runtime.NumGoroutine(), fds: openFDs(), heap: m.HeapAlloc}
}

type resourceTrack struct {
	t           *testing.T
	start, peak resources
}

func trackResources(t *testing.T) *resourceTrack {
	r := &resourceTrack{t: t, start: measure()}
	r.peak = r.start
	return r
}

func (r *resourceTrack) sample() {
	m := resources{goroutines: runtime.NumGoroutine(), fds: openFDs()}
	r.peak.goroutines = max(r.peak.goroutines, m.goroutines)
	r.peak.fds = max(r.peak.fds, m.fds)
}

// settle waits (up to 5s) for goroutines and descriptors to come back to the
// start values plus the allowance, then checks and logs start / peak / after.
func (r *resourceTrack) settle(what string, heapAllowance uint64) {
	r.t.Helper()
	const goroutineAllowance, fdAllowance = 3, 2
	var end resources
	deadline := time.Now().Add(5 * time.Second)
	for {
		end = measure()
		if (end.goroutines <= r.start.goroutines+goroutineAllowance && end.fds <= r.start.fds+fdAllowance) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.t.Logf("%s: goroutines start=%d peak=%d after=%d; fds start=%d peak=%d after=%d; heap start=%dKiB after=%dKiB",
		what, r.start.goroutines, r.peak.goroutines, end.goroutines, r.start.fds, r.peak.fds, end.fds,
		r.start.heap/1024, end.heap/1024)
	if end.goroutines > r.start.goroutines+goroutineAllowance {
		buf := make([]byte, 1<<16)
		r.t.Errorf("%s: goroutines leaked: %d -> %d\n%s", what, r.start.goroutines, end.goroutines, buf[:runtime.Stack(buf, true)])
	}
	if end.fds > r.start.fds+fdAllowance {
		r.t.Errorf("%s: file descriptors leaked: %d -> %d", what, r.start.fds, end.fds)
	}
	if end.heap > r.start.heap+heapAllowance {
		r.t.Errorf("%s: live heap grew by %d KiB (allowance %d KiB)", what, (end.heap-r.start.heap)/1024, heapAllowance/1024)
	}
}

// Many fires of jobs that succeed, fail with retries, time out, leave children
// behind or cannot start, with too few slots (busy and capacity skips).
func TestSoak_SchedulerManyFires(t *testing.T) {
	seconds := soakScale(40)
	track := trackResources(t)
	func() {
		h := newHarness(t, t0, nil, 3,
			jobSpec{name: "ok", cron: "* * * * * *", command: "true"},
			jobSpec{name: "fail", cron: "* * * * * *", command: "exit 1", retries: 2, retryDelay: time.Second},
			jobSpec{name: "slow", cron: "*/2 * * * * *", command: "sleep 5", timeout: 30 * time.Millisecond},
			jobSpec{name: "leftover", cron: "*/3 * * * * *", command: "sleep 30 & exit 0"},
			jobSpec{name: "nostart", cron: "* * * * * *", command: "true", dir: filepath.Join(t.TempDir(), "gone")},
		)
		for range seconds {
			h.advance(time.Second)
			track.sample()
		}
		evs := h.snapshot()
		t.Logf("events: %d, job_success=%d job_failed=%d job_timeout=%d skips=%d", len(evs),
			count(evs, core.EventJobSuccess, ""), count(evs, core.EventJobFailed, ""), count(evs, core.EventJobTimeout, ""),
			count(evs, core.EventSkip, ""))
		if count(evs, core.EventJobSuccess, "ok") == 0 || count(evs, core.EventJobTimeout, "slow") == 0 ||
			count(evs, core.EventJobFailed, "nostart") == 0 || count(evs, core.EventRetryStart, "fail") == 0 {
			t.Fatalf("not every kind of run happened:\n%s", dump(evs))
		}
		if err := core.SchedulerShutdownForTest(h.r); err != nil {
			t.Fatal(err)
		}
	}()
	track.settle("scheduler", 4<<20)
}

// Many command sequences in hotload and watch: success, failure with retries,
// timeouts and leftovers; and many daemon restarts.
func TestSoak_CommandsAndDaemonRestarts(t *testing.T) {
	n := soakScale(25)
	t.Chdir(t.TempDir())
	track := trackResources(t)
	func() {
		w := &core.WatchRunner{Options: &option.WatchOption{Cmd: "true", PreCmd: "sleep 30 & exit 0", PostCmd: "true", Placeholder: "{}"},
			CmdTimeout: time.Minute, ExecCommand: exec.CommandContext}
		core.SetWatchStopTimeoutsForTest(w, 200*time.Millisecond, time.Second)
		failing := &core.WatchRunner{Options: &option.WatchOption{Cmd: "exit 2", Placeholder: "{}"},
			CmdTimeout: time.Minute, ExecCommand: exec.CommandContext}
		slow := &core.WatchRunner{Options: &option.WatchOption{Cmd: "sleep 5", Placeholder: "{}"},
			CmdTimeout: 30 * time.Millisecond, ExecCommand: exec.CommandContext}
		h := &core.HotloadRunner{Options: &option.HotloadOption{Build: "true", Daemon: "sleep 30", Placeholder: "{}"},
			BuildTimeout: time.Minute, ExecCommand: exec.CommandContext}
		if err := h.Options.SignalName.Set("SIGTERM"); err != nil {
			t.Fatal(err)
		}
		core.SetStopTimeoutsForTest(h, 500*time.Millisecond, time.Second)
		for i := range n {
			if err := core.WatchRunSequenceForTest(w, fmt.Sprint(i)); err != nil {
				t.Fatalf("watch sequence %d: %v", i, err)
			}
			if core.WatchRunSequenceForTest(failing, "") == nil {
				t.Fatal("a failing command succeeded")
			}
			if core.WatchRunSequenceForTest(slow, "") == nil {
				t.Fatal("a timed-out command succeeded")
			}
			if err := core.StopDaemonForTest(h); err != nil {
				t.Fatalf("stop daemon %d: %v", i, err)
			}
			if err := core.StartDaemonForTest(h); err != nil {
				t.Fatalf("start daemon %d: %v", i, err)
			}
			track.sample()
		}
		for _, err := range []error{core.WatchShutdownForTest(w), core.WatchShutdownForTest(failing),
			core.WatchShutdownForTest(slow), core.ShutdownForTest(h)} {
			if err != nil {
				t.Fatal(err)
			}
		}
	}()
	track.settle("hotload/watch", 4<<20)
}
