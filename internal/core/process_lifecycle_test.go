//go:build !windows

package core_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// H1b invariants (F04):
//   L1 a stop request finishes in bounded time (grace + kill wait + slack);
//   L2 after a successful stop no process of the group is left (leader and descendants);
//   L3 a restart never leaves the previous daemon running;
//   L4 a timed-out / cancelled build leaves no descendants behind;
//   L5 a failure to stop is reported, not hidden.

const (
	testGrace   = 300 * time.Millisecond
	testKill    = 2 * time.Second
	stopBound   = testGrace + testKill + 2*time.Second
	pollDefault = 10 * time.Millisecond
)

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil {
		return false
	}
	// A zombie still answers kill(pid, 0); treat it as gone.
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

func readPids(t *testing.T, path string) []int {
	t.Helper()
	var pids []int
	b, _ := os.ReadFile(path)
	for _, f := range strings.Fields(string(b)) {
		if n, err := strconv.Atoi(f); err == nil {
			pids = append(pids, n)
		}
	}
	return pids
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(pollDefault)
	}
	return cond()
}

// killLeftovers is the test's own safety net: only pids this test recorded.
func killLeftovers(t *testing.T, pidFile string) {
	t.Cleanup(func() {
		for _, pid := range readPids(t, pidFile) {
			if pid <= 0 { // 0 or below would signal a whole process group
				continue
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

func newDaemonRunner(t *testing.T, daemon string) *core.HotloadRunner {
	opt := &option.HotloadOption{Daemon: daemon, Placeholder: "{}"}
	if err := opt.SignalName.Set("SIGTERM"); err != nil {
		t.Fatal(err)
	}
	r := core.NewHotloadRunner(HelperInflateHotloadOptionDefaultValue(opt))
	core.SetStopTimeoutsForTest(r, testGrace, testKill)
	t.Cleanup(func() { core.ForceKillForTest(r) }) // safety net besides the pid files
	return r
}

func TestLifecycle_ForceKillAloneStopsEverything(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newDaemonRunner(t, `trap '' TERM; echo $$ >> '`+pids+`'; while :; do sleep 0.05; done`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start") // the checks below would pass on no pid at all
	}
	core.ForceKillForTest(r) // second signal without a first one
	if err := core.StartDaemonForTest(r); !errors.Is(err, core.ErrShuttingDownForTest) {
		t.Fatalf("start after force kill: got %v, want refusal", err)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("daemon %d survived a force kill", pid)
		}
	}
}

// stopWithin runs stop and fails (instead of hanging the suite) if it does not return in time.
func stopWithin(t *testing.T, d time.Duration, stop func() error) error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- stop() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(d):
		t.Fatalf("L1 violated: stop did not return within %v", d)
		return nil
	}
}

func TestLifecycle_StopEscalatesWhenTermIgnored(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newDaemonRunner(t, `trap '' TERM; echo $$ >> '`+pids+`'; while :; do sleep 0.05; done`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start") // the checks below would pass on no pid at all
	}
	start := time.Now()
	err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) })
	if err != nil {
		t.Fatalf("stop reported error: %v", err)
	}
	if el := time.Since(start); el < testGrace {
		t.Fatalf("escalated before the grace period: %v", el)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("L2 violated: daemon %d still alive after stop", pid)
		}
	}
}

func TestLifecycle_StopKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	// The leader exits on TERM; its child ignores TERM and would survive a leader-only stop.
	// (the child sets its trap before it records its pid, which the test waits for)
	r := newDaemonRunner(t, `sh -c 'trap "" TERM; echo $$ >> "$0"; exec sleep 1000' '`+pids+`' & wait`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("child pid not recorded")
	}
	if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatalf("stop reported error: %v", err)
	}
	for _, pid := range readPids(t, pids) {
		if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
			t.Fatalf("L2 violated: descendant %d survived the stop", pid)
		}
	}
}

