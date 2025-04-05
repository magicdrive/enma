package commandline

import (
	_ "embed"

	"github.com/magicdrive/enma/internal/commandline/subcmd"
)

const (
	Hotload = "hotload"
	Watch   = "watch"
	Init    = "init"
	General = ""
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
	default:
		err := subcmd.General(version, args)
		return err
	}
}
