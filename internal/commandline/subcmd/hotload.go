package subcmd

import (
	"os"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

func Hotload(args []string) error {
	opt, err := option.ParseHotload(args)

	if err != nil {
		return err
	}

	return RunHotload(opt)
}

func RunHotload(opt *option.HotloadOption) error {
	if opt.WorkingDir != "" {
		if err := os.Chdir(opt.WorkingDir); err != nil {
			return err
		}
	}

	runner := core.NewHotloadRunner(opt)

	if err := runner.Start(); err != nil {
		return err
	} else {
		return nil
	}

}
