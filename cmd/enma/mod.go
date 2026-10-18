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
	common.SetVersion(version)
	if len(os.Args) <= 1 {
		if err := Default(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, textbank.ShortHelpMessage)
			os.Exit(1)
		}
		os.Exit(0)
	}

	err := commandline.Execute(version, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// errNoConfig: enma without arguments (or with flags only) found no config file.
var errNoConfig = errors.New(subcmd.NoConfigMessage)

func Default() error {
	if path, err := common.FindEnmaConfigFile(); err != nil {
		return errNoConfig
	} else {
		if err := subcmd.RunViaConfigfile(path); err != nil {
			return err
		}
	}
	return nil
}
