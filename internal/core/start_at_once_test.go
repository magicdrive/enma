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
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// start-at-once invariants:
//   O1 it runs at most once per runner: repeated calls, file changes, retries and
//      daemon restarts never run it again;
//   O2 an empty (or blank) command runs nothing;
//   O3 a non-zero exit or a failure to run it is an error;
//   O4 the placeholder is not processed: the command reaches the shell as written;
//   O5 nothing of its process group outlives it (leftovers are stopped, TERM first);
//   O6 shutdown stops it like any other command (SIGTERM, grace, SIGKILL; a force kill at once)
//      and reports errShuttingDown.

type startAtOnceRunner struct {
	name     string
	hotload  *core.HotloadRunner // set for "hotload"
	watch    *core.WatchRunner   // set for "watch"
	run      func() error
	change   func() // the real change handler
	shutdown func() error
	force    func()
}

// startAtOnceRunners returns a hotload and a watch runner with command as start-at-once;
// their build / command steps succeed.
func startAtOnceRunners(t *testing.T, command string, retry int) []startAtOnceRunner {
	t.Helper()
	dir := t.TempDir()
	daemonPids := filepath.Join(dir, "daemon-pids")
	killLeftovers(t, daemonPids)

	h := newChangeRunner(t, `echo $$ >> '`+daemonPids+`'; exec sleep 1000`)
	h.Options.StartAtOnce = command
	h.Options.Retry = retry

	wopt := HelperInflateWatchOptionDefaultValue(&option.WatchOption{Cmd: "true", Placeholder: "{}", Retry: retry})
	wopt.StartAtOnce = command
	w := core.NewWatchRunner(wopt)
	w.CmdTimeout = 10 * time.Second
	core.SetWatchStopTimeoutsForTest(w, testGrace, testKill)
	t.Cleanup(func() { core.WatchForceKillForTest(w) })

	return []startAtOnceRunner{
		{
			name:    "hotload",
			hotload: h,
			run:     func() error { return core.RunStartAtOnceForTest(h) },
			change: func() {
				core.HandleChangeForTest(h, "")
			},
			shutdown: func() error { return core.ShutdownForTest(h) },
			force:    func() { core.ForceKillForTest(h) },
		},
		{
			name:     "watch",
			watch:    w,
			run:      func() error { return core.WatchRunStartAtOnceForTest(w) },
			change:   func() { core.WatchHandleChangeForTest(w, "") },
			shutdown: func() error { return core.WatchShutdownForTest(w) },
			force:    func() { core.WatchForceKillForTest(w) },
		},
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func TestStartAtOnce_RunsOnceAcrossCallsAndChanges(t *testing.T) {
	for _, tc := range []string{"hotload", "watch"} {
		t.Run(tc, func(t *testing.T) {
			marks := filepath.Join(t.TempDir(), "marks")
			for _, r := range startAtOnceRunners(t, `echo run >> '`+marks+`'`, 1) {
				if r.name != tc {
					continue
				}
				for i := 0; i < 3; i++ {
					if err := r.run(); err != nil {
						t.Fatalf("call %d: %v", i, err)
					}
				}
				for i := 0; i < 3; i++ { // hotload: build + daemon restart each time
					r.change()
				}
				if n := countLines(t, marks); n != 1 {
					t.Fatalf("O1 violated: start-at-once ran %d times", n)
				}
			}
		})
	}
}

func TestStartAtOnce_FailureIsReturnedAndNotRetried(t *testing.T) {
	marks := filepath.Join(t.TempDir(), "marks")
	for _, r := range startAtOnceRunners(t, `echo run >> '`+marks+`'; exit 3`, 2) {
		t.Run(r.name, func(t *testing.T) {
			before := countLines(t, marks)
			err := r.run()
			if err == nil || !strings.Contains(err.Error(), "start-at-once failed") || !strings.Contains(err.Error(), "exit status 3") {
				t.Fatalf("O3 violated: got %v", err)
			}
			if again := r.run(); again == nil || again.Error() != err.Error() {
				t.Fatalf("second call: got %v, want the first error", again)
			}
			if n := countLines(t, marks) - before; n != 1 {
				t.Fatalf("retry must not apply: ran %d times", n)
			}
		})
	}
}

func TestStartAtOnce_EmptyRunsNothing(t *testing.T) {
	for _, command := range []string{"", "   ", "\n\t"} {
		called := false
		mock := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return exec.CommandContext(ctx, "false")
		}
		h := core.NewHotloadRunner(HelperInflateHotloadOptionDefaultValue(&option.HotloadOption{Build: "true", Daemon: "true", StartAtOnce: command}))
		h.ExecCommand = mock
		w := core.NewWatchRunner(HelperInflateWatchOptionDefaultValue(&option.WatchOption{Cmd: "true", StartAtOnce: command}))
		w.ExecCommand = mock
		if err := core.RunStartAtOnceForTest(h); err != nil {
			t.Fatalf("hotload %q: %v", command, err)
		}
		if err := core.WatchRunStartAtOnceForTest(w); err != nil {
			t.Fatalf("watch %q: %v", command, err)
		}
		if called {
			t.Fatalf("O2 violated: %q ran a command", command)
		}
	}
}

func TestStartAtOnce_ExecFailureIsAnError(t *testing.T) {
	missing := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "no-such-shell"), args...)
	}
	h := core.NewHotloadRunner(HelperInflateHotloadOptionDefaultValue(&option.HotloadOption{Build: "true", Daemon: "true", StartAtOnce: "true"}))
	h.ExecCommand = missing
	w := core.NewWatchRunner(HelperInflateWatchOptionDefaultValue(&option.WatchOption{Cmd: "true", StartAtOnce: "true"}))
	w.ExecCommand = missing
	for name, err := range map[string]error{"hotload": core.RunStartAtOnceForTest(h), "watch": core.WatchRunStartAtOnceForTest(w)} {
		if err == nil || !strings.Contains(err.Error(), "start-at-once failed") {
			t.Fatalf("O3 violated (%s): got %v", name, err)
		}
	}
}

