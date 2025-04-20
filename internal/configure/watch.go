package configure

import (
	"fmt"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
)

type TomlWatchConf struct {
	PreCmd           string   `toml:"pre_command"`
	Cmd              string   `toml:"command"`
	PostCmd          string   `toml:"post_command"`
	WorkingDir       string   `toml:"working_dir"`
	Placeholder      string   `toml:"placeholder"`
	ArgsPathStyle    string   `toml:"args_path_style"`
	CheckContentDiff *bool    `toml:"check_content_diff"`
	AbsolutePath     *bool    `toml:"absolute_path"`
	Timeout          string   `toml:"timeout"`
	Delay            string   `toml:"delay"`
	Retry            int      `toml:"retry"`
	DefaultIgnore    string   `toml:"default_ignores"`
	WatchDir         []string `toml:"watch_dir"`
	PatternRegexp    string   `toml:"pattern_regex"`
	IncludeExt       []string `toml:"include_ext"`
	IgnoreRegex      string   `toml:"ignore_regex"`
	ExcludeExt       []string `toml:"exclude_ext"`
	ExcludeDir       []string `toml:"exclude_dir"`
	EnmaIgnore       []string `toml:"enmaignore"`
	LogPath          string   `toml:"logs"`
	PidPath          string   `toml:"pid"`
}

func NewWatchOptionFromTOMLConfig(h TomlWatchConf) (*option.WatchOption, error) {
	cmd := Fallback(h.Cmd, "")
	defaultIgnores := Fallback(h.DefaultIgnore, "minimal")
	watchDir := FallbackArray(h.WatchDir, []string{})
	workingDir := Fallback(h.WorkingDir, common.GetCurrentDir())
	placeholder := Fallback(h.Placeholder, "{}")
	argPathStyle := Fallback(h.ArgsPathStyle, "dirname,basename,extension")
	checkContentDiff := FallbackOnOffSwitch(h.CheckContentDiff, true)
	absolutePathFlag := FallbackOnOffSwitch(h.AbsolutePath, true)
	timeout := Fallback(h.Timeout, "5sec")
	delay := Fallback(h.Delay, "1sec")

	if cmd == "" {
		return nil, fmt.Errorf("required fields missing in watch config")
	}

	opt := &option.WatchOption{
		PreCmd:                   h.PreCmd,
		Cmd:                      cmd,
		PostCmd:                  h.PostCmd,
		WorkingDir:               workingDir,
		Placeholder:              placeholder,
		ArgsPathStyleStringValue: argPathStyle,
		CheckContentDiffValue:    checkContentDiff.String(),
		AbsolutePathFlagValue:    absolutePathFlag.String(),
		TimeoutValue:             timeout,
		DelayValue:               delay,
		Retry:                    h.Retry,
		DefaultIgnoresValue:      defaultIgnores,
		WatchDir:                 JoinComma(watchDir),
		PatternRegexpString:      h.PatternRegexp,
		IncludeExt:               JoinComma(h.IncludeExt),
		IgnoreFileRegexpString:   h.IgnoreRegex,
		ExcludeExt:               JoinComma(h.ExcludeExt),
		ExcludeDir:               JoinComma(h.ExcludeDir),
		EnmaIgnoreString:         JoinComma(h.EnmaIgnore),
		LogPathOpt:               h.LogPath,
		PidPathOpt:               h.PidPath,
	}

	if err := opt.Valid(); err != nil {
		return nil, err
	}

	if err := opt.Normalize(); err != nil {
		return nil, err
	}

	return opt, nil
}
