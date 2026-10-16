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

	"github.com/magicdrive/enma/internal/textbank"
)

// schedDir is an isolated directory whose pid files (written by jobs) are
// killed at cleanup: jobs run in their own process groups.
func schedDir(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Cleanup(func() {
		b, _ := os.ReadFile(filepath.Join(dir, "pids"))
		for _, s := range strings.Fields(string(b)) {
			n, _ := strconv.Atoi(s)
			killPid(n) // ignores pid <= 0
		}
	})
	return dir
}

func countIn(s, sub string) int { return strings.Count(s, sub) }

func pidsIn(dir string) []int {
	var out []int
	b, _ := os.ReadFile(filepath.Join(dir, "pids"))
	for _, s := range strings.Fields(string(b)) {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func TestE2E_Scheduler_CLIJobRunsAndStops(t *testing.T) {
	dir := schedDir(t)
	p := startEnma(t, dir, nil, "scheduler", "--cron", "* * * * * *", "--command", "echo run >> log", "--name", "tick",
		"--working-dir", dir, "--pid", "enma.pid", "--logs", "enma.log")
	if !waitFor(10*time.Second, func() bool { return count(filepath.Join(dir, "log"), "run") >= 2 }) {
		t.Fatalf("the job did not run twice:\n%s", p.out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "enma.pid")); err != nil {
		t.Fatal("no pid file")
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit %d after SIGINT", p.exitCode())
	}
	out := p.out.String()
	for _, want := range []string{"event=scheduler_start", `event=job_start job="tick" attempt=1`, `event=job_success job="tick"`, "event=scheduler_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(out, "echo run") {
		t.Error("the command text was logged")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "enma.log")); !strings.Contains(string(b), "event=job_success") {
		t.Error("--logs file lacks the events")
	}
	if _, err := os.Stat(filepath.Join(dir, "enma.pid")); err == nil {
		t.Error("pid file left behind")
	}
}

func TestE2E_Scheduler_ConfigErrorsExit1(t *testing.T) {
	dir := isolated(t, "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\nmax_concurrent = 0\n[[subcommand.scheduler.jobs]]\nname = \"a\"\ncron = \"* * * * *\"\ncommand = \"true\"\nbogus = 1\n")
	r := runEnma(t, dir)
	if r.code != 1 || !strings.Contains(r.stderr, "unknown key") {
		t.Fatalf("unknown key: exit %d, stderr %q", r.code, r.stderr)
	}
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte("[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\nmax_concurrent = 0\n[[subcommand.scheduler.jobs]]\nname = \"a\"\ncron = \"* * * * *\"\ncommand = \"true\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = runEnma(t, dir)
	if r.code != 1 || !strings.Contains(r.stderr, "max_concurrent: must be 1 or more") || !strings.Contains(r.stderr, `scheduler job "a" (#1): cron:`) {
		t.Fatalf("validation: exit %d, stderr %q", r.code, r.stderr)
	}
	for _, args := range [][]string{
		{"scheduler", "--command", "true"},
		{"scheduler", "--cron", "0 * * * *", "--command", "true"},
		{"scheduler", "--cron", "* * * * * *", "--command", "true", "--timezone", "Nowhere/City"},
		{"scheduler", "--cron", "* * * * * *", "--command", "true", "--max-concurrent", "0"},
	} {
		if r := runEnma(t, dir, args...); r.code != 1 || r.stderr == "" || r.stdout != "" {
			t.Errorf("enma %q: exit %d, stdout %q, stderr %q", args, r.code, r.stdout, r.stderr)
		}
	}
	if r := runEnma(t, dir, "scheduler", "--help"); r.code != 0 || !strings.Contains(r.stdout, "--max-concurrent") {
		t.Errorf("scheduler --help: exit %d, stdout %q", r.code, r.stdout)
	}
	if r := runEnma(t, dir, "init", "--help"); r.code != 0 || !strings.Contains(r.stdout, "hotload|watch|scheduler|enmaignore") {
		t.Errorf("init --help: exit %d, stdout %q", r.code, r.stdout)
	}
	if r := runEnma(t, dir, "--help"); r.code != 0 || !strings.Contains(r.stdout, "scheduler options") {
		t.Errorf("--help: exit %d", r.code)
	}
}

func TestE2E_Scheduler_InitTemplateRuns(t *testing.T) {
	dir := isolated(t, "")
	r := runEnma(t, dir, "init", "--mode", "scheduler")
	if r.code != 0 {
		t.Fatalf("init: exit %d, stderr %q", r.code, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Enma.toml"))
	if err != nil || string(b) != textbank.DefaultSchedulerEnmaToml {
		t.Fatalf("generated file differs from the template (%v)", err)
	}
	// existing file: refused, not overwritten (as for the other modes)
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := runEnma(t, dir, "init", "--mode", "scheduler"); r.code != 1 || !strings.Contains(r.stderr, "already exists") {
		t.Fatalf("init over an existing file: exit %d, stderr %q", r.code, r.stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Enma.toml")); string(b) != "mine" {
		t.Fatal("existing file overwritten")
	}
	// another file name: generated and runnable as is
	if r := runEnma(t, dir, "init", "-m", "scheduler", "-f", "sched.toml"); r.code != 0 {
		t.Fatalf("init -f: exit %d", r.code)
	}
	p := startEnma(t, dir, nil, "--config", "sched.toml")
	if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), "event=scheduler_start") }) {
		t.Fatalf("generated config did not start:\n%s", p.out.String())
	}
	if !strings.Contains(p.out.String(), "jobs=1 max_concurrent=4") {
		t.Fatalf("unexpected start line:\n%s", p.out.String())
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit %d", p.exitCode())
	}
	// hotload / watch templates are still generated
	for _, mode := range []string{"hotload", "watch"} {
		f := mode + ".toml"
		if r := runEnma(t, dir, "init", "-m", mode, "-f", f); r.code != 0 {
			t.Fatalf("init %s: exit %d", mode, r.code)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, f)); !strings.Contains(string(b), `use = "`+mode+`"`) {
			t.Fatalf("init %s: %q", mode, b)
		}
	}
}

