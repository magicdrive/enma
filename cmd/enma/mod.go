package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/magicdrive/enma/internal/commandline"
	"github.com/magicdrive/enma/internal/commandline/subcmd"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/textbank"
)

func Execute(version string) {
	if len(os.Args) <= 1 {
		if err := Default(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, textbank.ShortHelpMessage)
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
		return errors.New("Cann't find enma config file. (Enma.toml, .enma.toml, .enma/enma.toml)")
	} else {
		if err := subcmd.RunViaConfigfile(path); err != nil {
			return err
		}
	}
	return nil
}
