//go:build !windows

package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// How a running build / watch command is stopped, on every one of the six
// command paths (hotload PreBuild/Build/PostBuild, watch PreCmd/Cmd/PostCmd):
//   S1 on shutdown its process group gets SIGTERM and the grace period first (like the daemon);
//   S2 a group ignoring SIGTERM is killed after the grace period, descendants included;
//   S3 a force kill (second signal) cuts that grace period;
//   S4 a timeout kills the group at once;
//   S5 a retry wait does not delay shutdown;
//   S6 what a command leaves behind in its process group is stopped when it
//      ends: SIGTERM first, then the command's result counts (owner 2026-10-17);
//   S7 a leftover ignoring SIGTERM is killed after the grace period; once it is
//      gone the step's result is the command's own (only a group that cannot be
//      killed fails it).

type commandPath struct {
	name      string
	seq       func() bool
	shutdown  func() error
	forceKill func()
	handle    func() // change handler, with retries
}

// commandPaths puts command at each of the six steps (the other steps succeed).
func commandPaths(t *testing.T, command string, timeout, grace, kill time.Duration, retry int) []commandPath {
	t.Helper()
	var paths []commandPath
	for _, step := range []string{"PreBuild", "Build", "PostBuild"} {
		opt := &option.HotloadOption{Build: "true", Placeholder: "{}", Retry: retry}
		switch step {
		case "PreBuild":
			opt.PreBuild = command
		case "Build":
			opt.Build = command
		case "PostBuild":
			opt.PostBuild = command
		}
		r := &core.HotloadRunner{Options: opt, BuildTimeout: timeout, ExecCommand: exec.CommandContext}
		core.SetStopTimeoutsForTest(r, grace, kill)
		t.Cleanup(func() { core.ForceKillForTest(r) })
		paths = append(paths, commandPath{
			name:      "hotload/" + step,
			seq:       func() bool { return r.RunBuildSequence(0, "") },
			shutdown:  func() error { return core.ShutdownForTest(r) },
			forceKill: func() { core.ForceKillForTest(r) },
			handle:    func() { core.HandleChangeForTest(r, "") },
		})
	}
	for _, step := range []string{"PreCmd", "Cmd", "PostCmd"} {
		opt := &option.WatchOption{Cmd: "true", Placeholder: "{}", Retry: retry}
		switch step {
		case "PreCmd":
			opt.PreCmd = command
		case "Cmd":
			opt.Cmd = command
		case "PostCmd":
			opt.PostCmd = command
		}
		w := &core.WatchRunner{Options: opt, CmdTimeout: timeout, ExecCommand: exec.CommandContext}
		core.SetWatchStopTimeoutsForTest(w, grace, kill)
		t.Cleanup(func() { core.WatchForceKillForTest(w) })
		paths = append(paths, commandPath{
			name:      "watch/" + step,
			seq:       func() bool { return w.RunBuildSequence(0, "") },
			shutdown:  func() error { return core.WatchShutdownForTest(w) },
			forceKill: func() { core.WatchForceKillForTest(w) },
			handle:    func() { core.WatchHandleChangeForTest(w, "") },
		})
	}
	return paths
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// startSeq runs the command sequence in the background and waits until the
// command under test has written its pid.
func startSeq(t *testing.T, p commandPath, pids string) chan bool {
	t.Helper()
	result := make(chan bool, 1)
	go func() { result <- p.seq() }()
	if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) >= 1 }) {
		t.Fatal("command did not start")
	}
	return result
}

