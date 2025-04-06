package subcmd

import (
	"fmt"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/text"
)

func Init(args []string) error {
	opt, err := option.ParseInit(args)

	if err != nil {
		return err
	}
	return RunInit(opt)
}

func RunInit(opt *option.InitOption) error {
	var tomlString string
	switch opt.ModeOpt {
	case "hotload":
		tomlString = text.DefaultHotloadEnmaToml
	case "watch":
		tomlString = text.DefaultWatchEnmaToml
	default:
		return fmt.Errorf("Invalid --mode: %s", opt.ModeOpt)
	}

	if err := common.CreateNewFileWithContent(opt.FileNameOpt, tomlString); err != nil {
		return err
	}

	common.CreateNewFileWithContent(opt.EnmaIgnoreName, text.DefaultEnmaIgnore)

	return nil
}