func TestLifecycle_StopRightAfterStartAndRepeatedRestart(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newDaemonRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	for i := 0; i < 8; i++ {
		if err := core.StartDaemonForTest(r); err != nil {
			t.Fatal(err)
		}
		if i%2 == 1 {
			time.Sleep(50 * time.Millisecond)
		}
		if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
			t.Fatalf("stop %d: %v", i, err)
		}
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("L3 violated: daemon %d left running after restarts", pid)
		}
	}
}

func TestLifecycle_StopWithoutDaemonIsNoop(t *testing.T) {
	r := newDaemonRunner(t, "true")
	if err := stopWithin(t, time.Second, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatalf("stop without daemon: %v", err)
	}
}

func TestLifecycle_DaemonThatAlreadyExited(t *testing.T) {
	r := newDaemonRunner(t, "exit 7")
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatalf("stopping an exited daemon must succeed: %v", err)
	}
}

func TestLifecycle_BuildTimeoutKillsDescendants(t *testing.T) {
	for _, kind := range []string{"hotload", "watch"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			pids := filepath.Join(dir, "pids")
			killLeftovers(t, pids)
			command := `sleep 1000 & echo $! >> '` + pids + `'; wait`
			var run func(context.Context, string) error
			if kind == "hotload" {
				r := &core.HotloadRunner{Options: &option.HotloadOption{Build: command, Placeholder: "{}"}, BuildTimeout: 300 * time.Millisecond, ExecCommand: exec.CommandContext}
				core.SetStopTimeoutsForTest(r, testGrace, testKill)
				run = r.RunBuild
			} else {
				w := &core.WatchRunner{Options: &option.WatchOption{Cmd: command, Placeholder: "{}"}, CmdTimeout: 300 * time.Millisecond, ExecCommand: exec.CommandContext}
				core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
				run = w.RunCmd
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := run(ctx, "")
			if err == nil {
				t.Fatal("L5 violated: a timed-out build reported success")
			}
			if el := time.Since(start); el > stopBound {
				t.Fatalf("L1 violated: build cancel took %v", el)
			}
			for _, pid := range readPids(t, pids) {
				if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
					t.Fatalf("L4 violated: build descendant %d survived the timeout", pid)
				}
			}
		})
	}
}

func TestLifecycle_ChildFailureIsReported(t *testing.T) {
	r := &core.HotloadRunner{Options: &option.HotloadOption{Build: "exit 5", Placeholder: "{}"}, BuildTimeout: 5 * time.Second, ExecCommand: exec.CommandContext}
	err := r.RunBuild(context.Background(), "")
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 5 {
		t.Fatalf("want exit 5, got %v", err)
	}
}

func TestLifecycle_ShutdownAbortsRunningCommand(t *testing.T) {
	for _, kind := range []string{"hotload", "watch"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			pids := filepath.Join(dir, "pids")
			killLeftovers(t, pids)
			command := `trap '' TERM; echo $$ >> '` + pids + `'; sleep 30`
			var seq func() bool
			var shutdown func() error
			if kind == "hotload" {
				r := &core.HotloadRunner{Options: &option.HotloadOption{Build: command, Placeholder: "{}"}, BuildTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetStopTimeoutsForTest(r, testGrace, testKill)
				seq = func() bool { return r.RunBuildSequence(0, "") }
				shutdown = func() error { return core.ShutdownForTest(r) }
			} else {
				w := &core.WatchRunner{Options: &option.WatchOption{Cmd: command, Placeholder: "{}"}, CmdTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
				seq = func() bool { return w.RunBuildSequence(0, "") }
				shutdown = func() error { return core.WatchShutdownForTest(w) }
			}
			result := make(chan bool, 1)
			go func() { result <- seq() }()
			if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
				t.Fatal("command did not start")
			}
			if err := stopWithin(t, stopBound, shutdown); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			select {
			case ok := <-result:
				if ok {
					t.Fatal("L5 violated: an aborted command reported success")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("L1 violated: the running command did not return after shutdown")
			}
			for _, pid := range readPids(t, pids) {
				if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
					t.Fatalf("L4 violated: command %d survived shutdown", pid)
				}
			}
		})
	}
}

