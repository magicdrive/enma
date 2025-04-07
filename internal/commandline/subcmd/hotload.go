package subcmd

import (
	"os"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/core"
)

func Hotload(args []string) error {
	opt, err := option.ParseHotload(args)

	if err != nil {
		return err
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	return RunHotload(opt)
}

func RunHotload(opt *option.HotloadOption) error {
	if opt.WatchDir != "" {
		if err := os.Chdir(opt.WatchDir); err != nil {
			return err
		}
	}

	if opt.PidPathOpt != "" {
		common.CreatePidFile(opt.PidPathOpt)
		defer common.DeletePidFile(opt.PidPathOpt)
	}

	runner := core.NewHotloadRunner(opt)

	if err := runner.Start(); err != nil {
		return err
	} else {
		return nil
	}

}
