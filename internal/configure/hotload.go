package configure

import (
	"fmt"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
)

type tomlHotloadConf struct {
	Daemon           string   `toml:"daemon"`
	PreBuild         string   `toml:"pre_build"`
	Build            string   `toml:"build"`
	PostBuild        string   `toml:"post_build"`
	WorkingDir       string   `toml:"working_dir"`
	Placeholder      string   `toml:"placeholder"`
	ArgsPathStyle    string   `toml:"args_path_style"`
	BuildAtStart     *bool    `toml:"build_at_start"`
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

func NewHotloadOptionFromTOMLConfig(h tomlHotloadConf) (*option.HotloadOption, error) {
	daemon := fallback(h.Daemon, "")
	build := fallback(h.Build, "")
	defaultIgnores := fallback(h.DefaultIgnore, "minimal")
	watchDir := fallbackArray(h.WatchDir, []string{"./"})
	workingDir := fallback(h.WorkingDir, common.GetCurrentDir())
	placeholder := fallback(h.Placeholder, "{}")
	argPathStyle := fallback(h.ArgsPathStyle, "dirname,basename,extension")
	checkContentDiff := fallbackOnOffSwitch(h.CheckContentDiff, true)
	absolutePathFlag := fallbackOnOffSwitch(h.AbsolutePath, true)
	buildAtStart := fallbackOnOffSwitch(h.BuildAtStart, true)
	timeout := fallback(h.Timeout, "5sec")
	delay := fallback(h.Delay, "1sec")

	if daemon == "" || build == "" || len(watchDir) == 0 {
		return nil, fmt.Errorf("required fields missing in hotload config")
	}

	opt := &option.HotloadOption{
		Daemon:                   daemon,
		PreBuild:                 h.PreBuild,
		Build:                    build,
		PostBuild:                h.PostBuild,
		ArgsPathStyleStringValue: argPathStyle,
		BuildAtStartValue:        buildAtStart.String(),
		Placeholder:              placeholder,
		AbsolutePathFlagValue:    absolutePathFlag.String(),
		CheckContentDiffValue:    checkContentDiff.String(),
		WorkingDir:               workingDir,
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
