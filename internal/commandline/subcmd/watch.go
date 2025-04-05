package subcmd

import (
	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
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
	if opt.PidPathOpt != "" {
		common.CreatePidFile(opt.PidPathOpt)
		defer common.DeletePidFile(opt.PidPathOpt)
	}

	runner := core.NewWatchRunner(opt)

	if err := runner.Start(); err != nil {
		return err
	} else {
		return nil
	}
}
