package commandline

import (
	"fmt"
	"strings"

	_ "embed"

	"github.com/magicdrive/enma/internal/commandline/subcmd"
)

const (
	Hotload   = "hotload"
	Watch     = "watch"
	Scheduler = "scheduler"
	Init      = "init"
	Config    = "config"
	General   = ""
)

var HelpMessage string

func Execute(version string, args []string) error {
	subcommand := args[0]

	switch subcommand {
	case Hotload:
		err := subcmd.Hotload(args[1:])
		return err
	case Watch:
		err := subcmd.Watch(args[1:])
		return err
	case Scheduler:
		err := subcmd.Scheduler(args[1:])
		return err
	case Init:
		err := subcmd.Init(args[1:])
		return err
	case Config:
		return subcmd.Config(args[1:])
	default:
		if !strings.HasPrefix(subcommand, "-") {
			return fmt.Errorf("unknown subcommand %q (use hotload, watch, scheduler, init or config)\nRun 'enma --help' for usage", subcommand)
		}
		err := subcmd.General(version, args)
		return err
	}
}
