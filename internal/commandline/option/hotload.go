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

type HotloadOption struct {
	Daemon                 string
	PreBuild               string
	Build                  string
	PostBuild              string
	WorkingDir             string
	Placeholder            string
	ArgsPathStyleString    model.ArgsPathStyleString
	ArgsPathStyle          *model.ArgsPathStyleObj
	AbsolutePathFlag       bool
	Timeout                model.TimeString
	Delay                  model.TimeString
	Retry                  int
	WatchDir               string
	WatchDirList           []string
	PatternRegexpString    string
	PatternRegexp          *regexp.Regexp
	IncludeExt             string
	IncludeExtList         []string
	IgnoreDirRegexpString  string
	IgnoreDirRegexp        *regexp.Regexp
	IgnoreFileRegexpString string
	IgnoreFileRegexp       *regexp.Regexp
	ExcludeExt             string
	ExcludeExtList         []string
	ExcludeDir             string
	ExcludeDirList         []string
	EnmaIgnoreString       string
	EnmaIgnoreList         []string
	EnmaIgnore             *ignorerule.GitIgnore
	LogPathOpt             string
	PidPathOpt             string
	HelpFlag               bool
	FlagSet                *flag.FlagSet
}

func (cr *HotloadOption) Mode() string {
	return "hotload"
}

func ParseHotload(args []string) (*HotloadOption, error) {
	fs := flag.NewFlagSet("hotload", flag.ExitOnError)

	// --daemon
	daemonOpt := fs.String("daemon", "", "Defines the daemon command (required)")
	fs.StringVar(daemonOpt, "d", "", "Defines the daemon command (required)")

	// --pre-build
	preBuildOpt := fs.String("pre-build", "", "Defines the command to pre-build daemon (optional)")
	fs.StringVar(preBuildOpt, "p", "", "Defines the command to pre-build daemon (optional)")

	// --build
	buildOpt := fs.String("build", "", "Defines the command to build daemon (required)")
	fs.StringVar(buildOpt, "b", "", "Defines the command to build daemon (required)")

	// --post-build
	postBuildOpt := fs.String("post-build", "", "Defines the command to post-build daemon (optional)")
	fs.StringVar(postBuildOpt, "P", "", "Defines the command to post-build daemon (optional)")

	// --working-dir
	currentDir := common.GetCurrentDir()
	workingDirOpt := fs.String("working-dir", currentDir, "Defines working directory (optional)")
	fs.StringVar(workingDirOpt, "W", currentDir, "Defines working directory (optional)")

	// --placeholder
	placeholderOpt :=
		fs.String("placeholder", "{}",
			"Defines placeholder that will be replaced with file name where event occurred in command. (optional)")
	fs.StringVar(placeholderOpt, "I", "",
		"Defines placeholder that will be replaced with file name where event occurred in command. (optional)")

	// --args-path-style
	argsPathStyleOpt := model.ArgsPathStyleString("dirname,basename,extension")
	fs.Var(&argsPathStyleOpt, "args-path-style",
		"Defines args path-style string that will be use in file name where event occurred in command. (optional)")
	fs.Var(&argsPathStyleOpt, "s",
		"Defines args path-style string that will be use in file name where event occurred in command. (optional)")

	// --absolute-path
	absolutePathFlagOpt :=
		fs.Bool("absolute-path", false, "File name passed to placeholder must be an absolute path.  (optional)")
	fs.BoolVar(absolutePathFlagOpt, "abs", false, "File name passed to placeholder must be an absolute path.  (optional)")
	fs.BoolVar(absolutePathFlagOpt, "A", false, "File name passed to placeholder must be an absolute path.  (optional)")

	// --timeout
	timeoutOpt := model.TimeString("5sec")
	fs.Var(&timeoutOpt, "timeout", "Specify the build command timeout (optional)")
	fs.Var(&timeoutOpt, "t", "Specify the build command timeout (optional)")

	// --delay
	delayOpt := model.TimeString("0sec")
	fs.Var(&delayOpt, "delay", "Specify delay time after the build command (optional)")
	fs.Var(&delayOpt, "l", "Specify delay time after the build command (optional)")

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
	fs.Parse(args)

	fs.Usage = common.EnmaHotloadHelpFunc

	// Validate required flags
	if *daemonOpt == "" || *buildOpt == "" || *watchDirOpt == "" {
		fmt.Println("Missing required flags for hotload")
		fs.Usage()
		os.Exit(1)
	}

	options := &HotloadOption{
		Daemon:                 *daemonOpt,
		PreBuild:               *preBuildOpt,
		Build:                  *buildOpt,
		PostBuild:              *postBuildOpt,
		WorkingDir:             *workingDirOpt,
		Placeholder:            *placeholderOpt,
		ArgsPathStyleString:    argsPathStyleOpt,
		AbsolutePathFlag:       *absolutePathFlagOpt,
		Timeout:                timeoutOpt,
		Delay:                  delayOpt,
		Retry:                  *retryOpt,
		WatchDir:               *watchDirOpt,
		PatternRegexpString:    *patternRegexOpt,
		IncludeExt:             *includeExtOpt,
		IgnoreFileRegexpString: *ignoreFileRegexOpt,
		IgnoreDirRegexpString:  *ignoreDirRegexOpt,
		ExcludeExt:             *excludeExtOpt,
		ExcludeDir:             *excludeDirOpt,
		EnmaIgnoreString:       *enmaIgnoreOpt,
		PidPathOpt:             *pidPathOpt,
		LogPathOpt:             *logPathOpt,
		HelpFlag:               *helpFlagOpt,
		FlagSet:                fs,
	}

	if err := options.Normalize(); err != nil {
		return nil, err
	}

	return options, nil
}

func (cr *HotloadOption) Normalize() error {
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

	// compile regexp
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
