package configure

import (
	"fmt"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/model"
)

type tomlWatchConf struct {
	PreCmd        string `toml:"pre_build"`
	Cmd           string `toml:"build"`
	PostCmd       string `toml:"post_build"`
	Timeout       string `toml:"timeout"`
	Delay         string `toml:"delay"`
	Retry         int    `toml:"retry"`
	WatchDir      string `toml:"watch_dir"`
	PatternRegexp string `toml:"pattern_regex"`
	IncludeExt    string `toml:"include_ext"`
	IgnoreRegex   string `toml:"ignore_regex"`
	ExcludeExt    string `toml:"exclude_ext"`
	ExcludeDir    string `toml:"exclude_dir"`
	EnmaIgnore    string `toml:"enmaignore"`
	LogPath       string `toml:"logs"`
	PidPath       string `toml:"pid"`
}

func NewWatchOptionFromTOMLConfig(h tomlWatchConf) (*option.WatchOption, error) {
	cmd := fallback(h.Cmd, "")
	watchDir := fallback(h.WatchDir, "")
	placeholder := "{}"
	timeout := fallback(h.Timeout, "5sec")
	delay := fallback(h.Delay, "1sec")

	if cmd == "" || watchDir == "" {
		return nil, fmt.Errorf("required fields missing in hotload config")
	}

	opt := &option.WatchOption{
		PreCmd:                 h.PreCmd,
		Cmd:                    cmd,
		PostCmd:                h.PostCmd,
		Placeholder:            placeholder,
		AbsolutePathFlag:       false,
		Timeout:                model.TimeString(timeout),
		Delay:                  model.TimeString(delay),
		Retry:                  h.Retry,
		WatchDir:               watchDir,
		PatternRegexpString:    fallback(h.PatternRegexp, ".*"),
		IncludeExt:             h.IncludeExt,
		IgnoreFileRegexpString: h.IgnoreRegex,
		ExcludeExt:             h.ExcludeExt,
		ExcludeDir:             h.ExcludeDir,
		EnmaIgnoreString:       h.EnmaIgnore,
		LogPathOpt:             h.LogPath,
		PidPathOpt:             h.PidPath,
	}

	if err := opt.Normalize(); err != nil {
		return nil, err
	}

	return opt, nil
}
