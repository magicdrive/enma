package subcmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/core"
)

// Config runs `enma config <action>`; the only action is validate.
func Config(args []string) error {
	if len(args) == 0 {
		return errors.New("config: missing action (use: enma config validate)\nRun 'enma config validate --help' for usage")
	}
	switch args[0] {
	case "validate":
		opt, err := option.ParseConfigValidate(args[1:])
		if err != nil {
			return err
		}
		return ConfigValidate(os.Stdout, opt, time.Now())
	case "-h", "--help", "help":
		common.EnmaConfigHelpFunc()
		return nil
	default:
		return fmt.Errorf("config: unknown action %q (use: enma config validate)\nRun 'enma config validate --help' for usage", args[0])
	}
}

// ConfigValidate loads a config file like a run does, checks what the run
// would fail on at start (directories that do not exist), and reports the
// result on w. Problems are reported per check, as a run reports them: all the
// problems one check finds together; a later check runs once the earlier
// ones pass. Nothing is started and nothing is written: no pid file, no log
// file, no change of directory. For scheduler configs, opt.Next fire times
// after now are listed per job.
func ConfigValidate(w io.Writer, opt *option.ConfigValidateOption, now time.Time) error {
	path := opt.ConfigFilePath
	if path == "" {
		found, err := common.FindEnmaConfigFile() // the lookup a plain `enma` does
		if err != nil {
			return errors.New(NoConfigMessage)
		}
		path = found
	}
	if ok, err := common.FileExists(path); err == nil && !ok { // the wording of a run (enma -c)
		return fmt.Errorf("failed load configfile %s: no such file", path)
	}
	cfg, err := LoadConfigfile(path)
	if err != nil {
		return err
	}
	if problems := startProblems(cfg); len(problems) > 0 {
		return inConfigFile(path, errors.New(strings.Join(problems, "\n")), false)
	}
	switch {
	case cfg.Scheduler != nil:
		loc := cfg.Scheduler.Location
		if loc == nil {
			loc = time.Local
		}
		fmt.Fprintf(w, "%s: OK (scheduler config, %d job(s), timezone %s)\n", path, len(cfg.Scheduler.Jobs), core.ZoneLabel(loc, now))
		if opt.Next > 0 {
			core.WritePreview(w, cfg.Scheduler, now, opt.Next)
		}
	default:
		fmt.Fprintf(w, "%s: OK (%s config)\n", path, cfg.Mode)
		if opt.Next > 0 {
			fmt.Fprintf(os.Stderr, "--next applies to scheduler configs only (%s is a %s config)\n", path, cfg.Mode)
		}
	}
	return nil
}

// startProblems lists what a hotload / watch run would fail on at start: a
// working directory or a watch directory that does not exist. (Scheduler job
// directories are checked when the config is loaded.)
func startProblems(cfg *LoadedConfig) []string {
	var workingDir string
	var watchDirs []string
	switch {
	case cfg.Hotload != nil:
		workingDir, watchDirs = cfg.Hotload.WorkingDir, cfg.Hotload.WatchDirList
	case cfg.Watch != nil:
		workingDir, watchDirs = cfg.Watch.WorkingDir, cfg.Watch.WatchDirList
	default:
		return nil
	}
	var problems []string
	workingDirOK := true
	if workingDir != "" {
		if msg := dirProblem(workingDir); msg != "" {
			workingDirOK = false
			problems = append(problems, fmt.Sprintf("working_dir %s: %s", workingDir, msg))
		}
	}
	for _, d := range watchDirs {
		p := d
		if !filepath.IsAbs(p) && workingDir != "" {
			if !workingDirOK {
				continue // relative to a directory that does not exist: reported above
			}
			p = filepath.Join(workingDir, p) // a run watches it from the working directory
		}
		if _, err := os.Stat(p); err != nil {
			problems = append(problems, fmt.Sprintf("watch_dir %s: %s", d, statProblem(err, "does not exist")))
		}
	}
	return problems
}

// dirProblem says why path cannot be used as a directory ("" when it can).
func dirProblem(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return statProblem(err, "not an existing directory")
	}
	if !st.IsDir() {
		return "not a directory" // as a run reports it
	}
	return ""
}

// statProblem words a stat error: notExist when the path is missing,
// otherwise the cause a run reports too (e.g. "permission denied").
func statProblem(err error, notExist string) string {
	if errors.Is(err, fs.ErrNotExist) {
		return notExist
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
