package option

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // --timezone works without system zone data (Windows, minimal images)
	"unicode"

	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/cronspec"
	"github.com/magicdrive/enma/internal/model"
)

// Scheduler defaults. The config template (textbank/config_toml/scheduler.enma.toml)
// documents the same values; a test keeps them in step.
const (
	SchedulerDefaultJobName       = "default"
	SchedulerDefaultRetries       = 0
	SchedulerDefaultRetryDelay    = 10 * time.Second
	SchedulerDefaultMaxConcurrent = 4
)

// SchedulerJob is one validated job.
type SchedulerJob struct {
	Name       string
	CronExpr   string
	Cron       *cronspec.Schedule
	Command    string
	WorkingDir string        // absolute
	Timeout    time.Duration // 0: no limit
	Retries    int
	RetryDelay time.Duration
}

// SchedulerOption is a validated scheduler configuration: CLI (one job) or TOML (one or more).
type SchedulerOption struct {
	Jobs          []SchedulerJob
	TimezoneName  string // "" when not given (OS local time)
	Location      *time.Location
	MaxConcurrent int
	LogPathOpt    string
	PidPathOpt    string
	HelpFlag      bool
	FlagSet       *flag.FlagSet
}

func (cr *SchedulerOption) Mode() string { return "scheduler" }

// SchedulerJobInput is one job as given; nil means "not given" (the default applies).
type SchedulerJobInput struct {
	Name, Cron, Command, WorkingDir, Timeout, RetryDelay *string
	Retries                                              *int
}

// SchedulerInput is a scheduler configuration as given.
type SchedulerInput struct {
	Jobs          []SchedulerJobInput
	Timezone      *string
	MaxConcurrent *int
	LogPath       string
	PidPath       string
}

// FieldNames names the fields in error messages: TOML keys or CLI flags.
type FieldNames map[string]string

var (
	TOMLSchedulerFields = FieldNames{"name": "name", "cron": "cron", "command": "command", "working_dir": "working_dir",
		"timeout": "timeout", "retries": "retries", "retry_delay": "retry_delay", "timezone": "timezone",
		"max_concurrent": "max_concurrent", "jobs": "jobs"}
	CLISchedulerFields = FieldNames{"name": "--name", "cron": "--cron", "command": "--command", "working_dir": "--working-dir",
		"timeout": "--timeout", "retries": "--retries", "retry_delay": "--retry-delay", "timezone": "--timezone",
		"max_concurrent": "--max-concurrent", "jobs": "jobs"}
)

