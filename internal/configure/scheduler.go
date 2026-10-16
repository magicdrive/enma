package configure

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/commandline/option"
)

// TomlSchedulerConf is [subcommand.scheduler]. Pointers tell a key that is
// left out (default) from one given an invalid value.
type TomlSchedulerConf struct {
	Timezone      *string            `toml:"timezone"`
	MaxConcurrent *int               `toml:"max_concurrent"`
	LogPath       string             `toml:"logs"`
	PidPath       string             `toml:"pid"`
	Jobs          []TomlSchedulerJob `toml:"jobs"`
}

// TomlSchedulerJob is one [[subcommand.scheduler.jobs]] table.
type TomlSchedulerJob struct {
	Name       *string `toml:"name"`
	Cron       *string `toml:"cron"`
	Command    *string `toml:"command"`
	WorkingDir *string `toml:"working_dir"`
	Timeout    *string `toml:"timeout"`
	Retries    *int    `toml:"retries"`
	RetryDelay *string `toml:"retry_delay"`
}

// DecodeSchedulerTOML decodes [subcommand.scheduler] of a config file strictly:
// an unknown key or a value of the wrong type under it is an error that names
// the job and the key. The rest of the file (other modes' sections) is not
// looked at, so they keep their lenient decoding.
func DecodeSchedulerTOML(data []byte) (*TomlSchedulerConf, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	sub, _ := raw["subcommand"].(map[string]any)
	section, ok := sub["scheduler"]
	if !ok {
		return nil, errors.New("scheduler config: [subcommand.scheduler] is missing")
	}
	table, ok := section.(map[string]any)
	if !ok {
		return nil, errors.New("scheduler config: subcommand.scheduler must be a table")
	}
	var conf TomlSchedulerConf
	var errs []string
	for _, k := range sortedKeys(table) {
		v := table[k]
		bad := func(format string, a ...any) {
			errs = append(errs, "scheduler: "+k+": "+fmt.Sprintf(format, a...))
		}
		switch k {
		case "timezone":
			conf.Timezone = stringValue(v, bad)
		case "max_concurrent":
			conf.MaxConcurrent = intValue(v, bad)
		case "logs":
			if s := stringValue(v, bad); s != nil {
				conf.LogPath = *s
			}
		case "pid":
			if s := stringValue(v, bad); s != nil {
				conf.PidPath = *s
			}
		case "jobs":
			items, ok := v.([]any)
			if !ok {
				bad("want an array of tables ([[subcommand.scheduler.jobs]]), got %s", describe(v))
				continue
			}
			for n, item := range items {
				job, jerrs := decodeSchedulerJob(item, n+1)
				errs = append(errs, jerrs...)
				conf.Jobs = append(conf.Jobs, job)
			}
		default:
			bad("unknown key")
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("scheduler config:\n%s", strings.Join(errs, "\n"))
	}
	return &conf, nil
}

func decodeSchedulerJob(item any, n int) (TomlSchedulerJob, []string) {
	var job TomlSchedulerJob
	m, ok := item.(map[string]any)
	if !ok {
		return job, []string{fmt.Sprintf("scheduler job #%d: want a table, got %s", n, describe(item))}
	}
	label := fmt.Sprintf("scheduler job #%d", n)
	if name, ok := m["name"].(string); ok && name != "" {
		label = fmt.Sprintf("scheduler job %q (#%d)", name, n)
	}
	var errs []string
	for _, k := range sortedKeys(m) {
		v := m[k]
		bad := func(format string, a ...any) {
			errs = append(errs, label+": "+k+": "+fmt.Sprintf(format, a...))
		}
		switch k {
		case "name":
			job.Name = stringValue(v, bad)
		case "cron":
			job.Cron = stringValue(v, bad)
		case "command":
			job.Command = stringValue(v, bad)
		case "working_dir":
			job.WorkingDir = stringValue(v, bad)
		case "timeout":
			job.Timeout = stringValue(v, bad)
		case "retry_delay":
			job.RetryDelay = stringValue(v, bad)
		case "retries":
			job.Retries = intValue(v, bad)
		default:
			bad("unknown key")
		}
	}
	return job, errs
}

func stringValue(v any, bad func(string, ...any)) *string {
	s, ok := v.(string)
	if !ok {
		bad("want a string, got %s", describe(v))
		return nil
	}
	return &s
}

func intValue(v any, bad func(string, ...any)) *int {
	i, ok := v.(int64)
	if !ok {
		bad("want an integer, got %s", describe(v))
		return nil
	}
	if int64(int(i)) != i {
		bad("%d is out of range", i)
		return nil
	}
	n := int(i)
	return &n
}

func describe(v any) string {
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("string %q", x)
	case int64:
		return fmt.Sprintf("integer %d", x)
	case float64:
		return fmt.Sprintf("float %v", x)
	case bool:
		return fmt.Sprintf("boolean %v", x)
	case []any:
		return "array"
	case map[string]any:
		return "table"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// NewSchedulerOptionFromTOMLConfig validates a decoded scheduler section.
func NewSchedulerOptionFromTOMLConfig(c TomlSchedulerConf) (*option.SchedulerOption, error) {
	in := option.SchedulerInput{Timezone: c.Timezone, MaxConcurrent: c.MaxConcurrent, LogPath: c.LogPath, PidPath: c.PidPath}
	for _, j := range c.Jobs {
		in.Jobs = append(in.Jobs, option.SchedulerJobInput{Name: j.Name, Cron: j.Cron, Command: j.Command,
			WorkingDir: j.WorkingDir, Timeout: j.Timeout, Retries: j.Retries, RetryDelay: j.RetryDelay})
	}
	return option.NewSchedulerOption(in, option.TOMLSchedulerFields)
}

// NewSchedulerOptionFromTOML decodes and validates the scheduler section of a config file.
func NewSchedulerOptionFromTOML(data []byte) (*option.SchedulerOption, error) {
	conf, err := DecodeSchedulerTOML(data)
	if err != nil {
		return nil, err
	}
	return NewSchedulerOptionFromTOMLConfig(*conf)
}
