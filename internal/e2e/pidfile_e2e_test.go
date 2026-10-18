//go:build !windows

package e2e_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Pid file lifecycle of the enma binary:
//   P1 every start error after the pid file was written removes it (exit 1);
//   P2 a stop by signal (graceful or forced) removes it, in every mode;
//   P3 a pid file another process has written since is never removed;
//   P4 a pid file that cannot be written stops the start before anything runs.

func TestE2E_PidFile_RemovedOnStartErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"watch missing watch dir":   {"watch", "-c", "true", "-w", "./missing"},
		"hotload missing watch dir": {"hotload", "-d", "sleep 30", "-b", "true", "-w", "./missing"},
		"watch start-at-once fails": {"watch", "-c", "true", "--start-at-once", "exit 3"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolated(t, "")
			r := runEnma(t, dir, append(args, "--pid", "run/enma.pid")...)
			if r.code != 1 {
				t.Fatalf("exit %d, want 1; stderr: %s", r.code, r.stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, "run", "enma.pid")); !os.IsNotExist(err) {
				t.Fatalf("pid file left after a start error (%v); stderr: %s", err, r.stderr)
			}
		})
	}
}

func TestE2E_PidFile_RemovedOnSignalStop(t *testing.T) {
	for name, args := range map[string][]string{
		"watch":     {"watch", "-c", "true"},
		"hotload":   {"hotload", "-d", "sleep 30", "-b", "true"},
		"scheduler": {"scheduler", "--cron", "0 0 0 1 1 *", "--command", "true"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolated(t, "")
			pid := filepath.Join(dir, "enma.pid")
			p := startEnma(t, dir, nil, append(args, "--pid", "enma.pid")...)
			if !waitFor(10*time.Second, func() bool { return readPid(pid) == p.cmd.Process.Pid }) {
				t.Fatalf("no pid file with enma's pid:\n%s", p.out.String())
			}
			p.interrupt(t)
			if p.exitCode() != 0 {
				t.Fatalf("exit %d after SIGINT", p.exitCode())
			}
			if _, err := os.Stat(pid); !os.IsNotExist(err) {
				t.Fatalf("pid file left after the stop (%v):\n%s", err, p.out.String())
			}
		})
	}
}

// A second signal during a shutdown that waits for a process ignoring SIGTERM
// takes the forced path (exit 1) and still removes the pid file.
func TestE2E_PidFile_RemovedOnForcedStop(t *testing.T) {
	// the stubborn process records its pid; sleep keeps that pid (exec)
	stubborn := "trap '' TERM; echo $$ >> stubborn.pids; echo started > started; exec sleep 30"
	for name, args := range map[string][]string{
		"hotload":   {"hotload", "-d", stubborn, "-b", "true"},
		"scheduler": {"scheduler", "--cron", "* * * * * *", "--command", stubborn},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolated(t, "")
			pid := filepath.Join(dir, "enma.pid")
			p := startEnma(t, dir, nil, append(args, "--pid", "enma.pid")...)
			if !waitFor(10*time.Second, func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil }) {
				t.Fatalf("the stubborn process did not start:\n%s", p.out.String())
			}
			_ = p.cmd.Process.Signal(syscall.SIGINT)
			if !waitFor(5*time.Second, func() bool { return strings.Contains(p.out.String(), "send the signal again") }) {
				t.Fatalf("no graceful shutdown began:\n%s", p.out.String())
			}
			_ = p.cmd.Process.Signal(syscall.SIGINT) // within the 5s grace period
			if !p.exited(10 * time.Second) {
				t.Fatal("enma did not exit after the second signal")
			}
			if p.exitCode() != 1 || !strings.Contains(p.out.String(), "Forced shutdown") {
				t.Fatalf("exit %d, want 1 through the forced path:\n%s", p.exitCode(), p.out.String())
			}
			if _, err := os.Stat(pid); !os.IsNotExist(err) {
				t.Fatalf("pid file left after the forced stop (%v)", err)
			}
			for _, f := range lines(filepath.Join(dir, "stubborn.pids")) {
				n, _ := strconv.Atoi(f)
				if !waitFor(2*time.Second, func() bool { return !alive(n) }) {
					killPid(n)
					t.Fatalf("the process ignoring SIGTERM (pid %d) survived the forced stop", n)
				}
			}
		})
	}
}

func TestE2E_PidFile_OthersFileIsKept(t *testing.T) {
	dir := isolated(t, "")
	pid := filepath.Join(dir, "enma.pid")
	p := startEnma(t, dir, nil, "watch", "-c", "true", "--pid", "enma.pid")
	if !waitFor(10*time.Second, func() bool { return readPid(pid) == p.cmd.Process.Pid }) {
		t.Fatalf("no pid file:\n%s", p.out.String())
	}
	// another enma took the file over meanwhile; enma's own pid followed by a digit
	// held it as a substring, which the old check took for its own
	other := strconv.Itoa(p.cmd.Process.Pid) + "1"
	if err := os.WriteFile(pid, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	p.interrupt(t)
	if b, err := os.ReadFile(pid); err != nil || string(b) != other {
		t.Fatalf("the other process's pid file was touched: %q, %v", b, err)
	}
}

func TestE2E_PidFile_LiveOwnerIsReplacedWithAWarning(t *testing.T) {
	dir := isolated(t, "")
	pid := filepath.Join(dir, "enma.pid")
	if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil { // the test process is alive
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil, "watch", "-c", "true", "--pid", "enma.pid")
	if !waitFor(10*time.Second, func() bool { return readPid(pid) == p.cmd.Process.Pid }) {
		t.Fatalf("pid file not replaced:\n%s", p.out.String())
	}
	if !strings.Contains(p.out.String(), "which is still running") {
		t.Errorf("no warning about the live owner:\n%s", p.out.String())
	}
	p.interrupt(t)
}

func TestE2E_PidFile_UnwritableStopsTheStart(t *testing.T) {
	dir := isolated(t, "")
	if err := os.WriteFile(filepath.Join(dir, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := runEnma(t, dir, "watch", "-c", "touch ran", "--start-at-once", "touch started", "--pid", "blocker/enma.pid")
	if r.code != 1 || !strings.Contains(r.stderr, "blocker/enma.pid") {
		t.Fatalf("exit %d, stderr %q: want 1 naming the pid file", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
		t.Fatal("start-at-once ran although the pid file could not be written")
	}
}
