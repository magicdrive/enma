//go:build !windows

// Package e2e runs the enma binary against real processes in temporary directories.
package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var enmaBin string

// started records every enma this test binary started (its process group), so
// that TestMain can stop them even when a test ends without its cleanup.
var (
	startedMu sync.Mutex
	started   []int
)

func forget(pgid int) {
	startedMu.Lock()
	defer startedMu.Unlock()
	for i, p := range started {
		if p == pgid {
			started = append(started[:i], started[i+1:]...)
			return
		}
	}
}

func stopStarted() {
	startedMu.Lock()
	defer startedMu.Unlock()
	for _, pgid := range started {
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
}

func TestMain(m *testing.M) {
	code := func() int {
		tmp, err := os.MkdirTemp("", "enma-e2e-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(tmp)
		_, file, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(file), "..", "..")
		enmaBin = filepath.Join(tmp, "enma")
		build := exec.Command("go", "build", "-o", enmaBin, ".")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build enma: %v\n%s", err, out)
			return 1
		}
		defer stopStarted()
		return m.Run()
	}()
	os.Exit(code)
}

// syncBuffer collects enma's output.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type enmaProc struct {
	cmd  *exec.Cmd
	out  *syncBuffer
	done chan struct{}
	err  error
}

// startEnma runs enma in dir. It gets its own process group, so the test's
// cleanup can kill whatever is left; a Ctrl+C is simulated with SIGINT to enma.
func startEnma(t *testing.T, dir string, env []string, args ...string) *enmaProc {
	t.Helper()
	cmd := exec.Command(enmaBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// a descendant in another process group may keep the output pipe open: do not wait for it forever
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	startedMu.Lock()
	started = append(started, cmd.Process.Pid)
	startedMu.Unlock()
	p := &enmaProc{cmd: cmd, out: out, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
		forget(cmd.Process.Pid) // reaped: its pid may be reused, never signal it again
		if t.Failed() {
			t.Logf("enma output:\n%s", out.String())
		}
	})
	return p
}

