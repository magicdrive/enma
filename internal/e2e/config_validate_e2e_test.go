//go:build !windows

package e2e_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// enma config validate / enma scheduler --next (H5):
//   V1 a valid config: exit 0, "OK" on stdout, nothing on stderr;
//   V2 nothing happens: no command runs (start-at-once, build, jobs), no pid or
//      log file and no directory is created, no file changes;
//   V3 every problem is reported, each line naming the file; exit 1;
//   V4 what a run fails on at start (missing working or watch directory) is an error;
//   V5 the templates of `enma init` are valid and use only known keys;
//   V6 --next lists the next fire times of each job, with the time zone; it is bounded.

// tree lists the files and directories below dir with their size and mtime.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprint(p, " ", info.Mode(), " ", info.ModTime(), " ", info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func writeConf(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestE2E_ConfigValidate_ValidConfigsChangeNothing(t *testing.T) {
	dir := isolated(t, "")
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "ran")
	outside := isolated(t, "") // pid and log paths outside the project (audit H5B-09)
	confs := map[string]string{
		"outside.toml": `[subcommand]
use = "watch"
[subcommand.watch]
command = "touch '` + marker + `'"
logs = "` + filepath.Join(outside, "log", "enma.log") + `"
pid = "` + filepath.Join(outside, "run", "enma.pid") + `"
`,
		"hotload.toml": `[subcommand]
use = "hotload"
[subcommand.hotload]
daemon = "touch '` + marker + `'"
build = "touch '` + marker + `'"
start_at_once = "touch '` + marker + `'"
watch_dir = ["src"]
logs = "out/log/enma.log"
pid = "out/run/enma.pid"
`,
		"watch.toml": `[subcommand]
use = "watch"
[subcommand.watch]
command = "touch '` + marker + `'"
start_at_once = "touch '` + marker + `'"
logs = "out/log/enma.log"
pid = "out/run/enma.pid"
`,
		"scheduler.toml": `[subcommand]
use = "scheduler"
[subcommand.scheduler]
timezone = "UTC"
logs = "out/log/enma.log"
pid = "out/run/enma.pid"
[[subcommand.scheduler.jobs]]
name = "every-second"
cron = "* * * * * *"
command = "touch '` + marker + `'"
`,
	}
	for name, body := range confs {
		writeConf(t, dir, name, body)
	}
	time.Sleep(20 * time.Millisecond) // mtimes of later changes differ
	before, beforeOutside := tree(t, dir), tree(t, outside)
	for name := range confs {
		t.Run(name, func(t *testing.T) {
			r := runEnma(t, dir, "config", "validate", "-c", name)
			if r.code != 0 || r.stderr != "" {
				t.Fatalf("V1: exit %d, stderr %q", r.code, r.stderr)
			}
			if !strings.HasPrefix(r.stdout, name+": OK (") {
				t.Fatalf("V1: stdout %q", r.stdout)
			}
		})
	}
	time.Sleep(1500 * time.Millisecond) // an every-second job or a daemon would have run by now
	if after := tree(t, dir); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("V2: validate changed the directory:\nbefore %v\nafter  %v", before, after)
	}
	if after := tree(t, outside); strings.Join(after, "\n") != strings.Join(beforeOutside, "\n") {
		t.Fatalf("V2: validate wrote outside the project:\nbefore %v\nafter  %v", beforeOutside, after)
	}
}