func TestE2E_Scheduler_TimeoutAndRetriesWithRealProcesses(t *testing.T) {
	dir := schedDir(t)
	cmd := "echo try >> log; sleep 1000 & echo $! >> pids; echo $$ >> pids; wait"
	conf := "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\n[[subcommand.scheduler.jobs]]\nname = \"slow\"\ncron = \"* * * * * *\"\n" +
		"command = \"" + strings.ReplaceAll(cmd, `"`, `\"`) + "\"\ntimeout = \"500ms\"\nretries = 1\nretry_delay = \"1s\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil)
	if !waitFor(15*time.Second, func() bool { return strings.Contains(p.out.String(), "event=retries_exhausted") }) {
		t.Fatalf("no retries_exhausted:\n%s", p.out.String())
	}
	out := p.out.String()
	if countIn(out, "event=job_timeout") < 2 || !strings.Contains(out, "event=retry_start") || !strings.Contains(out, "event=retry_scheduled") {
		t.Fatalf("timeline:\n%s", out)
	}
	// the two timed-out attempts wrote the first four pids: their groups were killed
	timedOut := pidsIn(dir)
	if len(timedOut) < 4 {
		t.Fatalf("pids %v", timedOut)
	}
	for _, pid := range timedOut[:4] {
		if alive(pid) {
			t.Fatalf("%d outlived its timed-out attempt", pid)
		}
	}
	p.interrupt(t)
	if p.exitCode() != 0 {
		t.Fatalf("exit %d", p.exitCode())
	}
	for _, pid := range pidsIn(dir) {
		if alive(pid) {
			t.Fatalf("process %d left behind after stop", pid)
		}
	}
}

func TestE2E_Scheduler_ParallelStopIsBounded(t *testing.T) {
	dir := schedDir(t)
	// three jobs that ignore SIGTERM: each needs the 5s grace and SIGKILL
	job := func(name string) string {
		return "[[subcommand.scheduler.jobs]]\nname = \"" + name + "\"\ncron = \"* * * * * *\"\n" +
			"command = \"trap '' TERM; echo $$ >> pids; sleep 1000 & echo $! >> pids; while :; do sleep 0.1; done\"\n"
	}
	conf := "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\nmax_concurrent = 3\n" + job("a") + job("b") + job("c")
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil)
	if !waitFor(10*time.Second, func() bool { return len(pidsIn(dir)) == 6 }) {
		t.Fatalf("jobs did not start:\n%s", p.out.String())
	}
	began := time.Now()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if !p.exited(20 * time.Second) {
		t.Fatal("enma did not exit")
	}
	if d := time.Since(began); d > 9*time.Second {
		t.Fatalf("stop took %v: the jobs were stopped one after another", d)
	}
	if p.exitCode() != 0 {
		t.Fatalf("exit %d", p.exitCode())
	}
	for _, pid := range pidsIn(dir) {
		if alive(pid) {
			t.Fatalf("process %d left behind", pid)
		}
	}
	if n := countIn(p.out.String(), "event=job_canceled"); n != 3 {
		t.Fatalf("want 3 job_canceled, got %d:\n%s", n, p.out.String())
	}
}

func TestE2E_Scheduler_MaxConcurrentAndBusySkips(t *testing.T) {
	dir := schedDir(t)
	job := func(name string) string {
		return "[[subcommand.scheduler.jobs]]\nname = \"" + name + "\"\ncron = \"* * * * * *\"\n" +
			"command = \"echo start >> log; sleep 1.5; echo end >> log\"\n"
	}
	conf := "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\nmax_concurrent = 1\n" + job("a") + job("b")
	if err := os.WriteFile(filepath.Join(dir, "Enma.toml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startEnma(t, dir, nil)
	if !waitFor(15*time.Second, func() bool { return count(filepath.Join(dir, "log"), "end") >= 3 }) {
		t.Fatalf("jobs did not run:\n%s", p.out.String())
	}
	p.interrupt(t)
	// with max_concurrent = 1 the attempts never overlap: start/end alternate
	l := lines(filepath.Join(dir, "log"))
	for i := 1; i < len(l); i++ {
		if l[i] == l[i-1] {
			t.Fatalf("attempts overlapped: %v", l)
		}
	}
	out := p.out.String()
	if !strings.Contains(out, "reason=capacity_unavailable") || !strings.Contains(out, "reason=job_busy") {
		t.Fatalf("expected both skip reasons:\n%s", out)
	}
}
