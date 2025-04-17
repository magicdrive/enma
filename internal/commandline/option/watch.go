package option

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/ignorerule"
	"github.com/magicdrive/enma/internal/model"
)

type WatchOption struct {
	PreCmd                   string
	Cmd                      string
	PostCmd                  string
	WorkingDir               string
	Placeholder              string
	ArgsPathStyleString      model.ArgsPathStyleString
	ArgsPathStyleStringValue string
	ArgsPathStyle            *model.ArgsPathStyleObj
	CheckContentDiff         model.OnOffSwitch
	CheckContentDiffValue    string
	AbsolutePathFlag         model.OnOffSwitch
	AbsolutePathFlagValue    string
	Timeout                  model.TimeString
	TimeoutValue             string
	Delay                    model.TimeString
	DelayValue               string
	Retry                    int
	WatchDir                 string
	WatchDirList             []string
	PatternRegexpString      string
	PatternRegexp            *regexp.Regexp
	IncludeExt               string
	IncludeExtList           []string
	IgnoreDirRegexpString    string
	IgnoreDirRegexp          *regexp.Regexp
	IgnoreFileRegexpString   string
	IgnoreFileRegexp         *regexp.Regexp
	ExcludeExt               string
	ExcludeExtList           []string
	ExcludeDir               string
	ExcludeDirList           []string
	EnmaIgnoreString         string
	EnmaIgnoreList           []string
	EnmaIgnore               *ignorerule.GitIgnore
	LogPathOpt               string
	PidPathOpt               string
	HelpFlag                 bool
	FlagSet                  *flag.FlagSet
}

func (cr *WatchOption) Mode() string {
	return "hotload"
}

