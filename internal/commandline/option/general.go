package option

import (
	"flag"
	"fmt"
)

type GeneralOption struct {
	HelpFlag       bool
	VersionFlag    bool
	ConfigFilePath string
}

func ParseGeneral(args []string) {
	fs := flag.NewFlagSet("general", flag.ExitOnError)
	options := GeneralOption{}

	fs.StringVar(&options.ConfigFilePath, "config", "", "Defines target enma process name (required)")

	fs.Parse(args)

	fmt.Println("Executing ctrl with:", options)
}
