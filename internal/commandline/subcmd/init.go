package subcmd

import (
	"fmt"
	"os"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/textbank"
)

func Init(args []string) error {
	opt, err := option.ParseInit(args)

	if err != nil {
		return err
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	return RunInit(opt)
}

func RunInit(opt *option.InitOption) error {
	var textString string

	switch opt.ModeOpt {
	case "hotload":
		textString = textbank.DefaultHotloadEnmaToml
	case "watch":
		textString = textbank.DefaultWatchEnmaToml
	case "scheduler":
		textString = textbank.DefaultSchedulerEnmaToml
	case "enmaignore":
		textString = textbank.MinimalEnmaIgnore
	default:
		return fmt.Errorf("invalid --mode %q (use hotload, watch, scheduler or enmaignore)", opt.ModeOpt)
	}

	if err := common.CreateNewFileWithContent(opt.FileNameOpt, textString); err != nil {
		return err
	}
	fmt.Printf("Created %s (mode %s).\n", opt.FileNameOpt, opt.ModeOpt)
	return nil
}