func expectAborted(t *testing.T, result chan bool) {
	t.Helper()
	select {
	case ok := <-result:
		if ok {
			t.Fatal("L5 violated: an aborted command reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("L1 violated: the running command did not return after shutdown")
	}
}

func expectAllGone(t *testing.T, pids string) {
	t.Helper()
	for _, pid := range readPids(t, pids) {
		if !waitFor(t, time.Second, func() bool { return !alive(pid) }) {
			t.Fatalf("L4 violated: process %d survived", pid)
		}
	}
}

func TestCommandStop_ShutdownSendsTermFirst(t *testing.T) {
	dir := t.TempDir()
	pids, mark := filepath.Join(dir, "pids"), filepath.Join(dir, "mark")
	killLeftovers(t, pids)
	command := `trap 'echo term > "` + mark + `"; exit 0' TERM; sleep 30 & echo $! >> '` + pids + `'; wait`
	for _, p := range commandPaths(t, command, time.Minute, 5*time.Second, testKill, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(mark)
			_ = os.Remove(pids)
			result := startSeq(t, p, pids)
			waitExeced(t, readPids(t, pids)[0], "sleep")
			start := time.Now()
			if err := stopWithin(t, stopBound, p.shutdown); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			if el := time.Since(start); el > 3*time.Second {
				t.Fatalf("S1 violated: a command exiting on SIGTERM took %v (grace 5s not cut short)", el)
			}
			if !exists(mark) {
				t.Fatal("S1 violated: the command was killed without SIGTERM first")
			}
			expectAborted(t, result)
			expectAllGone(t, pids)
		})
	}
}

func TestCommandStop_ShutdownKillsAfterGrace(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	command := `trap '' TERM; sleep 30 & echo $! >> '` + pids + `'; echo $$ >> '` + pids + `'; wait`
	// A kill wait shorter than the grace period must neither cut the grace
	// period nor make shutdown give up before it (grace > kill wait + 1s).
	const grace = 1500 * time.Millisecond
	for _, p := range commandPaths(t, command, time.Minute, grace, 100*time.Millisecond, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(pids)
			result := startSeq(t, p, pids)
			start := time.Now()
			if err := stopWithin(t, stopBound, p.shutdown); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			if el := time.Since(start); el < grace {
				t.Fatalf("S1 violated: killed after %v, before the grace period %v", el, grace)
			}
			expectAborted(t, result)
			expectAllGone(t, pids) // S2, leader and descendant
		})
	}
}

func TestCommandStop_ForceKillCutsTheGracePeriod(t *testing.T) {
	dir := t.TempDir()
	pids, mark, ready := filepath.Join(dir, "pids"), filepath.Join(dir, "mark"), filepath.Join(dir, "ready")
	killLeftovers(t, pids)
	// The leader notes SIGTERM and keeps running; its child ignores SIGTERM.
	// The pids are written once the child's trap is set (startSeq waits for them).
	command := `trap 'echo term >> "` + mark + `"' TERM; (trap '' TERM; : > '` + ready + `'; exec sleep 30) & ` +
		awaitReadySh(ready) + `; ` +
		`echo $! >> '` + pids + `'; echo $$ >> '` + pids + `'; while :; do wait; done`
	for _, p := range commandPaths(t, command, time.Minute, 5*time.Second, testKill, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(mark)
			_ = os.Remove(pids)
			_ = os.Remove(ready)
			result := startSeq(t, p, pids)
			done := make(chan error, 1)
			go func() { done <- p.shutdown() }() // graceful: would wait 5s
			// force kill only once the grace period has really begun
			if !waitFor(t, 2*time.Second, func() bool { return exists(mark) }) {
				t.Fatal("S1 violated: shutdown sent no SIGTERM")
			}
			start := time.Now()
			p.forceKill() // second signal
			expectAllGone(t, pids)
			if el := time.Since(start); el > 2*time.Second {
				t.Fatalf("S3 violated: force kill took %v", el)
			}
			expectAborted(t, result)
			select {
			case <-done:
			case <-time.After(stopBound):
				t.Fatal("graceful shutdown did not return after the force kill")
			}
		})
	}
}

