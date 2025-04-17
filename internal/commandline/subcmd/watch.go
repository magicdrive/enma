package subcmd

import (
	"os"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

func Watch(args []string) error {
	opt, err := option.ParseWatch(args)

	if err != nil {
		return err
	}

	return RunWatch(opt)
}

func RunWatch(opt *option.WatchOption) error {
	if opt.WorkingDir != "" {
		if err := os.Chdir(opt.WorkingDir); err != nil {
			return err
		}
	}

	runner := core.NewWatchRunner(opt)

	if err := runner.Start(); err != nil {
		return err
	} else {
		return nil
	}
}