// NewSchedulerOption validates every job and the scheduler settings and
// reports all problems at once. Relative working directories are resolved
// against the current directory.
func NewSchedulerOption(in SchedulerInput, names FieldNames) (*SchedulerOption, error) {
	var errs []string
	opt := &SchedulerOption{MaxConcurrent: SchedulerDefaultMaxConcurrent, Location: time.Local,
		LogPathOpt: in.LogPath, PidPathOpt: in.PidPath}

	if in.Timezone != nil {
		opt.TimezoneName = *in.Timezone
		switch {
		case strings.TrimSpace(*in.Timezone) == "":
			errs = append(errs, fmt.Sprintf("scheduler: %s: must not be empty (omit it for the OS local time zone)", names["timezone"]))
		default:
			loc, err := time.LoadLocation(*in.Timezone)
			if err != nil {
				errs = append(errs, fmt.Sprintf("scheduler: %s: unknown time zone %q", names["timezone"], *in.Timezone))
			} else {
				opt.Location = loc
			}
		}
	}
	if in.MaxConcurrent != nil {
		if *in.MaxConcurrent < 1 {
			errs = append(errs, fmt.Sprintf("scheduler: %s: must be 1 or more, got %d", names["max_concurrent"], *in.MaxConcurrent))
		} else {
			opt.MaxConcurrent = *in.MaxConcurrent
		}
	}
	if len(in.Jobs) == 0 {
		errs = append(errs, fmt.Sprintf("scheduler: %s: at least one job is required", names["jobs"]))
	}
	seen := map[string]int{}
	for i, j := range in.Jobs {
		job, jerrs := newSchedulerJob(j, names)
		label := fmt.Sprintf("scheduler job #%d", i+1)
		if j.Name != nil && *j.Name != "" {
			label = fmt.Sprintf("scheduler job %q (#%d)", *j.Name, i+1)
		}
		for _, e := range jerrs {
			errs = append(errs, label+": "+e)
		}
		if job.Name != "" {
			if first, dup := seen[job.Name]; dup {
				errs = append(errs, fmt.Sprintf("%s: %s: duplicate of job #%d", label, names["name"], first))
			} else {
				seen[job.Name] = i + 1
			}
		}
		opt.Jobs = append(opt.Jobs, job)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return opt, nil
}

func newSchedulerJob(in SchedulerJobInput, names FieldNames) (SchedulerJob, []string) {
	var errs []string
	add := func(field, format string, a ...any) {
		errs = append(errs, names[field]+": "+fmt.Sprintf(format, a...))
	}
	job := SchedulerJob{Retries: SchedulerDefaultRetries, RetryDelay: SchedulerDefaultRetryDelay}

	switch {
	case in.Name == nil:
		add("name", "is required")
	case strings.TrimSpace(*in.Name) == "":
		add("name", "must not be empty")
	case !validJobName(*in.Name):
		add("name", "%q: use printable characters without leading or trailing spaces", *in.Name)
	default:
		job.Name = *in.Name
	}

	switch {
	case in.Cron == nil:
		add("cron", "is required")
	default:
		s, err := cronspec.Parse(*in.Cron)
		if err != nil {
			add("cron", "%v", err)
		} else {
			job.CronExpr, job.Cron = *in.Cron, s
		}
	}

	switch {
	case in.Command == nil:
		add("command", "is required")
	case strings.TrimSpace(*in.Command) == "":
		add("command", "must not be empty")
	default:
		job.Command = *in.Command
	}

	wd := common.GetCurrentDir()
	if in.WorkingDir != nil {
		if *in.WorkingDir == "" {
			add("working_dir", "must not be empty (omit it for the current directory)")
		}
		wd = *in.WorkingDir
	}
	if abs, err := filepath.Abs(wd); err != nil {
		add("working_dir", "%q: %v", wd, err)
	} else if st, err := os.Stat(abs); err != nil {
		add("working_dir", "%q: %v", wd, err)
	} else if !st.IsDir() {
		add("working_dir", "%q: not a directory", wd)
	} else {
		job.WorkingDir = abs
	}

	if in.Timeout != nil {
		d, err := positiveDuration(*in.Timeout)
		if err != nil {
			add("timeout", "%v (omit it for no limit)", err)
		} else {
			job.Timeout = d
		}
	}
	if in.Retries != nil {
		if *in.Retries < 0 {
			add("retries", "must not be negative, got %d", *in.Retries)
		} else {
			job.Retries = *in.Retries
		}
	}
	if in.RetryDelay != nil {
		d, err := duration(*in.RetryDelay)
		switch {
		case err != nil:
			add("retry_delay", "%v", err)
		case d <= 0 && job.Retries > 0:
			add("retry_delay", "must be positive when retries is set, got %q", *in.RetryDelay)
		default:
			job.RetryDelay = d
		}
	}
	return job, errs
}

func validJobName(name string) bool {
	if name != strings.TrimSpace(name) || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// duration parses an enma time string (e.g. "10s", "30sec", "1m", "2h", "1d").
func duration(s string) (time.Duration, error) {
	if strings.HasPrefix(strings.TrimSpace(s), "-") {
		return 0, fmt.Errorf("%q: must not be negative", s)
	}
	var ts model.TimeString
	if err := ts.Set(s); err != nil {
		return 0, fmt.Errorf("%q: not a duration (e.g. 30s, 10m, 1h)", s)
	}
	d, err := ts.TimeDuration()
	if err != nil {
		return 0, fmt.Errorf("%q: %v", s, err)
	}
	return d, nil
}

func positiveDuration(s string) (time.Duration, error) {
	d, err := duration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q: must be positive", s)
	}
	return d, nil
}

// ParseScheduler parses `enma scheduler` flags: exactly one job.
func ParseScheduler(args []string) (*SchedulerOption, error) {
	fs := flag.NewFlagSet("scheduler", flag.ExitOnError)
	cronOpt := fs.String("cron", "", "6-field cron expression: second minute hour day-of-month month day-of-week (required)")
	commandOpt := fs.String("command", "", "Command to run (required)")
	nameOpt := fs.String("name", SchedulerDefaultJobName, "Job name (optional)")
	workingDirOpt := fs.String("working-dir", "", "Working directory of the command (optional. default: $PWD)")
	timezoneOpt := fs.String("timezone", "", "Time zone of the cron expression (optional. default: OS local time)")
	timeoutOpt := fs.String("timeout", "", "Time limit of one attempt (optional. default: none)")
	retriesOpt := fs.Int("retries", SchedulerDefaultRetries, "Retries after a failed attempt (optional)")
	retryDelayOpt := fs.String("retry-delay", "10s", "Wait between a failed attempt and its retry (optional)")
	maxConcurrentOpt := fs.Int("max-concurrent", SchedulerDefaultMaxConcurrent, "Maximum attempts running at once (optional)")
	pidPathOpt := fs.String("pid", "", "Specify pid file path (optional)")
	logPathOpt := fs.String("logs", "", "Specify log file path (optional)")
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = common.EnmaHelpFunc
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	fs.Usage = common.EnmaSchedulerHelpFunc
	if *helpFlagOpt {
		fs.Usage()
		os.Exit(0)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("scheduler: unexpected argument %q (the CLI defines one job; use a config file for more)", fs.Arg(0))
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	str := func(name string, v *string) *string {
		if set[name] {
			return v
		}
		return nil
	}
	job := SchedulerJobInput{
		Name:       nameOpt, // always given: the default name is "default"
		Cron:       str("cron", cronOpt),
		Command:    str("command", commandOpt),
		WorkingDir: str("working-dir", workingDirOpt),
		Timeout:    str("timeout", timeoutOpt),
		RetryDelay: str("retry-delay", retryDelayOpt),
	}
	if set["retries"] {
		job.Retries = retriesOpt
	}
	in := SchedulerInput{Jobs: []SchedulerJobInput{job}, Timezone: str("timezone", timezoneOpt), LogPath: *logPathOpt, PidPath: *pidPathOpt}
	if set["max-concurrent"] {
		in.MaxConcurrent = maxConcurrentOpt
	}
	opt, err := NewSchedulerOption(in, CLISchedulerFields)
	if err != nil {
		return nil, err
	}
	opt.HelpFlag, opt.FlagSet = *helpFlagOpt, fs
	return opt, nil
}
