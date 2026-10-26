//go:build !windows

package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exit codes of the enma binary:
//   X1 a failing start exits 1, also for `enma` without arguments (default invocation);
//   X2 the error goes to stderr, once, and nothing of it to stdout;
//   X3 --help / --version exit 0;
//   X4 a running enma stopped by a signal exits 0, as before.

type result struct {
	code           int
	stdout, stderr string
}

// runEnma runs enma in dir (an isolated temporary directory) and waits for it to
// exit; one still running after the timeout has its process group killed and
// fails the test.
func runEnma(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(enmaBin, args...)
	cmd.Dir = dir
	cmd.Env = enmaEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	startedMu.Lock()
	started = append(started, pgid)
	startedMu.Unlock()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(15 * time.Second):
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		<-done
		forget(pgid)
		t.Fatalf("enma %q did not exit; stderr:\n%s", args, stderr.String())
	}
	forget(pgid)
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("wait: %v", err)
		}
		code = ee.ExitCode()
	}
	return result{code, stdout.String(), stderr.String()}
}

// isolated returns an empty directory with the given Enma.toml (none when conf is "").
func isolated(t *testing.T, conf string) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	// a daemon runs in its own process group: if a test ends early, enma's cleanup does not reach it
	t.Cleanup(func() {
		b, _ := os.ReadFile(filepath.Join(dir, "daemon.pids"))
		for _, s := range strings.Fields(string(b)) {
			n, _ := strconv.Atoi(s)
			killPid(n) // ignores pid <= 0
		}
	})
	if conf != "" {
		if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// expectFailure checks X1 and X2 for one run.
func expectFailure(t *testing.T, r result, msg string) {
	t.Helper()
	if r.code != 1 {
		t.Errorf("exit code %d, want 1 (stderr: %q)", r.code, r.stderr)
	}
	if n := strings.Count(r.stderr, msg); n != 1 {
		t.Errorf("stderr has %q %d times, want once:\n%s", msg, n, r.stderr)
	}
	if strings.Contains(r.stdout, msg) {
		t.Errorf("the error went to stdout too:\n%s", r.stdout)
	}
}

const (
	watchConf   = "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nwatch_dir = [\".\"]\n"
	hotloadConf = "[subcommand]\nuse = \"hotload\"\n[subcommand.hotload]\ndaemon = \"echo $$ >> daemon.pids; exec sleep 1000\"\nbuild = \"true\"\nwatch_dir = [\".\"]\n"
)

func TestE2E_ExitCode_DefaultInvocationFailures(t *testing.T) {
	cases := []struct {
		name, conf, msg string
	}{
		{"no config file", "", "can't find an enma config file"}, // wording fixed in H4 (was "Cann't find enma config file")
		{"invalid TOML", "[subcommand\nuse = ", "failed to parse config"},
		{"unsupported mode", "[subcommand]\nuse = \"bogus\"\n", "unsupported subcommand: bogus"},
		{"empty mode", "[subcommand]\n", "unsupported subcommand: "},
		{"watch required field missing", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\n", "required fields missing in watch config"},
		{"hotload required field missing", "[subcommand]\nuse = \"hotload\"\n[subcommand.hotload]\ndaemon = \"x\"\n", "required fields missing in hotload config"},
		{"invalid option value", watchConf + "timeout = \"soon\"\n", "--timeout"},
		{"watch start-at-once exit 7", watchConf + "start_at_once = \"exit 7\"\n", "start-at-once failed: exit status 7"},
		{"hotload start-at-once exit 7", hotloadConf + "start_at_once = \"exit 7\"\n", "start-at-once failed: exit status 7"},
		{"runner: working dir missing", watchConf + "working_dir = \"./no-such-dir\"\n", "no-such-dir"},
		{"runner: watch dir missing", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nwatch_dir = [\"no-such-dir\"]\n", "no-such-dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolated(t, tc.conf)
			r := runEnma(t, dir)
			expectFailure(t, r, tc.msg)
			if !strings.Contains(r.stderr, "enma --help") { // the short help still follows the error
				t.Errorf("short help missing from stderr:\n%s", r.stderr)
			}
		})
	}
}

func TestE2E_ExitCode_ConfigOptionFailures(t *testing.T) {
	dir := isolated(t, "")
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name string
		args []string
		msg  string
	}{
		// H4: the message names the file and the cause (it used to be "failed load configfile: <nil>")
		{"missing file", []string{"--config", filepath.Join(dir, "none.toml")}, "failed load configfile"},
		{"invalid TOML", []string{"--config", write("bad.toml", "[x")}, "failed to parse config"},
		{"unsupported mode", []string{"-c", write("mode.toml", "[subcommand]\nuse = \"bogus\"\n")}, "unsupported subcommand: bogus"},
		{"start-at-once exit 7", []string{"--config", write("sao.toml", watchConf+"start_at_once = \"exit 7\"\n")}, "start-at-once failed: exit status 7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectFailure(t, runEnma(t, dir, tc.args...), tc.msg)
		})
	}
}