func ParseWatch(args []string) (*WatchOption, error) {
	fs := flag.NewFlagSet("hotload", flag.ExitOnError)

	// --pre-command
	preCmdOpt := fs.String("pre-command", "", "Defines the command to pre-command (optional)")
	fs.StringVar(preCmdOpt, "pre-cmd", "", "Defines the command to pre-command (optional)")
	fs.StringVar(preCmdOpt, "p", "", "Defines the command to pre-command (optional)")

	// --command
	cmdOpt := fs.String("command", "", "Defines the daemon command (required)")
	fs.StringVar(cmdOpt, "cmd", "", "Defines the action command (required)")
	fs.StringVar(cmdOpt, "c", "", "Defines the action command (required)")

	// --post-cmd
	postCmdOpt := fs.String("post-command", "", "Defines the command to post-command (optional)")
	fs.StringVar(postCmdOpt, "post-cmd", "", "Defines the command to post-command (optional)")
	fs.StringVar(postCmdOpt, "P", "", "Defines the command to post-command (optional)")

	// --working-dir
	currentDir := common.GetCurrentDir()
	workingDirOpt := fs.String("working-dir", currentDir, "Defines working directory (optional)")
	fs.StringVar(workingDirOpt, "W", currentDir, "Defines working directory (optional)")

	// --placeholder
	placeholderOpt :=
		fs.String("placeholder", "{}",
			"Defines placeholder that will be replaced with file name where event occurred in command. (optional)")
	fs.StringVar(placeholderOpt, "I", "{}",
		"Defines placeholder that will be replaced with file name where event occurred in command. (optional)")

	// --args-path-style
	argsPathStyleOpt := fs.String("args-path-style", "dirname,basename,extension",
		"Defines args path-style string that will be use in file name where event occurred in command. (optional)")
	fs.StringVar(argsPathStyleOpt, "s", "dirname,basename,extension",
		"Defines args path-style string that will be use in file name where event occurred in command. (optional)")

	// --check-content-diff
	checkContentDiffFlagOpt :=
		fs.String("check-content-diff", "on", "Fires only when the file contents are changed.  (optional)")

	// --absolute-path
	absolutePathFlagOpt :=
		fs.String("absolute-path", "on", "File name passed to placeholder must be an absolute path.  (optional)")
	fs.StringVar(absolutePathFlagOpt, "abs", "on", "File name passed to placeholder must be an absolute path.  (optional)")
	fs.StringVar(absolutePathFlagOpt, "A", "on", "File name passed to placeholder must be an absolute path.  (optional)")

	// --timeout
	timeoutOpt := fs.String("timeout", "5sec", "Specify the build command timeout (optional)")
	fs.StringVar(timeoutOpt, "t", "5sec", "Specify the build command timeout (optional)")

	// --delay
	delayOpt := fs.String("delay", "0sec", "Specify delay time after the build command (optional)")
	fs.StringVar(delayOpt, "l", "0sec", "Specify delay time after the build command (optional)")

	// --retry
	retryOpt := fs.Int("retry", 0, "Specify retry count (optional)")
	fs.IntVar(retryOpt, "r", 0, "Specify retry count (optional)")

	// --watch-dir
	watchDirOpt := fs.String("watch-dir", "", "Specify watch directories (required)")
	fs.StringVar(watchDirOpt, "w", "", "Specify watch directories (required)")

	// --pattern-regex
	patternRegexOpt := fs.String("pattern-regex", ".*", "Specify watch file pattern regexp (optional)")
	fs.StringVar(patternRegexOpt, "x", ".*", "Specify watch file pattern regexp (optional)")

	// --include-ext
	includeExtOpt := fs.String("include-ext", "", "Specify watch file extension (optional)")
	fs.StringVar(includeExtOpt, "i", "", "Specify watch file extension (optional)")

	// --ignore-file-regex
	ignoreFileRegexOpt := fs.String("ignore-file-regex", "", "Specify watch file ignore pattern regexp (optional)")
	fs.StringVar(ignoreFileRegexOpt, "g", "", "Specify watch file ignore pattern regexp (optional)")

	// --ignore-dir-regex
	ignoreDirRegexOpt := fs.String("ignore-dir-regex", "", "Specify watch dir ignore pattern regexp (optional)")
	fs.StringVar(ignoreDirRegexOpt, "G", "", "Specify watch file ignore pattern regexp (optional)")

	// --exclude-ext
	excludeExtOpt := fs.String("exclude-ext", "", "Specify watch exclude file extension (optional)")
	fs.StringVar(excludeExtOpt, "e", "", "Specify watch exclude file extension (optional)")

	// --exclude-dir
	excludeDirOpt := fs.String("exclude-dir", "", "Specify watch exclude directory (optional)")
	fs.StringVar(excludeDirOpt, "E", "", "Specify watch exclude directory (optional)")

	// --enmaignore
	enmaIgnoreOpt := fs.String("enmaignore", "", "Defines the enmaignore paths allowed commma sepalated.(optional)")
	fs.StringVar(enmaIgnoreOpt, "n", "", "Defines the enmaignore paths allowed commma sepalated.(optional)")

	// --pid
	pidPathOpt := fs.String("pid", "", "Specify pid file path (optional)")

	// --logs
	logPathOpt := fs.String("logs", "", "Specify log file path (optional)")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = common.EnmaHelpFunc

	// Parse flags
	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	fs.Usage = common.EnmaWatchHelpFunc

	// Individual help
	if *helpFlagOpt {
		fs.Usage()
		os.Exit(0)
	}
	// Validate required flags
	if *cmdOpt == "" || *watchDirOpt == "" {
		fmt.Println("Missing required flags for watch")
		fs.Usage()
		os.Exit(1)
	}

	options := &WatchOption{
		PreCmd:                   *preCmdOpt,
		Cmd:                      *cmdOpt,
		PostCmd:                  *postCmdOpt,
		WorkingDir:               *workingDirOpt,
		Placeholder:              *placeholderOpt,
		ArgsPathStyleStringValue: *argsPathStyleOpt,
		CheckContentDiffValue:    *checkContentDiffFlagOpt,
		AbsolutePathFlagValue:    *absolutePathFlagOpt,
		TimeoutValue:             *timeoutOpt,
		DelayValue:               *delayOpt,
		Retry:                    *retryOpt,
		WatchDir:                 *watchDirOpt,
		PatternRegexpString:      *patternRegexOpt,
		IncludeExt:               *includeExtOpt,
		IgnoreFileRegexpString:   *ignoreFileRegexOpt,
		IgnoreDirRegexpString:    *ignoreDirRegexOpt,
		ExcludeExt:               *excludeExtOpt,
		ExcludeDir:               *excludeDirOpt,
		EnmaIgnoreString:         *enmaIgnoreOpt,
		PidPathOpt:               *pidPathOpt,
		LogPathOpt:               *logPathOpt,
		HelpFlag:                 *helpFlagOpt,
		FlagSet:                  fs,
	}

	if err := options.Valid(); err != nil {
		return nil, err
	}

	if err := options.Normalize(); err != nil {
		return nil, err
	}

	return options, nil
}