func TestStartAtOnce_PlaceholderIsNotProcessed(t *testing.T) {
	for _, ph := range []string{"{}", "%%", "FILE"} {
		out := filepath.Join(t.TempDir(), "out")
		command := `printf '%s|%s|%s\n' "` + ph + `" "$1" "$#" > '` + out + `'; printf '%s\n' ` + ph + ` >> '` + out + `'`
		h := core.NewHotloadRunner(HelperInflateHotloadOptionDefaultValue(&option.HotloadOption{Build: "true", Daemon: "true", Placeholder: ph, StartAtOnce: command}))
		if err := core.RunStartAtOnceForTest(h); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		if want := ph + "||0\n" + ph + "\n"; string(b) != want {
			t.Fatalf("O4 violated for %q: got %q, want %q", ph, b, want)
		}
	}
}

func TestStartAtOnce_RunsInTheCurrentDirectory(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Chdir(dir) // the subcommands chdir to --working-dir before Start
	w := core.NewWatchRunner(HelperInflateWatchOptionDefaultValue(&option.WatchOption{Cmd: "true", StartAtOnce: "pwd -P > wd"}))
	if err := core.WatchRunStartAtOnceForTest(w); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "wd"))
	if err != nil || strings.TrimSpace(string(b)) != dir {
		t.Fatalf("got %q (%v), want %q", b, err, dir)
	}
}

func TestStartAtOnce_LeftoversAreStopped(t *testing.T) {
	cases := map[string]string{
		"term":        `sleep 1000 & echo $! >> '%s'`,
		"ignore-term": `sh -c "trap '' TERM; echo \$\$ >> '%s'; while :; do sleep 0.05; done" & sleep 0.2`,
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			pids := filepath.Join(t.TempDir(), "pids")
			killLeftovers(t, pids)
			for _, r := range startAtOnceRunners(t, strings.ReplaceAll(tmpl, "%s", pids), 0) {
				before := len(readPids(t, pids))
				if err := stopWithin(t, stopBound, r.run); err != nil {
					t.Fatalf("%s: %v", r.name, err)
				}
				all := readPids(t, pids)
				if len(all) != before+1 {
					t.Fatalf("%s: want one leftover pid, got %v", r.name, all)
				}
				if alive(all[len(all)-1]) {
					t.Fatalf("O5 violated (%s): pid %d outlived start-at-once", r.name, all[len(all)-1])
				}
			}
		})
	}
}

func TestStartAtOnce_ShutdownStopsItTermFirst(t *testing.T) {
	dir := t.TempDir()
	pids, mark := filepath.Join(dir, "pids"), filepath.Join(dir, "term")
	killLeftovers(t, pids)
	// it takes a while to exit after SIGTERM: shutdown must wait for it
	command := `trap 'echo term >> "` + mark + `"; sleep 0.5; exit 0' TERM; sleep 1000 & echo $! >> '` + pids + `'; wait`
	for _, r := range startAtOnceRunners(t, command, 0) {
		t.Run(r.name, func(t *testing.T) {
			n := len(readPids(t, pids))
			done := make(chan error, 1)
			go func() { done <- r.run() }()
			if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == n+1 }) {
				t.Fatal("start-at-once did not start")
			}
			if err := stopWithin(t, stopBound, r.shutdown); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			select {
			case err := <-done: // shutdown waits for start-at-once to return
				if !errors.Is(err, core.ErrShuttingDownForTest) {
					t.Fatalf("O6: got %v, want shutting down", err)
				}
			case <-time.After(100 * time.Millisecond): // only the hand-over after its release
				t.Fatal("O6 violated: shutdown returned while start-at-once was still running")
			}
			if b, _ := os.ReadFile(mark); !strings.Contains(string(b), "term") {
				t.Fatal("O6 violated: no SIGTERM before the kill")
			}
			for _, pid := range readPids(t, pids) {
				if alive(pid) {
					t.Fatalf("O6 violated: %d survived shutdown", pid)
				}
			}
		})
	}
}

