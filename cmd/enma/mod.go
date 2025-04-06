package cmd

import (
	"fmt"
	"os"

	"github.com/magicdrive/enma/internal/commandline"
	"github.com/magicdrive/enma/internal/commandline/subcmd"
	"github.com/magicdrive/enma/internal/common"
)

func Execute(version string) {
	if len(os.Args) <= 1 {
		if err := Default(); err != nil {
			common.EnmaHelpFunc()
		}
		os.Exit(0)
	}

	err := commandline.Execute(version, os.Args[1:])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func Default() error {
	if path, err := common.FindEnmaConfigFile(); err != nil {
		return err
	} else {
		if err := subcmd.RunViaConfigfile(path); err != nil {
			return err
		}
	}
	return nil
}