func (cr *WatchOption) Valid() error {
	var errorMessages = []string{}

	if err := cr.ArgsPathStyleString.Set(cr.ArgsPathStyleStringValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--args-path-style %s", err.Error()))
	}

	if err := cr.CheckContentDiff.Set(cr.CheckContentDiffValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--check-content-diff %s", err.Error()))
	}

	if err := cr.AbsolutePathFlag.Set(cr.AbsolutePathFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--absolute-path %s", err.Error()))
	}
	if err := cr.Timeout.Set(cr.TimeoutValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--timeout %s", err.Error()))
	}

	if err := cr.Delay.Set(cr.DelayValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--delay %s", err.Error()))
	}

	if len(errorMessages) == 0 {
		return nil
	} else {
		return errors.New(strings.Join(errorMessages, "\n"))
	}
}

func (cr *WatchOption) Normalize() error {
	var errorMessages = []string{}

	// args path style
	if cr.ArgsPathStyleString != "" {
		obj, err := cr.ArgsPathStyleString.ArgsPathStyleObj()
		if err != nil {
			errorMessages = append(errorMessages, err.Error())
		} else {
			cr.ArgsPathStyle = obj
		}
	}

	// comma sepalated to list.
	if cr.WatchDir != "" {
		cr.WatchDirList = common.CommaSeparated2StringList(cr.WatchDir)
	}
	if cr.IncludeExt != "" {
		cr.IncludeExtList = common.CommaSeparated2StringList(cr.IncludeExt)
	}
	if cr.ExcludeExt != "" {
		cr.ExcludeExtList = common.CommaSeparated2StringList(cr.ExcludeExt)
	}
	if cr.ExcludeDir != "" {
		cr.ExcludeDirList = common.CommaSeparated2StringList(cr.ExcludeDir)
	}
	if cr.EnmaIgnoreString != "" {
		cr.EnmaIgnoreList = common.CommaSeparated2StringList(cr.EnmaIgnoreString)

		// enmaignore
		if enmaIgnore, err := ignorerule.NewGitignore(cr.WorkingDir, cr.EnmaIgnoreList); err != nil {
			e := fmt.Errorf("ennmaignore load error: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.EnmaIgnore = enmaIgnore
		}
	}

	if cr.PatternRegexpString != "" {
		re, err := regexp.Compile(cr.PatternRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile pattern-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.PatternRegexp = re
		}
	}

	if cr.IgnoreFileRegexpString != "" {
		re, err := regexp.Compile(cr.IgnoreFileRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile ignore-file-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.IgnoreFileRegexp = re
		}
	}

	if cr.IgnoreDirRegexpString != "" {
		re, err := regexp.Compile(cr.IgnoreDirRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile ignore-dir-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.IgnoreDirRegexp = re
		}
	}

	if len(errorMessages) == 0 {
		return nil
	} else {
		return errors.New(strings.Join(errorMessages, "\n"))
	}
}
