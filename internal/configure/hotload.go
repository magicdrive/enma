package configure

import (
	"fmt"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/model"
)

type tomlHotloadConf struct {
	Daemon        string   `toml:"daemon"`
	PreBuild      string   `toml:"pre_build"`
	Build         string   `toml:"build"`
	PostBuild     string   `toml:"post_build"`
	WorkingDir    string   `toml:"working_dir"`
	Placeholder   string   `toml:"placeholder"`
	Timeout       string   `toml:"timeout"`
	Delay         string   `toml:"delay"`
	Retry         int      `toml:"retry"`
	WatchDir      []string `toml:"watch_dir"`
	PatternRegexp string   `toml:"pattern_regex"`
	IncludeExt    []string `toml:"include_ext"`
	IgnoreRegex   string   `toml:"ignore_regex"`
	ExcludeExt    []string `toml:"exclude_ext"`
	ExcludeDir    []string `toml:"exclude_dir"`
	EnmaIgnore    []string `toml:"enmaignore"`
	LogPath       string   `toml:"logs"`
	PidPath       string   `toml:"pid"`
}

func NewHotloadOptionFromTOMLConfig(h tomlHotloadConf) (*option.HotloadOption, error) {
	daemon := fallback(h.Daemon, "")
	build := fallback(h.Build, "")
	watchDir := fallbackArray(h.WatchDir, []string{})
	workingDir := fallback(h.WorkingDir, common.GetCurrentDir())
	placeholder := fallback(h.Placeholder, "{}")
	timeout := fallback(h.Timeout, "5sec")
	delay := fallback(h.Delay, "1sec")

	if daemon == "" || build == "" || len(watchDir) == 0 {
		return nil, fmt.Errorf("required fields missing in hotload config")
	}

	opt := &option.HotloadOption{
		Daemon:                 daemon,
		PreBuild:               h.PreBuild,
		Build:                  build,
		PostBuild:              h.PostBuild,
		Placeholder:            placeholder,
		WorkingDir:             workingDir,
		AbsolutePathFlag:       false,
		Timeout:                model.TimeString(timeout),
		Delay:                  model.TimeString(delay),
		Retry:                  h.Retry,
		WatchDir:               JoinComma(watchDir),
		PatternRegexpString:    fallback(h.PatternRegexp, ".*"),
		IncludeExt:             JoinComma(h.IncludeExt),
		IgnoreFileRegexpString: h.IgnoreRegex,
		ExcludeExt:             JoinComma(h.ExcludeExt),
		ExcludeDir:             JoinComma(h.ExcludeDir),
		EnmaIgnoreString:       JoinComma(h.EnmaIgnore),
		LogPathOpt:             h.LogPath,
		PidPathOpt:             h.PidPath,
	}

	if err := opt.Normalize(); err != nil {
		return nil, err
	}

	return opt, nil
}
