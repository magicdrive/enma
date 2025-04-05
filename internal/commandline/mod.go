package commandline

import (
	"fmt"
	"os"

	_ "embed"

	"github.com/magicdrive/enma/internal/commandline/subcmd"
	"github.com/magicdrive/enma/internal/text"
)

const (
	Hotload = "hotload"
	Watch   = "watch"
	General = ""
)

var HelpMessage string

func Execute(version string, args []string) error {
	if len(args) < 1 {
		fmt.Println(text.HelpMessage)
		os.Exit(1)
	}

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