func TestLifecycle_ForceKillCutsTheGracePeriod(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newDaemonRunner(t, `trap '' TERM; echo $$ >> '`+pids+`'; while :; do sleep 0.05; done`)
	core.SetStopTimeoutsForTest(r, 5*time.Second, testKill)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start") // the checks below would pass on no pid at all
	}
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- core.ShutdownForTest(r) }() // graceful: would wait 5s
	time.Sleep(100 * time.Millisecond)
	core.ForceKillForTest(r) // second signal
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("force kill took %v", el)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("daemon %d survived a force kill", pid)
		}
	}
	select {
	case <-done:
	case <-time.After(stopBound):
		t.Fatal("graceful shutdown did not return after the force kill")
	}
}

func TestLifecycle_StopFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newDaemonRunner(t, `trap '' TERM; echo $$ >> '`+pids+`'; while :; do sleep 0.05; done`)
	core.SetStopTimeoutsForTest(r, 50*time.Millisecond, 50*time.Millisecond)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start")
	}
	// A member enma cannot reap keeps the group from being confirmed gone after
	// SIGKILL: the stop must fail, not pretend.
	release := holdGroupOpen(t, readPids(t, pids)[0])
	err := core.StopDaemonForTest(r)
	if !errors.Is(err, core.ErrStopTimeout) {
		t.Fatalf("L5 violated: unconfirmed stop returned %v, want ErrStopTimeout", err)
	}
	// The daemon stays tracked, so a later stop can still confirm it is gone.
	if err := core.StartDaemonForTest(r); !errors.Is(err, core.ErrDaemonRunningForTest) {
		t.Fatalf("start after an unconfirmed stop: got %v, want refusal", err)
	}
	release()
	core.SetStopTimeoutsForTest(r, testGrace, testKill)
	if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("daemon %d still alive", pid)
		}
	}
}

// holdGroupOpen makes the stop of the process group pgid unconfirmable, with
// real processes and whatever the stop timeouts: a helper process outside the
// group starts a member of the group and never reaps it. Once killed, that
// member stays a zombie of the helper, so the group never looks empty to enma
// (it can only reap its own children). A stop that merely runs out of time
// (tiny timeouts) is not a reliable failure: SIGKILL and the reaping of the
// group can complete before the first check. release kills the helper; the
// zombie is then re-parented and reaped, and the group can be confirmed gone.
func holdGroupOpen(t *testing.T, pgid int) (release func()) {
	t.Helper()
	memberFile := filepath.Join(t.TempDir(), "member")
	h := exec.Command(os.Args[0], "-test.run=^TestLifecycleHelper_ForeignGroupMember$")
	h.Env = append(os.Environ(), "ENMA_FOREIGN_MEMBER_PGID="+strconv.Itoa(pgid), "ENMA_FOREIGN_MEMBER_FILE="+memberFile)
	h.Stderr = os.Stderr
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	member := func() int {
		b, _ := os.ReadFile(memberFile)
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			// The member first: while the helper (its parent) lives, its pid
			// cannot be reused, so this never signals another process.
			if pid := member(); pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			_ = h.Process.Kill()
			_ = h.Wait()
		})
	}
	t.Cleanup(release)
	if !waitFor(t, 5*time.Second, func() bool { return member() > 0 }) {
		t.Fatal("the foreign group member did not start")
	}
	if got, err := syscall.Getpgid(member()); err != nil || got != pgid {
		t.Fatalf("foreign member %d is in group %d (%v), want %d", member(), got, err, pgid)
	}
	return release
}