func TestE2E_ExitCode_SubcommandFailures(t *testing.T) {
	dir := isolated(t, "")
	cases := []struct {
		name string
		args []string
		msg  string
	}{
		{"watch start-at-once exit 7", []string{"watch", "--command", "true", "--watch-dir", ".", "--start-at-once", "exit 7"}, "start-at-once failed: exit status 7"},
		{"hotload start-at-once exit 7", []string{"hotload", "--daemon", "true", "--build", "true", "--watch-dir", ".", "--start-at-once", "exit 7"}, "start-at-once failed: exit status 7"},
		{"watch dir missing", []string{"watch", "--command", "true", "--watch-dir", "no-such-dir"}, "no-such-dir"},
		{"hotload working dir missing", []string{"hotload", "--daemon", "true", "--build", "true", "--working-dir", filepath.Join(dir, "no-such-dir")}, "no-such-dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectFailure(t, runEnma(t, dir, tc.args...), tc.msg)
		})
	}
	// a missing required flag is an error (stderr, exit 1); since H4 without the usage on stdout
	if r := runEnma(t, dir, "watch"); r.code != 1 {
		t.Errorf("enma watch without --command: exit %d, want 1", r.code)
	}
	if r := runEnma(t, dir, "hotload", "--daemon", "x"); r.code != 1 {
		t.Errorf("enma hotload without --build: exit %d, want 1", r.code)
	}
}

func TestE2E_ExitCode_HelpAndVersion(t *testing.T) {
	dir := isolated(t, "") // no config: --help / --version must not need one
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"-v"}, {"watch", "--help"}, {"hotload", "-h"}} {
		r := runEnma(t, dir, args...)
		if r.code != 0 || r.stdout == "" || r.stderr != "" {
			t.Errorf("enma %q: exit %d, stdout %d bytes, stderr %q; want 0, output on stdout only", args, r.code, len(r.stdout), r.stderr)
		}
	}
	if r := runEnma(t, dir, "--version"); !strings.HasPrefix(r.stdout, "enma version ") {
		t.Errorf("--version printed %q", r.stdout)
	}
}

// X4: a successful default invocation keeps running; a signal stops it with 0.
func TestE2E_ExitCode_DefaultInvocationSignalStopIsZero(t *testing.T) {
	for _, tc := range []struct {
		name, conf string
		sig        syscall.Signal
	}{
		{"watch SIGINT", watchConf, syscall.SIGINT},
		{"watch SIGTERM", watchConf, syscall.SIGTERM},
		{"hotload SIGINT", hotloadConf + "start_at_once = \"true\"\n", syscall.SIGINT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolated(t, tc.conf)
			p := startEnma(t, dir, nil)
			if !waitFor(10*time.Second, monitoring(p)) {
				t.Fatal("enma did not start monitoring")
			}
			if p.exited(300 * time.Millisecond) {
				t.Fatalf("enma exited by itself: %d", p.exitCode())
			}
			_ = p.cmd.Process.Signal(tc.sig)
			if !p.exited(15 * time.Second) {
				t.Fatal("enma did not exit after the signal")
			}
			if p.exitCode() != 0 {
				t.Fatalf("exit code %d after %v, want 0", p.exitCode(), tc.sig)
			}
		})
	}
}