func TestStartAtOnce_LeftoversGetTermFirst(t *testing.T) {
	dir := t.TempDir()
	pids, mark := filepath.Join(dir, "pids"), filepath.Join(dir, "term")
	killLeftovers(t, pids)
	command := `sh -c "trap 'echo term >> ` + mark + `; exit 0' TERM; echo \$\$ >> ` + pids + `; while :; do sleep 0.05; done" & sleep 0.2`
	for _, r := range startAtOnceRunners(t, command, 0) {
		if err := stopWithin(t, stopBound, r.run); err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
	}
	if b, _ := os.ReadFile(mark); strings.Count(string(b), "term") != 2 {
		t.Fatalf("O5: leftovers were not sent SIGTERM first: %q", b)
	}
}

func TestStartAtOnce_ForceKillCutsTheGrace(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	command := `trap '' TERM; echo $$ >> '` + pids + `'; while :; do sleep 0.05; done`
	for _, r := range startAtOnceRunners(t, command, 0) {
		t.Run(r.name, func(t *testing.T) {
			n := len(readPids(t, pids))
			done := make(chan error, 1)
			go func() { done <- r.run() }()
			if !waitFor(t, 2*time.Second, func() bool { return len(readPids(t, pids)) == n+1 }) {
				t.Fatal("start-at-once did not start")
			}
			go func() { _ = r.shutdown() }()
			time.Sleep(50 * time.Millisecond)
			began := time.Now()
			r.force()
			select {
			case <-done:
			case <-time.After(testKill + time.Second):
				t.Fatal("force kill did not end start-at-once")
			}
			if d := time.Since(began); d >= testGrace+testKill {
				t.Fatalf("force kill waited %v", d)
			}
			for _, pid := range readPids(t, pids) {
				if alive(pid) {
					t.Fatalf("%d survived a force kill", pid)
				}
			}
		})
	}
}

// A force kill also cuts the grace period of stopping leftovers after the command returned.
func TestStartAtOnce_ForceKillCutsTheLeftoverGrace(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	const grace = 5 * time.Second
	command := `sh -c "trap '' TERM; echo \$\$ >> ` + pids + `; while :; do sleep 0.05; done" & sleep 0.2`
	for _, r := range startAtOnceRunners(t, command, 0) {
		t.Run(r.name, func(t *testing.T) {
			n := len(readPids(t, pids))
			if r.name == "hotload" {
				core.SetStopTimeoutsForTest(r.hotload, grace, testKill)
			} else {
				core.SetWatchStopTimeoutsForTest(r.watch, grace, testKill)
			}
			done := make(chan error, 1)
			go func() { done <- r.run() }()
			if !waitFor(t, 3*time.Second, func() bool { return len(readPids(t, pids)) == n+1 }) {
				t.Fatal("start-at-once did not start")
			}
			// the leader exits after 0.2s; then the leftover ignores SIGTERM and its grace runs
			time.Sleep(600 * time.Millisecond)
			select {
			case err := <-done:
				t.Fatalf("returned before the grace ran out: %v", err)
			default:
			}
			go func() { _ = r.shutdown() }()
			began := time.Now()
			r.force()
			select {
			case <-done:
			case <-time.After(grace):
				t.Fatal("force kill did not cut the leftover grace")
			}
			if d := time.Since(began); d >= grace/2 {
				t.Fatalf("force kill waited %v", d)
			}
			for _, pid := range readPids(t, pids)[n:] {
				if alive(pid) {
					t.Fatalf("%d survived a force kill", pid)
				}
			}
		})
	}
}

func TestStartAtOnce_StartStepRemovesPidFileOnFailureOnly(t *testing.T) {
	dir := t.TempDir()
	pid := filepath.Join(dir, "enma.pid")
	for name, tc := range map[string]struct {
		err     error
		pidKept bool
	}{
		"ok":       {nil, true},
		"failed":   {errors.New("start-at-once failed: exit status 1"), false},
		"shutdown": {core.ErrShuttingDownForTest, true},
	} {
		// CreatePidFile writes enma's own pid; DeletePidFile removes only such a file.
		if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := core.StartAtOnceInStartForTest(func() error { return tc.err }, pid)
		if !errors.Is(err, tc.err) && err != tc.err {
			t.Fatalf("%s: got %v, want %v", name, err, tc.err)
		}
		if _, statErr := os.Stat(pid); (statErr == nil) != tc.pidKept {
			t.Fatalf("%s: pid file kept=%v, want %v", name, statErr == nil, tc.pidKept)
		}
	}
}

func TestStartAtOnceArgs(t *testing.T) {
	for _, command := range []string{"make prepare", `echo "{}" '$1'`, "a; b && c | d", ""} {
		got := core.StartAtOnceArgs(command)
		if len(got) != 3 || got[0] != "-c" || got[1] != command || got[2] != core.ShellArgv0 {
			t.Fatalf("StartAtOnceArgs(%q) = %q", command, got)
		}
	}
}