// TestLifecycleHelper_ForeignGroupMember is the helper process of holdGroupOpen.
func TestLifecycleHelper_ForeignGroupMember(t *testing.T) {
	pgidEnv := os.Getenv("ENMA_FOREIGN_MEMBER_PGID")
	if pgidEnv == "" {
		return // only meaningful as the helper process (like TestHotloadRunnerHelperProcess)
	}
	pgid, err := strconv.Atoi(pgidEnv)
	if err != nil || pgid <= 1 {
		t.Fatalf("bad pgid %q", pgidEnv)
	}
	c := exec.Command("sleep", "1000")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	tmp := os.Getenv("ENMA_FOREIGN_MEMBER_FILE") + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(c.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, os.Getenv("ENMA_FOREIGN_MEMBER_FILE")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Hour) // never reap c; holdGroupOpen kills this process
}

// newChangeRunner is a daemon runner whose build always succeeds, for the real
// change path (handleChangeDirect: build, stop, start).
func newChangeRunner(t *testing.T, daemon string) *core.HotloadRunner {
	r := newDaemonRunner(t, daemon)
	r.Options.Build = "true"
	r.BuildTimeout = 10 * time.Second
	r.ExecCommand = exec.CommandContext
	return r
}

func TestLifecycle_ChangeRestartsExactlyOneDaemon(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	// each daemon records its pid once it runs; the next change replaces it only
	// then (stopping one that has not written its pid yet would lose that pid)
	started := func(n int) bool {
		return waitFor(t, 5*time.Second, func() bool { return len(readPids(t, pids)) == n })
	}
	if !started(1) {
		t.Fatal("the first daemon did not start")
	}
	for i := 0; i < 3; i++ {
		core.HandleChangeForTest(r, "")
		if !started(i + 2) {
			t.Fatalf("daemon %d did not start", i+2)
		}
	}
	all := readPids(t, pids)
	if len(all) != 4 {
		t.Fatalf("want 4 daemon starts, got %d", len(all))
	}
	for _, pid := range all[:3] {
		if alive(pid) {
			t.Fatalf("L3 violated: replaced daemon %d still running", pid)
		}
	}
	if !alive(all[3]) {
		t.Fatal("the current daemon is not running")
	}
	if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycle_NoNewDaemonWhenStopFails(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newChangeRunner(t, `trap '' TERM; echo $$ >> '`+pids+`'; while :; do sleep 0.05; done`)
	core.SetStopTimeoutsForTest(r, 50*time.Millisecond, 50*time.Millisecond)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start")
	}
	release := holdGroupOpen(t, readPids(t, pids)[0])
	core.HandleChangeForTest(r, "") // stop cannot be confirmed: must not start a second daemon
	// HandleChange returned without starting a daemon; give one that was started
	// anyway the time to record its pid.
	time.Sleep(300 * time.Millisecond)
	if n := len(readPids(t, pids)); n != 1 {
		t.Fatalf("L3/L5 violated: %d daemons started although the previous stop failed", n)
	}
	if err := core.StartDaemonForTest(r); !errors.Is(err, core.ErrDaemonRunningForTest) {
		t.Fatalf("L5 violated: the unconfirmed daemon is no longer tracked (start: %v)", err)
	}
	release()
	core.SetStopTimeoutsForTest(r, testGrace, testKill)
	if err := stopWithin(t, stopBound, func() error { return core.StopDaemonForTest(r) }); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycle_NoDaemonAfterShutdown(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	r := newChangeRunner(t, `echo $$ >> '`+pids+`'; exec sleep 1000`)
	if err := core.StartDaemonForTest(r); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == 1 }) {
		t.Fatal("daemon did not start")
	}
	if err := stopWithin(t, stopBound, func() error { return core.ShutdownForTest(r) }); err != nil {
		t.Fatal(err)
	}
	if err := core.StartDaemonForTest(r); !errors.Is(err, core.ErrShuttingDownForTest) {
		t.Fatalf("start after shutdown: got %v, want refusal", err)
	}
	core.HandleChangeForTest(r, "") // a late change event
	time.Sleep(200 * time.Millisecond)
	if n := len(readPids(t, pids)); n != 1 {
		t.Fatalf("a daemon was started after shutdown (%d starts)", n)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("daemon %d running after shutdown", pid)
		}
	}
}

