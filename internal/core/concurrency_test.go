//go:build !windows

package core_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// H1c invariants (F05):
//   C1 each started process is reaped by exactly one Wait (race detector + mutation);
//   C2 shared runner state (daemon, running commands) is accessed under synchronisation;
//   C3 stop / restart / shutdown racing each other never deadlock (bounded time);
//   C4 at most one daemon runs at a time, none is (re)started after shutdown and
//      none is left running;
//   C5 commands started after shutdown fail fast; no goroutine is left behind.

func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("C3 violated: %s did not finish within %v (deadlock?)", what, d)
	}
}

// liveDaemons returns the recorded daemon pids that are still running.
func liveDaemons(t *testing.T, pids string) []int {
	var live []int
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			live = append(live, pid)
		}
	}
	return live
}

func TestConcurrency_ChangesAndStopsRacing(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	within(t, 30*time.Second, "concurrent changes and stops", func() {
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(3)
			go func() { defer wg.Done(); core.HandleChangeForTest(r, "") }()
			go func() { defer wg.Done(); core.HandleChangeForTest(r, "") }()
			go func() { defer wg.Done(); _ = core.StopDaemonForTest(r) }()
		}
		wg.Wait()
	})
	time.Sleep(200 * time.Millisecond) // let started daemons record their pid
	if live := liveDaemons(t, pids); len(live) > 1 {
		t.Fatalf("C4 violated: %d daemons running at once: %v", len(live), live)
	}
	within(t, stopBound, "final stop", func() { _ = core.StopDaemonForTest(r) })
	for _, pid := range readPids(t, pids) {
		if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
			t.Fatalf("C4 violated: daemon %d left running (orphaned)", pid)
		}
	}
}

func TestConcurrency_ShutdownWinsOverChanges(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	within(t, 30*time.Second, "changes racing shutdown", func() {
		var wg sync.WaitGroup
		for g := 0; g < 3; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					core.HandleChangeForTest(r, "")
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			_ = core.ShutdownForTest(r)
		}()
		wg.Wait()
	})
	if err := core.StartDaemonForTest(r); err == nil {
		t.Fatal("C4 violated: a daemon was started after shutdown")
	}
	time.Sleep(200 * time.Millisecond)
	for _, pid := range readPids(t, pids) {
		if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
			t.Fatalf("C4 violated: daemon %d still running after shutdown", pid)
		}
	}
}

func TestConcurrency_InFlightCommandsAndShutdown(t *testing.T) {
	for _, kind := range []string{"hotload", "watch"} {
		t.Run(kind, func(t *testing.T) {
			var seq func() bool
			var shutdown func() error
			if kind == "hotload" {
				r := &core.HotloadRunner{Options: &option.HotloadOption{Build: "sleep 30", Placeholder: "{}"}, BuildTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetStopTimeoutsForTest(r, testGrace, testKill)
				seq = func() bool { return r.RunBuildSequence(0, "") }
				shutdown = func() error { return core.ShutdownForTest(r) }
			} else {
				w := &core.WatchRunner{Options: &option.WatchOption{Cmd: "sleep 30", Placeholder: "{}"}, CmdTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
				seq = func() bool { return w.RunBuildSequence(0, "") }
				shutdown = func() error { return core.WatchShutdownForTest(w) }
			}
			var wg sync.WaitGroup
			results := make(chan bool, 6)
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); results <- seq() }()
			}
			time.Sleep(200 * time.Millisecond)
			// late commands race the shutdown itself
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); time.Sleep(time.Duration(i) * 5 * time.Millisecond); results <- seq() }()
			}
			within(t, stopBound, "shutdown with commands in flight", func() {
				if err := shutdown(); err != nil {
					t.Errorf("shutdown: %v", err)
				}
			})
			within(t, 3*time.Second, "all commands returning", wg.Wait)
			close(results)
			for ok := range results {
				if ok {
					t.Fatal("a command reported success although it was aborted")
				}
			}
			within(t, 2*time.Second, "command after shutdown", func() {
				if seq() {
					t.Error("C5 violated: a command ran after shutdown")
				}
			})
		})
	}
}

func TestConcurrency_NoGoroutineLeft(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()
	r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		core.HandleChangeForTest(r, "")
	}
	if err := stopWithin(t, stopBound, func() error { return core.ShutdownForTest(r) }); err != nil {
		t.Fatal(err)
	}
	// the abort helper goroutine ends with the scope; reapers end with their process
	if !waitFor(t, 3*time.Second, func() bool { return runtime.NumGoroutine() <= before+1 }) {
		buf := make([]byte, 1<<16)
		t.Fatalf("C5 violated: %d goroutines before, %d after\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
	}
}

// Starters spin while shutdown runs: whatever the interleaving, no daemon may be
// started once shutdown has stopped the current one.
func TestConcurrency_ShutdownVsSpinningStarts(t *testing.T) {
	for round := 0; round < 5; round++ {
		dir := t.TempDir()
		pids := filepath.Join(dir, "pids")
		killLeftovers(t, pids)
		r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
		if err := core.StartDaemonForTest(r); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						_ = core.StartDaemonForTest(r)
					}
				}
			}()
		}
		time.Sleep(20 * time.Millisecond)
		within(t, stopBound, "shutdown while starts spin", func() { _ = core.ShutdownForTest(r) })
		close(stop)
		within(t, 2*time.Second, "starters returning", wg.Wait)
		time.Sleep(200 * time.Millisecond)
		for _, pid := range readPids(t, pids) {
			if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
				t.Fatalf("round %d: C4 violated: daemon %d started around shutdown is still running", round, pid)
			}
		}
	}
}

// Same as above for the second signal: a force kill racing spinning starts must
// leave no daemon behind.
func TestConcurrency_ForceKillVsSpinningStarts(t *testing.T) {
	for round := 0; round < 5; round++ {
		dir := t.TempDir()
		pids := filepath.Join(dir, "pids")
		killLeftovers(t, pids)
		r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
		if err := core.StartDaemonForTest(r); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						_ = core.StartDaemonForTest(r)
					}
				}
			}()
		}
		time.Sleep(20 * time.Millisecond)
		within(t, stopBound, "force kill while starts spin", func() { core.ForceKillForTest(r) })
		close(stop)
		within(t, 2*time.Second, "starters returning", wg.Wait)
		time.Sleep(200 * time.Millisecond)
		for _, pid := range readPids(t, pids) {
			if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
				t.Fatalf("round %d: daemon %d started around a force kill is still running", round, pid)
			}
		}
	}
}