func (p *enmaProc) exited(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (p *enmaProc) exitCode() int {
	if p.err == nil {
		return 0
	}
	if ee, ok := p.err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// interrupt simulates Ctrl+C and expects enma to exit in time.
func (p *enmaProc) interrupt(t *testing.T) {
	t.Helper()
	_ = p.cmd.Process.Signal(syscall.SIGINT)
	if !p.exited(15 * time.Second) {
		t.Fatal("enma did not exit after SIGINT")
	}
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func lines(path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Fields(string(b))
}

func count(path, word string) int {
	n := 0
	for _, l := range lines(path) {
		if l == word {
			n++
		}
	}
	return n
}

func alive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// killPid kills one recorded process. pid 0 or below would signal a whole
// process group (the test's own), so it is never sent.
func killPid(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func readPid(path string) int {
	b, _ := os.ReadFile(path)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// project makes a temporary project with a watched src directory; log is the
// file every command appends its marker to.
func project(t *testing.T) (dir, src, log string) {
	t.Helper()
	dir, _ = filepath.EvalSymlinks(t.TempDir())
	src = filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// daemons and start-at-once children are in their own process groups: if a
	// test ends early, enma's cleanup does not reach them
	t.Cleanup(func() {
		for _, f := range []string{"daemon.pids", "child", "leader"} {
			b, _ := os.ReadFile(filepath.Join(dir, f))
			for _, s := range strings.Fields(string(b)) {
				n, _ := strconv.Atoi(s)
				killPid(n)
			}
		}
	})
	return dir, src, filepath.Join(dir, "log")
}

// touch changes a watched file (new content each time) and waits until want
// holds, so that each change is handled on its own.
func touch(t *testing.T, src string, i int, want func() bool) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), fmt.Appendf(nil, "change %d\n", i), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, want) {
		t.Fatalf("change %d was not handled", i)
	}
}

func monitoring(p *enmaProc) func() bool {
	return func() bool { return strings.Contains(p.out.String(), "👀 Watching") }
}

func watchArgs(dir, startAtOnce string, extra ...string) []string {
	args := []string{"watch", "--working-dir", dir, "--watch-dir", "src", "--command", "echo C >> log", "--delay", "0sec"}
	if startAtOnce != "\x00unset" {
		args = append(args, "--start-at-once", startAtOnce)
	}
	return append(args, extra...)
}

func hotloadArgs(dir, startAtOnce string, extra ...string) []string {
	args := []string{"hotload", "--working-dir", dir, "--watch-dir", "src",
		"--build", "echo B >> log", "--daemon", "echo D >> log; echo $$ >> daemon.pids; exec sleep 1000", "--delay", "0sec"}
	if startAtOnce != "\x00unset" {
		args = append(args, "--start-at-once", startAtOnce)
	}
	return append(args, extra...)
}

const unset = "\x00unset"

func TestE2E_Watch_CLI_OnceBeforeMonitoring(t *testing.T) {
	dir, src, log := project(t)
	// it also changes a watched file: monitoring starts after it, so no command runs for that
	p := startEnma(t, dir, nil, watchArgs(dir, "echo S >> log; echo x > src/generated.txt")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	if got := lines(log); len(got) != 1 || got[0] != "S" {
		t.Fatalf("at start: got %v, want [S] (command must not run at start)", got)
	}
	time.Sleep(800 * time.Millisecond)
	if count(log, "C") != 0 {
		t.Fatal("a change made by start-at-once triggered the command")
	}
	for i := 1; i <= 3; i++ {
		touch(t, src, i, func() bool { return count(log, "C") == i })
	}
	if count(log, "S") != 1 || lines(log)[0] != "S" {
		t.Fatalf("start-at-once ran again: %v", lines(log))
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit code %d after Ctrl+C", p.exitCode())
	}
}

func TestE2E_Watch_TOML_OnceBeforeMonitoring(t *testing.T) {
	for name, key := range map[string]string{"start_at_once": "start_at_once", "start-at-once": "start-at-once"} {
		t.Run(name, func(t *testing.T) {
			dir, src, log := project(t)
			conf := "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"echo C >> log\"\n" +
				key + " = \"echo S >> log\"\nwatch_dir = [\"src\"]\ndelay = \"0sec\"\n"
			if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
				t.Fatal(err)
			}
			p := startEnma(t, dir, nil) // bare enma reads ./Enma.toml
			if !waitFor(10*time.Second, monitoring(p)) {
				t.Fatal("monitoring did not start")
			}
			if got := lines(log); len(got) != 1 || got[0] != "S" {
				t.Fatalf("at start: got %v, want [S]", got)
			}
			for i := 1; i <= 2; i++ {
				touch(t, src, i, func() bool { return count(log, "C") == i })
			}
			if count(log, "S") != 1 {
				t.Fatalf("start-at-once ran again: %v", lines(log))
			}
			p.interrupt(t)
		})
	}
}

func TestE2E_Hotload_CLI_BeforeFirstBuild_OnceAcrossRestarts(t *testing.T) {
	dir, src, log := project(t)
	p := startEnma(t, dir, nil, hotloadArgs(dir, "echo S >> log")...)
	if !waitFor(10*time.Second, func() bool { return count(log, "D") == 1 }) {
		t.Fatal("daemon did not start")
	}
	if got := strings.Join(lines(log), ","); got != "S,B,D" {
		t.Fatalf("start order: got %s, want S,B,D", got)
	}
	for i := 1; i <= 3; i++ { // each change: build, daemon restart
		touch(t, src, i, func() bool { return count(log, "D") == i+1 })
	}
	if count(log, "S") != 1 || count(log, "B") != 4 {
		t.Fatalf("got %v, want one S and four builds", lines(log))
	}
	p.interrupt(t)
}

func TestE2E_Hotload_TOML_BeforeFirstBuild(t *testing.T) {
	dir, _, log := project(t)
	conf := "[subcommand]\nuse = \"hotload\"\n[subcommand.hotload]\ndaemon = \"echo D >> log; echo $$ >> daemon.pids; exec sleep 1000\"\n" +
		"build = \"echo B >> log\"\nbuild_at_start = true\nstart_at_once = \"echo S >> log\"\nwatch_dir = [\"src\"]\ndelay = \"0sec\"\n"
	cfg := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(cfg, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil, "--config", cfg)
	if !waitFor(10*time.Second, func() bool { return count(log, "D") == 1 }) {
		t.Fatal("daemon did not start")
	}
	if got := strings.Join(lines(log), ","); got != "S,B,D" {
		t.Fatalf("start order: got %s, want S,B,D", got)
	}
	p.interrupt(t)
}

func TestE2E_Hotload_BuildAtStartOff_KeepsItsBehaviour(t *testing.T) {
	dir, src, log := project(t)
	p := startEnma(t, dir, nil, hotloadArgs(dir, "echo S >> log", "--build-at-start", "off")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	time.Sleep(500 * time.Millisecond)
	if got := strings.Join(lines(log), ","); got != "S" {
		t.Fatalf("with build-at-start off: got %s, want only S", got)
	}
	touch(t, src, 1, func() bool { return count(log, "D") == 1 })
	if got := strings.Join(lines(log), ","); got != "S,B,D" {
		t.Fatalf("after a change: got %s, want S,B,D", got)
	}
	p.interrupt(t)
}

func TestE2E_NotSetOrEmpty_RunsNothing(t *testing.T) {
	for _, value := range []string{unset, ""} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			dir, src, log := project(t)
			p := startEnma(t, dir, nil, watchArgs(dir, value)...)
			if !waitFor(10*time.Second, monitoring(p)) {
				t.Fatal("monitoring did not start")
			}
			if strings.Contains(p.out.String(), "start-at-once") {
				t.Fatal("start-at-once ran")
			}
			touch(t, src, 1, func() bool { return count(log, "C") == 1 })
			p.interrupt(t)

			dir, _, log = project(t)
			h := startEnma(t, dir, nil, hotloadArgs(dir, value)...)
			if !waitFor(10*time.Second, func() bool { return count(log, "D") == 1 }) {
				t.Fatal("daemon did not start")
			}
			if got := strings.Join(lines(log), ","); got != "B,D" {
				t.Fatalf("hotload start: got %s, want B,D", got)
			}
			h.interrupt(t)
		})
	}
}