func TestLifecycle_CommandsAfterShutdownFailFast(t *testing.T) {
	for _, kind := range []string{"hotload", "watch"} {
		t.Run(kind, func(t *testing.T) {
			var seq func() bool
			var shutdown func() error
			if kind == "hotload" {
				r := &core.HotloadRunner{Options: &option.HotloadOption{Build: "true", Placeholder: "{}"}, BuildTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetStopTimeoutsForTest(r, testGrace, testKill)
				seq = func() bool { return r.RunBuildSequence(0, "") }
				shutdown = func() error { return core.ShutdownForTest(r) }
			} else {
				w := &core.WatchRunner{Options: &option.WatchOption{Cmd: "true", Placeholder: "{}"}, CmdTimeout: time.Minute, ExecCommand: exec.CommandContext}
				core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
				seq = func() bool { return w.RunBuildSequence(0, "") }
				shutdown = func() error { return core.WatchShutdownForTest(w) }
			}
			if !seq() {
				t.Fatal("command failed before shutdown")
			}
			if err := stopWithin(t, stopBound, shutdown); err != nil {
				t.Fatal(err)
			}
			done := make(chan bool, 1)
			go func() { done <- seq() }()
			select {
			case ok := <-done:
				if ok {
					t.Fatal("a command ran after shutdown")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("L1 violated: a command after shutdown blocked instead of failing")
			}
		})
	}
}

// Shutdown waits for commands that are still running, even ones that ignore
// cancellation, so nothing keeps running behind the exit (bounded by the abort timeout).
func TestLifecycle_ShutdownWaitsForRunningCommands(t *testing.T) {
	for _, kind := range []string{"hotload", "watch"} {
		t.Run(kind, func(t *testing.T) {
			// the command marks that it runs, then sleeps; it is not bound to ctx
			started := filepath.Join(t.TempDir(), "started")
			uncancellable := func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return exec.Command("sh", "-c", `touch "$1"; sleep 1.5`, "sh", started)
			}
			var seq func() bool
			var shutdown func() error
			if kind == "hotload" {
				r := &core.HotloadRunner{Options: &option.HotloadOption{Build: "x", Placeholder: "{}"}, BuildTimeout: time.Minute, ExecCommand: uncancellable}
				core.SetStopTimeoutsForTest(r, testGrace, testKill)
				seq = func() bool { return r.RunBuildSequence(0, "") }
				shutdown = func() error { return core.ShutdownForTest(r) }
			} else {
				w := &core.WatchRunner{Options: &option.WatchOption{Cmd: "x", Placeholder: "{}"}, CmdTimeout: time.Minute, ExecCommand: uncancellable}
				core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
				seq = func() bool { return w.RunBuildSequence(0, "") }
				shutdown = func() error { return core.WatchShutdownForTest(w) }
			}
			finished := make(chan struct{})
			go func() { seq(); close(finished) }()
			// wait until the command really runs (a fixed sleep is not enough under load)
			if !waitFor(t, 5*time.Second, func() bool { _, err := os.Stat(started); return err == nil }) {
				t.Fatal("the command did not start")
			}
			start := time.Now()
			if err := stopWithin(t, stopBound, shutdown); err != nil {
				t.Fatal(err)
			}
			if el := time.Since(start); el < 300*time.Millisecond {
				t.Fatalf("shutdown returned after %v while a command was still running", el)
			}
			select {
			case <-finished:
			case <-time.After(200 * time.Millisecond):
				t.Fatal("the command was still running after shutdown returned")
			}
		})
	}
}
