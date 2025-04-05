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

	common.CreatePidFile(opt.PidPath)
	defer common.DeletePidFile(opt.PidPath)

	runner := core.NewWatchRunner(opt)

	if err := runner.Start(); err != nil {
		return err
	} else {
		return nil
	}
}
