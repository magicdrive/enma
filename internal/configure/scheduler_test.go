package configure_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/configure"
	"github.com/magicdrive/enma/internal/textbank"
)

const schedulerHead = "[subcommand]\nuse = \"scheduler\"\n[subcommand.scheduler]\n"

func job(fields string) string { return "[[subcommand.scheduler.jobs]]\n" + fields + "\n" }

const okJob = "name = \"sync\"\ncron = \"0 */10 * * * *\"\ncommand = \"make sync\""

func loadScheduler(t *testing.T, doc string) (*option.SchedulerOption, error) {
	t.Helper()
	return configure.NewSchedulerOptionFromTOML([]byte(doc))
}

func TestSchedulerTOML_Full(t *testing.T) {
	dir := t.TempDir()
	doc := schedulerHead + "timezone = \"Asia/Tokyo\"\nmax_concurrent = 2\n" +
		job(okJob+"\nworking_dir = \""+dir+"\"\ntimeout = \"30s\"\nretries = 3\nretry_delay = \"10s\"") +
		job("name = \"backup\"\ncron = \"0 0 2 * * *\"\ncommand = \"./backup.sh\"\ntimeout = \"30m\"\nretries = 2\nretry_delay = \"1m\"") +
		job("name = \"cleanup\"\ncron = \"0 0 3 * * *\"\ncommand = \"./cleanup.sh\"\nretries = 0")
	opt, err := loadScheduler(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if opt.Location.String() != "Asia/Tokyo" || opt.MaxConcurrent != 2 || len(opt.Jobs) != 3 {
		t.Fatalf("got %+v", opt)
	}
	j := opt.Jobs[0]
	if j.Name != "sync" || j.CronExpr != "0 */10 * * * *" || j.Command != "make sync" || j.WorkingDir != dir ||
		j.Timeout != 30*time.Second || j.Retries != 3 || j.RetryDelay != 10*time.Second {
		t.Fatalf("job 0: %+v", j)
	}
	if b := opt.Jobs[1]; b.Timeout != 30*time.Minute || b.Retries != 2 || b.RetryDelay != time.Minute {
		t.Fatalf("job 1: %+v", b)
	}
	cwd, _ := os.Getwd()
	if c := opt.Jobs[2]; c.Timeout != 0 || c.Retries != 0 || c.RetryDelay != 10*time.Second || c.WorkingDir != cwd {
		t.Fatalf("job 2 defaults: %+v", c)
	}
}

func TestSchedulerTOML_Defaults(t *testing.T) {
	opt, err := loadScheduler(t, schedulerHead+job(okJob))
	if err != nil {
		t.Fatal(err)
	}
	if opt.Location != time.Local || opt.TimezoneName != "" || opt.MaxConcurrent != 4 {
		t.Fatalf("got %+v", opt)
	}
	if j := opt.Jobs[0]; j.Timeout != 0 || j.Retries != 0 || j.RetryDelay != 10*time.Second {
		t.Fatalf("job defaults: %+v", j)
	}
}

func TestSchedulerTOML_Rejects(t *testing.T) {
	cases := []struct {
		name, doc string
		want      []string // all must appear
	}{
		{"no section", "[subcommand]\nuse = \"scheduler\"\n", []string{"[subcommand.scheduler] is missing"}},
		{"no jobs", schedulerHead + "max_concurrent = 1\n", []string{"jobs: at least one job is required"}},
		{"no name", schedulerHead + job("cron = \"* * * * * *\"\ncommand = \"true\""), []string{"scheduler job #1: name: is required"}},
		{"empty name", schedulerHead + job("name = \"\"\ncron = \"* * * * * *\"\ncommand = \"true\""), []string{"scheduler job #1: name: must not be empty"}},
		{"control char in name", schedulerHead + job("name = \"a\\nb\"\ncron = \"* * * * * *\"\ncommand = \"true\""), []string{"name:", "printable"}},
		{"duplicate name", schedulerHead + job(okJob) + job(okJob), []string{`scheduler job "sync" (#2): name: duplicate of job #1`}},
		{"no cron", schedulerHead + job("name = \"a\"\ncommand = \"true\""), []string{`"a" (#1): cron: is required`}},
		{"5-field cron", schedulerHead + job("name = \"a\"\ncron = \"* * * * *\"\ncommand = \"true\""), []string{`"a" (#1): cron:`, "want 6 fields"}},
		{"quartz cron", schedulerHead + job("name = \"a\"\ncron = \"0 0 0 ? * MON\"\ncommand = \"true\""), []string{`cron:`}},
		{"no command", schedulerHead + job("name = \"a\"\ncron = \"* * * * * *\""), []string{`"a" (#1): command: is required`}},
		{"blank command", schedulerHead + job("name = \"a\"\ncron = \"* * * * * *\"\ncommand = \"  \""), []string{`command: must not be empty`}},
		{"bad timezone", schedulerHead + "timezone = \"Mars/Base\"\n" + job(okJob), []string{`timezone: unknown time zone "Mars/Base"`}},
		{"empty timezone", schedulerHead + "timezone = \"\"\n" + job(okJob), []string{`timezone: must not be empty`}},
		{"bad duration", schedulerHead + job(okJob+"\ntimeout = \"soon\""), []string{`"sync" (#1): timeout: "soon": not a duration`}},
		{"negative timeout", schedulerHead + job(okJob+"\ntimeout = \"-5s\""), []string{`timeout: "-5s": must not be negative`}},
		{"zero timeout", schedulerHead + job(okJob+"\ntimeout = \"0s\""), []string{`timeout: "0s": must be positive (omit it for no limit)`}},
		{"negative retries", schedulerHead + job(okJob+"\nretries = -1"), []string{`retries: must not be negative, got -1`}},
		{"zero retry_delay with retries", schedulerHead + job(okJob+"\nretries = 1\nretry_delay = \"0s\""), []string{`retry_delay: must be positive when retries is set`}},
		{"negative retry_delay", schedulerHead + job(okJob+"\nretry_delay = \"-1s\""), []string{`retry_delay: "-1s": must not be negative`}},
		{"zero max_concurrent", schedulerHead + "max_concurrent = 0\n" + job(okJob), []string{`max_concurrent: must be 1 or more, got 0`}},
		{"negative max_concurrent", schedulerHead + "max_concurrent = -2\n" + job(okJob), []string{`max_concurrent: must be 1 or more, got -2`}},
		{"missing working_dir", schedulerHead + job(okJob+"\nworking_dir = \"./no-such-dir\""), []string{`working_dir: "./no-such-dir"`}},
		{"empty working_dir", schedulerHead + job(okJob+"\nworking_dir = \"\""), []string{`working_dir: must not be empty`}},
		{"unknown scheduler key", schedulerHead + "max_concurrency = 2\n" + job(okJob), []string{"scheduler: max_concurrency: unknown key"}},
		{"unknown job key", schedulerHead + job(okJob+"\nretry = 2"), []string{"unknown key", "retry"}},
		{"wrong type", schedulerHead + job(okJob+"\nretries = \"3\""), []string{`scheduler job "sync" (#1): retries: want an integer, got string "3"`}},
		{"wrong type second job", schedulerHead + job(okJob) + job("name = \"b\"\ncron = \"* * * * * *\"\ncommand = \"x\"\ntimeout = 30"), []string{`scheduler job "b" (#2): timeout: want a string, got integer 30`}},
		{"wrong scheduler type", schedulerHead + "timezone = 5\n" + job(okJob), []string{`scheduler: timezone: want a string, got integer 5`}},
		{"jobs not tables", schedulerHead + "jobs = [1, 2]\n", []string{`scheduler job #1: want a table, got integer 1`}},
		{"jobs not an array", schedulerHead + "jobs = \"x\"\n", []string{`scheduler: jobs: want an array of tables`}},
		{"unknown key names the job", schedulerHead + job(okJob+"\nretry = 2"), []string{`scheduler job "sync" (#1): retry: unknown key`}},
		{"single job shorthand", schedulerHead + okJob + "\n", []string{"unknown key"}},
		{"several problems at once", schedulerHead + "max_concurrent = 0\n" + job("name = \"a\"\ncron = \"bad\"\ncommand = \"\"") + job("cron = \"* * * * * *\"\ncommand = \"x\""),
			[]string{"max_concurrent", `"a" (#1): cron:`, `"a" (#1): command:`, "job #2: name: is required"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadScheduler(t, tc.doc)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

// The scheduler's strict keys do not touch the other modes' sections.
func TestSchedulerTOML_OtherSectionsUntouched(t *testing.T) {
	doc := "name = \"my-app\"\n" + schedulerHead + job(okJob) +
		"[subcommand.watch]\ncommand = \"true\"\nunknown_watch_key = 1\n"
	if _, err := loadScheduler(t, doc); err != nil {
		t.Fatal(err)
	}
	var cfg configure.TomlConfig
	if err := toml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch); err != nil {
		t.Fatalf("watch section: %v", err)
	}
}

// --- config template -------------------------------------------------------

// The generated scheduler config parses, loads and validates as is.
func TestSchedulerTemplate_RoundTrip(t *testing.T) {
	tmpl := textbank.DefaultSchedulerEnmaToml
	var cfg configure.TomlConfig
	if err := toml.Unmarshal([]byte(tmpl), &cfg); err != nil {
		t.Fatalf("template is not valid TOML: %v", err)
	}
	if cfg.Subcommand.Use != "scheduler" {
		t.Fatalf("use = %q", cfg.Subcommand.Use)
	}
	conf, err := configure.DecodeSchedulerTOML([]byte(tmpl)) // strict: no unknown key
	if err != nil {
		t.Fatal(err)
	}
	opt, err := configure.NewSchedulerOptionFromTOMLConfig(*conf)
	if err != nil {
		t.Fatalf("template does not validate: %v", err)
	}
	if len(opt.Jobs) < 1 || opt.Jobs[0].Name == "" || opt.Jobs[0].Cron == nil {
		t.Fatalf("template jobs: %+v", opt.Jobs)
	}
	// the values written in the template are the runtime defaults
	if opt.MaxConcurrent != option.SchedulerDefaultMaxConcurrent || opt.Location != time.Local {
		t.Fatalf("scheduler defaults differ: %+v", opt)
	}
	j := opt.Jobs[0]
	if j.Retries != option.SchedulerDefaultRetries || j.RetryDelay != option.SchedulerDefaultRetryDelay || j.Timeout != 0 {
		t.Fatalf("job defaults differ: %+v", j)
	}
	// the documented default (comments) matches too
	for _, want := range []string{"Default: 4", "Default: 0", `Default: "10s"`, "Default: no limit", "Default: the OS local time zone", "Default: the directory enma was started in"} {
		if !strings.Contains(tmpl, want) {
			t.Errorf("template does not say %q", want)
		}
	}
	// the sample command is harmless
	for _, bad := range []string{"rm ", "mv ", "> /", "sudo", "curl", "wget"} {
		if strings.Contains(j.Command, bad) {
			t.Fatalf("sample command %q is not harmless", j.Command)
		}
	}
}

// Every key the template mentions (also in comments) is a real key, and
// uncommenting the example lines still gives a valid config with two jobs.
func TestSchedulerTemplate_CommentedKeysAndSecondJob(t *testing.T) {
	tmpl := textbank.DefaultSchedulerEnmaToml
	known := map[string]bool{"use": true, "timezone": true, "max_concurrent": true, "logs": true, "pid": true,
		"name": true, "cron": true, "command": true, "working_dir": true, "timeout": true, "retries": true, "retry_delay": true}
	keyRe := regexp.MustCompile(`(?m)^[#\s]*([a-z_]+)\s*=`)
	for _, m := range keyRe.FindAllStringSubmatch(tmpl, -1) {
		if !known[m[1]] {
			t.Errorf("template mentions unknown key %q", m[1])
		}
	}
	uncommented := regexp.MustCompile(`(?m)^## `).ReplaceAllString(tmpl, "")
	dir := t.TempDir()
	uncommented = strings.ReplaceAll(uncommented, `working_dir = "./"`, `working_dir = "`+dir+`"`)
	opt, err := configure.NewSchedulerOptionFromTOML([]byte(uncommented))
	if err != nil {
		t.Fatalf("template with its examples enabled: %v\n%s", err, uncommented)
	}
	if len(opt.Jobs) != 2 || opt.Jobs[1].Name == opt.Jobs[0].Name || opt.TimezoneName != "UTC" {
		t.Fatalf("examples enabled: %+v", opt)
	}
	if opt.Jobs[0].Timeout != 30*time.Second || opt.Jobs[1].Retries != 2 {
		t.Fatalf("examples enabled: %+v", opt.Jobs)
	}
}

// The init templates of the other modes are unchanged in meaning.
func TestTemplates_OtherModesStillLoad(t *testing.T) {
	var cfg configure.TomlConfig
	if err := toml.Unmarshal([]byte(textbank.DefaultHotloadEnmaToml), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := configure.NewHotloadOptionFromTOMLConfig(cfg.Subcommand.Hotload); err != nil {
		t.Fatalf("hotload template: %v", err)
	}
	cfg = configure.TomlConfig{}
	if err := toml.Unmarshal([]byte(textbank.DefaultWatchEnmaToml), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch); err != nil {
		t.Fatalf("watch template: %v", err)
	}
}

// FuzzSchedulerTOML: arbitrary documents never panic the loader, and an
// accepted one has jobs with unique names and valid settings.
func FuzzSchedulerTOML(f *testing.F) {
	f.Add(textbank.DefaultSchedulerEnmaToml)
	f.Add(schedulerHead + job(okJob) + job("name = \"b\"\ncron = \"0 0 0 29 2 *\"\ncommand = \"x\"\nretries = 2\nretry_delay = \"1m\""))
	f.Add(schedulerHead + "max_concurrent = 0\n" + job("cron = \"bad\""))
	f.Fuzz(func(t *testing.T, doc string) {
		opt, err := configure.NewSchedulerOptionFromTOML([]byte(doc))
		if err != nil {
			return
		}
		if len(opt.Jobs) == 0 || opt.MaxConcurrent < 1 || opt.Location == nil {
			t.Fatalf("accepted %+v", opt)
		}
		seen := map[string]bool{}
		for _, j := range opt.Jobs {
			if j.Name == "" || seen[j.Name] || j.Cron == nil || strings.TrimSpace(j.Command) == "" ||
				j.Timeout < 0 || j.Retries < 0 || (j.Retries > 0 && j.RetryDelay <= 0) || !filepath.IsAbs(j.WorkingDir) {
				t.Fatalf("accepted job %+v", j)
			}
			seen[j.Name] = true
		}
	})
}
