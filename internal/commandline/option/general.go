package option

import (
	"flag"

	"github.com/magicdrive/enma/internal/common"
)

type GeneralOption struct {
	ConfigFilePath     string
	HelpFlag           bool
	VersionFlag        bool
	IndividualHelpFlag bool
	FlagSet            *flag.FlagSet
}

func ParseGeneral(args []string) (*GeneralOption, error) {
	fs := flag.NewFlagSet("general", flag.ExitOnError)

	// --config
	configOpt := fs.String("config", "", "Defines enma.toml (optional. default: Enma.toml,.enma.toml, .config/~, .enma/~))")
	fs.StringVar(configOpt, "c", "", "Defines enma.toml (optional. default: Enma.toml,.enma.toml, .config/~, .enma/~)")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	// --version
	versionFlagOpt := fs.Bool("version", false, "Show version.")
	fs.BoolVar(versionFlagOpt, "v", false, "Show version.")

	fs.Usage = common.EnmaHelpFunc

	// Parse flags
	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	options := &GeneralOption{
		ConfigFilePath: *configOpt,
		HelpFlag:       *helpFlagOpt,
		VersionFlag:    *versionFlagOpt,
		FlagSet:        fs,
	}

	if err := options.Normalize(); err != nil {
		return nil, err
	}

	return options, nil
}

func (cr *GeneralOption) Normalize() error {

	if cr.ConfigFilePath != "" {
		if stat, err := common.FileExists(cr.ConfigFilePath); err != nil || !stat {
			return err
		}
	}

	if cr.HelpFlag == false && cr.VersionFlag == false && cr.ConfigFilePath == "" {
		if path, err := common.FindEnmaConfigFile(); err == nil {
			cr.ConfigFilePath = path
		}
	}

	return nil
}