func TestE2E_Failure_StopsStartup(t *testing.T) {
	type mode struct {
		name string
		args func(dir string) []string
	}
	modes := []mode{
		{"watch", func(dir string) []string { return watchArgs(dir, "echo S >> log; exit 3", "--pid", "enma.pid") }},
		{"hotload", func(dir string) []string { return hotloadArgs(dir, "echo S >> log; exit 3", "--pid", "enma.pid") }},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			dir, _, log := project(t)
			p := startEnma(t, dir, nil, m.args(dir)...)
			if !p.exited(10 * time.Second) {
				t.Fatal("enma kept running after start-at-once failed")
			}
			if p.exitCode() == 0 {
				t.Fatal("exit code 0 after start-at-once failed")
			}
			if !strings.Contains(p.out.String(), "start-at-once failed: exit status 3") {
				t.Fatalf("no error message: %s", p.out.String())
			}
			if got := strings.Join(lines(log), ","); got != "S" {
				t.Fatalf("got %s, want only S (no build, daemon or command)", got)
			}
			if strings.Contains(p.out.String(), "👀 Watching") {
				t.Fatal("monitoring started")
			}
			if _, err := os.Stat(filepath.Join(dir, "enma.pid")); err == nil {
				t.Fatal("pid file left behind")
			}
		})
	}
}

func TestE2E_ExecFailure_StopsStartup(t *testing.T) {
	dir, _, log := project(t)
	empty := t.TempDir() // no sh on PATH: the command cannot be run at all
	p := startEnma(t, dir, []string{"PATH=" + empty}, watchArgs(dir, "echo S >> log")...)
	if !p.exited(10 * time.Second) {
		t.Fatal("enma kept running")
	}
	if p.exitCode() == 0 || !strings.Contains(p.out.String(), "start-at-once failed") {
		t.Fatalf("exit %d, output: %s", p.exitCode(), p.out.String())
	}
	if len(lines(log)) != 0 || strings.Contains(p.out.String(), "👀 Watching") {
		t.Fatal("startup continued")
	}
}