func TestE2E_ConfigValidate_FindsTheDefaultConfig(t *testing.T) {
	dir := isolated(t, "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\n")
	r := runEnma(t, dir, "config", "validate")
	if r.code != 0 || r.stdout != "Enma.toml: OK (watch config)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	r = runEnma(t, dir, "config", "validate", "--next", "3")
	if r.code != 0 || r.stdout != "Enma.toml: OK (watch config)\n" || !strings.Contains(r.stderr, "--next applies to scheduler configs only") {
		t.Fatalf("--next on a watch config: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	empty := isolated(t, "")
	r = runEnma(t, empty, "config", "validate")
	if r.code != 1 || !strings.Contains(r.stderr, "can't find an enma config file") || r.stdout != "" {
		t.Fatalf("no config: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestE2E_ConfigValidate_ReportsEveryProblem(t *testing.T) {
	dir := isolated(t, "")
	cases := []struct {
		name, body string
		want       []string
	}{
		{"values.toml", `[subcommand]
use = "hotload"
[subcommand.hotload]
daemon = "x"
build = "y"
signal = "SIGNOPE"
timeout = "soon"
delay = "later"
`, []string{"values.toml: signal (--signal)", "values.toml: timeout (--timeout)", "values.toml: delay (--delay)"}},
		{"dirs.toml", `[subcommand]
use = "watch"
[subcommand.watch]
command = "true"
watch_dir = ["gone", "also-gone"]
`, []string{"dirs.toml: watch_dir gone: does not exist", "dirs.toml: watch_dir also-gone: does not exist"}},
		{"workdir.toml", `[subcommand]
use = "watch"
[subcommand.watch]
command = "true"
working_dir = "/nonexistent-enma-dir"
watch_dir = ["rel", "/nonexistent-enma-abs"]
`, []string{"workdir.toml: working_dir /nonexistent-enma-dir: not an existing directory",
			"workdir.toml: watch_dir /nonexistent-enma-abs: does not exist"}},
		{"regex.toml", `[subcommand]
use = "hotload"
[subcommand.hotload]
daemon = "x"
build = "y"
pattern_regex = "("
ignore_regex = "["
`, []string{"regex.toml: failed to compile pattern_regex (--pattern-regex)", "regex.toml: failed to compile ignore_regex (--ignore-file-regex)"}},
		{"names.toml", `[subcommand]
use = "scheduler"
[subcommand.scheduler]
[[subcommand.scheduler.jobs]]
name = "x --delay"
cron = "bad"
command = "true"
`, []string{`names.toml: scheduler job "x --delay" (#1): cron:`}}, // values are not rewritten (H5R2-01)
		{"jobs.toml", `[subcommand]
use = "scheduler"
[subcommand.scheduler]
[[subcommand.scheduler.jobs]]
name = "a"
cron = "bad"
command = "true"
[[subcommand.scheduler.jobs]]
name = "b"
cron = "* * * * * *"
`, []string{`jobs.toml: scheduler job "a" (#1): cron:`, `jobs.toml: scheduler job "b" (#2)`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeConf(t, dir, tc.name, tc.body)
			r := runEnma(t, dir, "config", "validate", "--config", tc.name)
			if r.code != 1 || r.stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
			for _, w := range tc.want {
				if !strings.Contains(r.stderr, w) {
					t.Errorf("V3: stderr lacks %q:\n%s", w, r.stderr)
				}
			}
		})
	}
}

// What validate rejects, a run rejects too: validate is not stricter.
func TestE2E_ConfigValidate_StartProblemsFailARunToo(t *testing.T) {
	dir := isolated(t, "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nwatch_dir = [\"gone\"]\n")
	if r := runEnma(t, dir, "config", "validate"); r.code != 1 {
		t.Fatalf("validate: exit %d", r.code)
	}
	if r := runEnma(t, dir); r.code != 1 || !strings.Contains(r.stderr, "gone") {
		t.Fatalf("run: exit %d, stderr %q", r.code, r.stderr)
	}
}

// H5R3-01: a directory that exists but cannot be read is reported with the
// cause a run reports ("permission denied"), not as missing; a working_dir
// that is a file is not a directory. (root reads any directory: the
// permission cases are left out then.)
func TestE2E_ConfigValidate_StatErrorNamesTheCause(t *testing.T) {
	dir := isolated(t, "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nwatch_dir = [\"locked/x\"]\n")
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	writeConf(t, dir, "wd.toml", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nworking_dir = \"locked/x\"\n")
	writeConf(t, dir, "file.toml", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nworking_dir = \"wd.toml\"\n")

	cases := []struct{ file, want, run string }{
		{"file.toml", "working_dir wd.toml: not a directory", "not a directory"},
	}
	if os.Geteuid() != 0 {
		cases = append(cases,
			struct{ file, want, run string }{"Enma.toml", "watch_dir locked/x: permission denied", "permission denied"},
			struct{ file, want, run string }{"wd.toml", "working_dir locked/x: permission denied", "permission denied"})
	}
	for _, c := range cases {
		v := runEnma(t, dir, "config", "validate", "-c", c.file)
		if v.code != 1 || !strings.Contains(v.stderr, c.want) {
			t.Errorf("validate %s: exit %d, stderr %q, want %q", c.file, v.code, v.stderr, c.want)
		}
		if r := runEnma(t, dir, "-c", c.file); r.code != 1 || !strings.Contains(r.stderr, c.run) {
			t.Errorf("run %s: exit %d, stderr %q", c.file, r.code, r.stderr)
		}
	}
}

func TestE2E_ConfigValidate_InitTemplatesAreValid(t *testing.T) {
	for _, mode := range []string{"hotload", "watch", "scheduler"} {
		t.Run(mode, func(t *testing.T) {
			dir := isolated(t, "")
			for _, m := range []string{mode, "enmaignore"} { // the .enmaignore template is read by validate too
				if r := runEnma(t, dir, "init", "--mode", m); r.code != 0 {
					t.Fatalf("init --mode %s: exit %d, stderr %q", m, r.code, r.stderr)
				}
			}
			args := []string{"config", "validate"}
			if mode == "scheduler" {
				args = append(args, "--next", "2") // the template's jobs have fire times
			}
			r := runEnma(t, dir, args...)
			if r.code != 0 || r.stderr != "" {
				t.Fatalf("V5: the %s template: exit %d, stderr %q", mode, r.code, r.stderr)
			}
			if !strings.Contains(r.stdout, "Enma.toml: OK ("+mode+" config") {
				t.Fatalf("V5: stdout %q", r.stdout)
			}
		})
	}
}

func TestE2E_ConfigValidate_Next(t *testing.T) {
	dir := isolated(t, `[subcommand]
use = "scheduler"
[subcommand.scheduler]
timezone = "Asia/Tokyo"
[[subcommand.scheduler.jobs]]
name = "nightly"
cron = "0 30 2 * * *"
command = "true"
[[subcommand.scheduler.jobs]]
name = "minutely"
cron = "0 * * * * *"
command = "true"
`)
	r := runEnma(t, dir, "config", "validate", "--next", "3")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	out := r.stdout
	for _, s := range []string{"timezone Asia/Tokyo", `job "nightly" (cron "0 30 2 * * *")`, `job "minutely"`} {
		if !strings.Contains(out, s) {
			t.Fatalf("V6: missing %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "Enma.toml: OK (scheduler config, 2 job(s), timezone Asia/Tokyo)\n") {
		t.Fatalf("V6: OK line:\n%s", out)
	}
	nightly := out[strings.Index(out, `job "nightly"`):strings.Index(out, `job "minutely"`)] // the minutely job may list 02:30:00 too
	if n := strings.Count(nightly, " 02:30:00 +0900 JST"); n != 3 || strings.Count(nightly, " +0900 JST") != 3 {
		t.Fatalf("V6: nightly fire times, want 3 at 02:30:00:\n%s", out)
	}
	if strings.Index(out, `job "nightly"`) > strings.Index(out, `job "minutely"`) {
		t.Fatalf("V6: jobs not in config order:\n%s", out)
	}
	if r := runEnma(t, dir, "config", "validate", "--next", "100"); r.code != 0 || strings.Count(r.stdout, " +0900 JST\n") != 200 {
		t.Fatalf("V6: --next 100 (the limit): exit %d, stderr %q", r.code, r.stderr)
	}
	for _, bad := range []string{"-1", "101"} {
		if r := runEnma(t, dir, "config", "validate", "--next", bad); r.code != 1 || !strings.Contains(r.stderr, "must be between 0 and 100") {
			t.Fatalf("V6: --next %s: exit %d, stderr %q", bad, r.code, r.stderr)
		}
	}
}

func TestE2E_SchedulerNext_ListsAndExits(t *testing.T) {
	dir := isolated(t, "")
	marker := filepath.Join(dir, "ran")
	before := tree(t, dir)
	r := runEnma(t, dir, "scheduler", "--cron", "* * * * * *", "--command", "touch '"+marker+"'", "--timezone", "UTC",
		"--next", "5", "--pid", "run/enma.pid", "--logs", "log/enma.log")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if n := strings.Count(r.stdout, " +0000 UTC\n"); n != 5 || !strings.Contains(r.stdout, "timezone UTC") {
		t.Fatalf("V6: stdout:\n%s", r.stdout)
	}
	time.Sleep(1500 * time.Millisecond)
	if after := tree(t, dir); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("V2: --next changed the directory:\nbefore %v\nafter  %v", before, after)
	}
}

func TestE2E_Config_Usage(t *testing.T) {
	dir := isolated(t, "")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"config"}, 1, "missing action"},
		{[]string{"config", "check"}, 1, `unknown action "check"`},
		{[]string{"config", "validate", "extra"}, 1, `unexpected argument "extra"`},
		{[]string{"config", "validate", "--bogus"}, 2, "enma config validate --help"},
	} {
		r := runEnma(t, dir, tc.args...)
		if r.code != tc.code || !strings.Contains(r.stderr, tc.want) || r.stdout != "" {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", tc.args, r.code, r.stdout, r.stderr)
		}
	}
	// a missing --config file: the same message as a run (audit H5R2-04)
	v, run := runEnma(t, dir, "config", "validate", "-c", "nope.toml"), runEnma(t, dir, "-c", "nope.toml")
	if v.code != 1 || run.code != 1 || v.stderr != run.stderr || !strings.Contains(v.stderr, "failed load configfile nope.toml: no such file") {
		t.Errorf("missing file: validate %d %q, run %d %q", v.code, v.stderr, run.code, run.stderr)
	}
	r := runEnma(t, dir, "config", "validate", "--help")
	if r.code != 0 || !strings.Contains(r.stdout, "Usage: enma config validate") {
		t.Errorf("--help: exit %d, stdout %q", r.code, r.stdout)
	}
}

// A relative watch_dir is found below working_dir, as a run looks for it there.
func TestE2E_ConfigValidate_WatchDirBelowWorkingDir(t *testing.T) {
	dir := isolated(t, "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nworking_dir = \"app\"\nwatch_dir = [\"src\"]\n")
	if err := os.MkdirAll(filepath.Join(dir, "app", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := runEnma(t, dir, "config", "validate"); r.code != 0 {
		t.Fatalf("V4: app/src exists: exit %d, stderr %q", r.code, r.stderr)
	}
	abs := isolated(t, "")
	writeConf(t, dir, "abs.toml", "[subcommand]\nuse = \"watch\"\n[subcommand.watch]\ncommand = \"true\"\nworking_dir = \"app\"\nwatch_dir = [\""+abs+"\"]\n")
	if r := runEnma(t, dir, "config", "validate", "-c", "abs.toml"); r.code != 0 {
		t.Fatalf("V4: an absolute watch_dir is not below working_dir: exit %d, stderr %q", r.code, r.stderr)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil { // ./src exists, app/src does not
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "app", "src")); err != nil {
		t.Fatal(err)
	}
	if r := runEnma(t, dir, "config", "validate"); r.code != 1 || !strings.Contains(r.stderr, "watch_dir src: does not exist") {
		t.Fatalf("V4: only ./src exists: exit %d, stderr %q", r.code, r.stderr)
	}
}
