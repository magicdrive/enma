//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Diagnostics contract of the enma binary (H4):
//   D1 an error goes to stderr only and exits 1; a command line the flag parser
//      rejects exits 2 with a hint (Go's flag package); --help / --version exit 0;
//   D2 the message names what is wrong: the file, the line and column of a TOML
//      error, the missing flags or keys, the unknown subcommand — never "<nil>";
//   D3 an unknown key of a hotload / watch section is reported, not fatal (the
//      sections stay lenient);
//   D4 enma does not log the commands it runs (they may hold secrets);
//   D5 a closed stdout / stderr pipe does not kill enma: it keeps running (and
//      logging to --logs), and stops cleanly on a signal.

func TestE2E_Diagnostics_ErrorMessages(t *testing.T) {
	dir := isolated(t, "")
	write := func(name, body string) string {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	cases := []struct {
		name string
		args []string
		code int
		want []string
	}{
		{"missing --config file", []string{"-c", "none.toml"}, 1, []string{"none.toml", "no such file"}},
		{"unknown subcommand", []string{"bogus"}, 1, []string{`unknown subcommand "bogus"`, "enma --help"}},
		{"flags only, no config", []string{"-c", ""}, 1, []string{"can't find an enma config file", "enma init"}},
		{"unknown flag", []string{"--bogus"}, 2, []string{"-bogus", "enma --help"}},
		{"unknown watch flag", []string{"watch", "--nope"}, 2, []string{"-nope", "enma watch --help"}},
		{"hotload required flags", []string{"hotload"}, 1, []string{"--daemon (-d)", "--build (-b)"}},
		{"hotload one flag missing", []string{"hotload", "-d", "x"}, 1, []string{"missing required flags: --build (-b)"}},
		{"watch required flag", []string{"watch"}, 1, []string{"missing required flags: --command (-c)"}},
		{"TOML syntax", []string{"-c", write("syntax.toml", "[subcommand]\nuse = \"watch\"\n\n[x\n")}, 1,
			[]string{"syntax.toml: failed to parse config: line 4, column 3"}},
		{"unsupported mode", []string{"-c", write("mode.toml", "[subcommand]\nuse = \"bogus\"\n")}, 1,
			[]string{"mode.toml: unsupported subcommand: bogus"}},
		{"hotload required keys", []string{"-c", write("hl.toml", "[subcommand]\nuse = \"hotload\"\n[subcommand.hotload]\ndaemon = \"x\"\n")}, 1,
			[]string{"hl.toml: required fields missing in hotload config: build"}},
		{"watch required key", []string{"-c", write("w.toml", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\n")}, 1,
			[]string{"w.toml: required fields missing in watch config: command"}},
		{"scheduler job field", []string{"-c", write("s.toml", "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\n[[subcommand.scheduler.jobs]]\nname = \"n\"\ncron = \"bad\"\ncommand = \"true\"\n")}, 1,
			[]string{"s.toml: ", `scheduler job "n" (#1): cron:`}},
		{"init invalid mode", []string{"init", "--mode", "bogus"}, 1, []string{`invalid --mode "bogus"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runEnma(t, dir, tc.args...)
			if r.code != tc.code {
				t.Errorf("exit %d, want %d (stderr %q)", r.code, tc.code, r.stderr)
			}
			for _, w := range tc.want {
				if !strings.Contains(r.stderr, w) {
					t.Errorf("D2: stderr lacks %q:\n%s", w, r.stderr)
				}
			}
			if r.stdout != "" {
				t.Errorf("D1: an error printed to stdout: %q", r.stdout)
			}
			if strings.Contains(r.stderr, "<nil>") {
				t.Errorf("D2: %q", r.stderr)
			}
		})
	}
}

func TestE2E_Diagnostics_InitWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("precondition: run the tests as a non-root user (root can write a read-only directory)")
	}
	dir := isolated(t, "")
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	r := runEnma(t, dir, "init", "-f", "ro/Enma.toml")
	if r.code != 1 || !strings.Contains(r.stderr, "cannot create ro/Enma.toml") || r.stdout != "" {
		t.Fatalf("init into a read-only directory: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	r = runEnma(t, dir, "init", "-f", "ok.toml")
	if r.code != 0 || !strings.Contains(r.stdout, "Created ok.toml") {
		t.Fatalf("init: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestE2E_Diagnostics_UnknownKeyIsAWarning(t *testing.T) {
	dir := isolated(t, watchConf+"timout = \"3s\"\n")
	p := startEnma(t, dir, nil)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatalf("D3: enma did not start:\n%s", p.out.String())
	}
	if !strings.Contains(p.out.String(), `unknown key "timout" in [subcommand.watch]`) {
		t.Errorf("D3: no warning:\n%s", p.out.String())
	}
	p.interrupt(t)
}

func TestE2E_Diagnostics_CommandsAreNotLogged(t *testing.T) {
	const secret = "TOKEN-c0ffee-SECRET"
	cmd := "true # " + secret
	for name, args := range map[string][]string{
		"watch start-at-once": {"watch", "-c", cmd, "--start-at-once", cmd},
		"hotload":             {"hotload", "-d", "sleep 30 # " + secret, "-b", cmd, "--start-at-once", cmd},
		"scheduler":           {"scheduler", "--cron", "* * * * * *", "--command", cmd},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolated(t, "")
			p := startEnma(t, dir, nil, append(args, "--logs", "enma.log")...)
			ready := "👀 Watching"
			if name == "scheduler" {
				ready = "event=job_success"
			}
			if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), ready) }) {
				t.Fatalf("enma did not get going:\n%s", p.out.String())
			}
			if name == "hotload" && !waitFor(5*time.Second, func() bool { return strings.Contains(p.out.String(), "Start daemon") }) {
				t.Fatalf("no daemon:\n%s", p.out.String())
			}
			p.interrupt(t)
			logged, _ := os.ReadFile(filepath.Join(dir, "enma.log"))
			for where, text := range map[string]string{"output": p.out.String(), "log file": string(logged)} {
				if strings.Contains(text, secret) {
					t.Errorf("D4: the command text reached the %s:\n%s", where, text)
				}
			}
		})
	}
}

func TestE2E_Diagnostics_ClosedOutputPipeDoesNotKill(t *testing.T) {
	proj, _ := projectIn(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := startEnmaWithOutput(t, proj, w, "hotload", "-d", "echo $$ >> daemon.pids; exec sleep 30", "-b", "echo built >> builds",
		"-w", "src", "--logs", "enma.log", "--pid", "enma.pid", "--delay", "0sec")
	w.Close()
	if !waitFor(10*time.Second, func() bool { return count(filepath.Join(proj, "builds"), "built") >= 1 }) {
		t.Fatal("no first build")
	}
	r.Close() // the reader goes away: enma's next log line hits a broken pipe
	if err := os.WriteFile(filepath.Join(proj, "src", "a.go"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, func() bool { return count(filepath.Join(proj, "builds"), "built") >= 2 }) {
		t.Fatalf("D5 violated: no build after the pipe closed; enma exited=%v", p.exited(0))
	}
	if p.exited(500 * time.Millisecond) {
		t.Fatalf("D5 violated: enma died (exit %d) after its output pipe closed", p.exitCode())
	}
	// the build after the pipe closed ("Build success"; the first was "Build at start success")
	if !waitFor(5*time.Second, func() bool {
		logged, _ := os.ReadFile(filepath.Join(proj, "enma.log"))
		return strings.Contains(string(logged), "Build success")
	}) {
		logged, _ := os.ReadFile(filepath.Join(proj, "enma.log"))
		t.Errorf("D5 violated: the log file stopped with stdout:\n%s", logged)
	}
	_ = p.cmd.Process.Signal(syscall.SIGINT)
	if !p.exited(15 * time.Second) {
		t.Fatal("enma did not exit after SIGINT")
	}
	if p.exitCode() != 0 {
		t.Errorf("exit %d after SIGINT, want 0", p.exitCode())
	}
	if _, err := os.Stat(filepath.Join(proj, "enma.pid")); !os.IsNotExist(err) {
		t.Errorf("pid file left (%v)", err)
	}
	for _, s := range lines(filepath.Join(proj, "daemon.pids")) {
		pid := readPidString(s)
		if !waitFor(3*time.Second, func() bool { return !alive(pid) }) {
			killPid(pid)
			t.Errorf("D5 violated: daemon %d left behind", pid)
		}
	}
}

// startEnmaWithOutput runs enma with stdout and stderr on out (e.g. a pipe the
// test closes), in its own process group that the cleanup kills.
func startEnmaWithOutput(t *testing.T, dir string, out *os.File, args ...string) *enmaProc {
	t.Helper()
	cmd := exec.Command(enmaBin, args...)
	cmd.Dir = dir
	cmd.Env = enmaEnv()
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	startedMu.Lock()
	started = append(started, cmd.Process.Pid)
	startedMu.Unlock()
	p := &enmaProc{cmd: cmd, out: &syncBuffer{}, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
		forget(cmd.Process.Pid)
		for _, s := range lines(filepath.Join(dir, "daemon.pids")) {
			killPid(readPidString(s))
		}
	})
	return p
}

func readPidString(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// D4 on the failure paths (audit B4-08): an error about a command does not
// quote the command either.
func TestE2E_Diagnostics_FailingCommandsAreNotQuoted(t *testing.T) {
	const secret = "TOKEN-f41led-SECRET"
	dir := isolated(t, "")
	r := runEnma(t, dir, "watch", "-c", "true", "--start-at-once", "exit 7 # "+secret)
	if r.code != 1 || !strings.Contains(r.stderr, "start-at-once failed") {
		t.Fatalf("start-at-once failure: exit %d, stderr %q", r.code, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, secret) {
		t.Errorf("D4: a failing start-at-once was quoted:\n%s%s", r.stdout, r.stderr)
	}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"hotload build fails": {[]string{"hotload", "-d", "sleep 30", "-b", "exit 2 # " + secret}, "Build at start fail"},
		"scheduler job fails": {[]string{"scheduler", "--cron", "* * * * * *", "--command", "exit 3 # " + secret}, "event=job_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolated(t, "")
			p := startEnma(t, dir, nil, append(tc.args, "--logs", "enma.log")...)
			if !waitFor(15*time.Second, func() bool { return strings.Contains(p.out.String(), tc.want) }) {
				t.Fatalf("no %q:\n%s", tc.want, p.out.String())
			}
			p.interrupt(t)
			logged, _ := os.ReadFile(filepath.Join(dir, "enma.log"))
			if strings.Contains(p.out.String()+string(logged), secret) {
				t.Errorf("D4: a failing command was quoted:\n%s\n%s", p.out.String(), logged)
			}
		})
	}
}
