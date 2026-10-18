package subcmd

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/configure"
)

func General(version string, args []string) error {
	opt, err := option.ParseGeneral(args)

	if err != nil {
		return err
	}

	if opt.VersionFlag {
		fmt.Printf("enma version %s\n", version)
		os.Exit(0)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	return RunViaOptionConfigfile(opt)
}

// NoConfigMessage is the error of a run that needs a config file and finds none.
const NoConfigMessage = "can't find an enma config file (Enma.toml, .enma.toml, .enma/enma.toml, .config/enma.toml); " +
	"create one with `enma init` or name one with --config"

// RunViaConfigfile loads, validates and runs a config file. Problems with the
// file itself (unreadable, not TOML, unknown mode, invalid settings) name the
// file; errors of the run that follows are the runner's own.
func RunViaConfigfile(config_file_path string) error {
	cfg, err := LoadConfigfile(config_file_path)
	if err != nil {
		return err
	}
	switch {
	case cfg.Hotload != nil:
		return RunHotload(cfg.Hotload)
	case cfg.Watch != nil:
		return RunWatch(cfg.Watch)
	default:
		return RunScheduler(cfg.Scheduler)
	}
}

// LoadedConfig is a config file read and validated: its mode and the options
// of that mode (exactly one of Hotload, Watch, Scheduler is set).
type LoadedConfig struct {
	Path      string
	Mode      string
	Hotload   *option.HotloadOption
	Watch     *option.WatchOption
	Scheduler *option.SchedulerOption
}

// LoadConfigfile reads and validates a config file the way a run does, without
// running anything: it reads files only (the config, .enmaignore files).
func LoadConfigfile(config_file_path string) (*LoadedConfig, error) {
	data, err := os.ReadFile(config_file_path)
	if err != nil {
		return nil, fmt.Errorf("failed load configfile: %w", err)
	}
	var cfg configure.TomlConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: failed to parse config: %s", config_file_path, describeTOMLError(err))
	}
	inFile := func(err error) error { return inConfigFile(config_file_path, err, true) }
	loaded := &LoadedConfig{Path: config_file_path, Mode: cfg.Subcommand.Use}

	switch cfg.Subcommand.Use {
	case "hotload":
		warnUnknownKeys(config_file_path, data, "hotload", configure.TomlHotloadConf{})
		if loaded.Hotload, err = configure.NewHotloadOptionFromTOMLConfig(cfg.Subcommand.Hotload); err != nil {
			return nil, inFile(err)
		}
	case "watch":
		warnUnknownKeys(config_file_path, data, "watch", configure.TomlWatchConf{})
		if loaded.Watch, err = configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch); err != nil {
			return nil, inFile(err)
		}
	case "scheduler":
		if loaded.Scheduler, err = configure.NewSchedulerOptionFromTOML(data); err != nil {
			return nil, inConfigFile(config_file_path, err, false) // names TOML keys already
		}
	default:
		return nil, fmt.Errorf("%s: unsupported subcommand: %s ([subcommand] use = \"hotload\", \"watch\" or \"scheduler\")", config_file_path, cfg.Subcommand.Use)
	}
	return loaded, nil
}

// describeTOMLError adds the position of a TOML syntax error.
func describeTOMLError(err error) string {
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		return fmt.Sprintf("line %d, column %d: %v", row, col, err)
	}
	return err.Error()
}

// warnUnknownKeys reports keys of [subcommand.<mode>] that enma does not know
// (a typo is otherwise silently ignored). It only warns: hotload and watch
// configs stay as lenient as before.
func warnUnknownKeys(path string, data []byte, mode string, conf any) {
	var raw struct {
		Subcommand map[string]any `toml:"subcommand"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return
	}
	section, _ := raw.Subcommand[mode].(map[string]any)
	var unknown []string
	for k := range section {
		if !knownKey(conf, k) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		fmt.Fprintf(os.Stderr, "⚠️  %s: unknown key %q in [subcommand.%s] (ignored)\n", path, k, mode)
	}
}

// knownKey reports whether the TOML decoder maps key to a field of conf. Like
// go-toml, it ignores case: `Daemon` sets daemon, so it is not unknown.
func knownKey(conf any, key string) bool {
	t := reflect.TypeOf(conf)
	for i := range t.NumField() {
		if tag := strings.Split(t.Field(i).Tag.Get("toml"), ",")[0]; tag != "" && strings.EqualFold(tag, key) {
			return true
		}
	}
	return false
}

// inConfigFile names the config file in every line of a validation error
// (blank lines are dropped). With keys, the error comes from the checks shared
// with the command line (hotload, watch): a line that starts with a flag names
// the TOML key too. Only the start of a line is changed: values the user wrote
// (job names, paths, patterns) are printed as they are.
func inConfigFile(path string, err error, keys bool) error {
	var lines []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if keys {
			l = flagToKey(l)
		}
		lines = append(lines, path+": "+l)
	}
	return errors.New(strings.Join(lines, "\n"))
}

// flagToKey names the TOML key of the flag a line of the shared checks starts
// with ("--timeout ...", "failed to compile pattern-regexp: ...").
func flagToKey(line string) string {
	for _, k := range flagKeys {
		if rest, ok := strings.CutPrefix(line, k.flag); ok && (rest == "" || rest[0] == ' ' || rest[0] == ':') {
			return k.key + rest
		}
	}
	return line
}

var flagKeys = []struct{ flag, key string }{
	{"--signal", "signal (--signal)"}, {"--args-path-style", "args_path_style (--args-path-style)"},
	{"--build-at-start", "build_at_start (--build-at-start)"}, {"--check-content-diff", "check_content_diff (--check-content-diff)"},
	{"--absolute-path", "absolute_path (--absolute-path)"}, {"--timeout", "timeout (--timeout)"}, {"--delay", "delay (--delay)"},
	{"--default-ignores", "default_ignores (--default-ignores)"},
	{"failed to compile pattern-regexp", "failed to compile pattern_regex (--pattern-regex)"},
	{"failed to compile ignore-file-regexp", "failed to compile ignore_regex (--ignore-file-regex)"},
}

func RunViaOptionConfigfile(opt *option.GeneralOption) error {
	if opt.ConfigFilePath == "" {
		return errors.New(NoConfigMessage)
	}
	return RunViaConfigfile(opt.ConfigFilePath)
}