func TestE2E_WorkingDir(t *testing.T) {
	dir, _, _ := project(t)
	elsewhere := t.TempDir()
	p := startEnma(t, elsewhere, nil, watchArgs(dir, "pwd -P > wd")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	b, err := os.ReadFile(filepath.Join(dir, "wd"))
	if err != nil || strings.TrimSpace(string(b)) != dir {
		t.Fatalf("ran in %q (%v), want %q", b, err, dir)
	}
	p.interrupt(t)
}

func TestE2E_Placeholder_IsNotExpanded(t *testing.T) {
	dir, src, _ := project(t)
	p := startEnma(t, dir, nil, watchArgs(dir, `printf '%s|%s\n' "{}" "$1" > ph`, "--command", "echo {} > last")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ph")); string(b) != "{}|\n" {
		t.Fatalf("start-at-once saw %q, want the literal placeholder and no $1", b)
	}
	touch(t, src, 1, func() bool { b, _ := os.ReadFile(filepath.Join(dir, "last")); return len(b) > 0 })
	if b, _ := os.ReadFile(filepath.Join(dir, "last")); !strings.Contains(string(b), "a.txt") {
		t.Fatalf("the command's placeholder broke: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ph")); string(b) != "{}|\n" {
		t.Fatal("start-at-once ran again")
	}
	p.interrupt(t)
}

func TestE2E_CtrlC_DuringStartAtOnce(t *testing.T) {
	for _, mode := range []string{"watch", "hotload"} {
		t.Run(mode, func(t *testing.T) {
			dir, _, log := project(t)
			command := "sleep 1000 & echo $! > child; echo $$ > leader; wait"
			args := watchArgs(dir, command)
			if mode == "hotload" {
				args = hotloadArgs(dir, command)
			}
			p := startEnma(t, dir, nil, args...)
			if !waitFor(10*time.Second, func() bool {
				return readPid(filepath.Join(dir, "child")) > 0 && readPid(filepath.Join(dir, "leader")) > 0
			}) {
				t.Fatal("start-at-once did not start")
			}
			child, leader := readPid(filepath.Join(dir, "child")), readPid(filepath.Join(dir, "leader"))
			t.Cleanup(func() { killPid(child); killPid(leader) })
			p.interrupt(t)
			if alive(child) || alive(leader) {
				t.Fatalf("leaked: child alive=%v leader alive=%v", alive(child), alive(leader))
			}
			if len(lines(log)) != 0 || strings.Contains(p.out.String(), "👀 Watching") {
				t.Fatalf("startup continued after Ctrl+C: %v", lines(log))
			}
		})
	}
}

func TestE2E_ChildLeftBehind_IsStopped(t *testing.T) {
	dir, src, log := project(t)
	p := startEnma(t, dir, nil, watchArgs(dir, "sleep 1000 & echo $! > child")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	child := readPid(filepath.Join(dir, "child"))
	t.Cleanup(func() { killPid(child) })
	if child <= 0 || alive(child) {
		t.Fatalf("background child %d outlived start-at-once", child)
	}
	touch(t, src, 1, func() bool { return count(log, "C") == 1 }) // startup went on
	p.interrupt(t)
}

func TestE2E_CLIAndTOML_CLIWinsForSubcommand(t *testing.T) {
	dir, _, log := project(t)
	conf := "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"echo T >> log\"\nstart_at_once = \"echo TOML >> log\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil, watchArgs(dir, "echo CLI >> log")...)
	if !waitFor(10*time.Second, monitoring(p)) {
		t.Fatal("monitoring did not start")
	}
	if got := strings.Join(lines(log), ","); got != "CLI" {
		t.Fatalf("got %s: a subcommand uses its flags only (Enma.toml is not merged)", got)
	}
	p.interrupt(t)
}