func TestCommandStop_TimeoutKillsAtOnce(t *testing.T) {
	dir := t.TempDir()
	pids, mark := filepath.Join(dir, "pids"), filepath.Join(dir, "mark")
	killLeftovers(t, pids)
	command := `trap 'echo term > "` + mark + `"; exit 0' TERM; sleep 30 & echo $! >> '` + pids + `'; wait`
	for _, p := range commandPaths(t, command, 300*time.Millisecond, 5*time.Second, testKill, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(mark)
			_ = os.Remove(pids)
			start := time.Now()
			if p.seq() {
				t.Fatal("L5 violated: a timed-out command reported success")
			}
			if el := time.Since(start); el > 3*time.Second {
				t.Fatalf("S4 violated: a timed-out command took %v (waited for the grace period?)", el)
			}
			if exists(mark) {
				t.Fatal("S4 violated: a timed-out command got SIGTERM instead of SIGKILL")
			}
			expectAllGone(t, pids)
		})
	}
}

func TestCommandStop_RetryWaitDoesNotDelayShutdown(t *testing.T) {
	for _, p := range commandPaths(t, "exit 1", time.Minute, testGrace, testKill, 5) {
		t.Run(p.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() { p.handle(); close(done) }()
			time.Sleep(200 * time.Millisecond) // inside the 1s wait before the first retry
			if err := stopWithin(t, stopBound, p.shutdown); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("S5 violated: the change handler kept retrying after shutdown")
			}
		})
	}
}

func TestCommandStop_LeftoversStoppedWhenTheCommandEnds(t *testing.T) {
	dir := t.TempDir()
	pids, mark := filepath.Join(dir, "pids"), filepath.Join(dir, "mark")
	killLeftovers(t, pids)
	// the command leaves a background shell that notes SIGTERM itself, and exits
	// once that shell is ready (its trap set). The leftover runs short sleeps in
	// the foreground instead of a long background one: a SIGTERM reaching a
	// background child between its fork and its exec can be lost (CI7B-02), the
	// shell itself then waits only for the current short sleep.
	ready := filepath.Join(dir, "ready")
	command := `(trap 'echo term >> "` + mark + `"; exit 0' TERM; : > '` + ready + `'; while :; do sleep 0.05; done) & ` +
		`echo $! >> '` + pids + `'; ` + awaitReadySh(ready) + `; exit 0`
	for _, p := range commandPaths(t, command, time.Minute, 5*time.Second, testKill, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(mark)
			_ = os.Remove(pids)
			_ = os.Remove(ready)
			start := time.Now()
			if !p.seq() {
				t.Fatal("S6 violated: a command whose leftovers stopped on SIGTERM failed")
			}
			if el := time.Since(start); el > 3*time.Second {
				t.Fatalf("S6 violated: stopping a leftover that exits on SIGTERM took %v", el)
			}
			if !exists(mark) {
				t.Fatal("S6 violated: the leftover got no SIGTERM")
			}
			expectAllGone(t, pids)
		})
	}
}

func TestCommandStop_LeftoverIgnoringTermIsKilled(t *testing.T) {
	dir := t.TempDir()
	pids, ready := filepath.Join(dir, "pids"), filepath.Join(dir, "ready")
	killLeftovers(t, pids)
	// The leftover must ignore SIGTERM before the command returns (that is when
	// it gets SIGTERM): it reports ready once its trap is set, and the command
	// exits only then. Without this, a leftover still starting up dies on
	// SIGTERM at once — correct behaviour (S6), but not what S7 tests.
	command := `(trap '' TERM; : > '` + ready + `'; exec sleep 30) & echo $! >> '` + pids + `'; ` +
		awaitReadySh(ready) + `; exit 0`
	const grace = 500 * time.Millisecond
	for _, p := range commandPaths(t, command, time.Minute, grace, testKill, 0) {
		t.Run(p.name, func(t *testing.T) {
			_ = os.Remove(pids)
			_ = os.Remove(ready)
			start := time.Now()
			ok := p.seq()
			expectAllGone(t, pids)
			if el := time.Since(start); el < grace {
				t.Fatalf("S7 violated: killed after %v, before the grace period %v", el, grace)
			}
			if !ok {
				t.Fatal("S7 violated: a leftover killed after the grace period failed the step")
			}
		})
	}
}
