package option

import (
	"flag"
	"fmt"

	"github.com/magicdrive/enma/internal/common"
)

type InitOption struct {
	ModeOpt     string
	FileNameOpt string
	FlagSet     *flag.FlagSet
	HelpFlag    bool
}

func ParseInit(args []string) (*InitOption, error) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)

	// --config
	modeOpt := fs.String("mode", "hotload", "Defines create enma.toml support mode. (optional. default: hotload)")
	fs.StringVar(modeOpt, "m", "hotload", "Defines create enma.toml support mode. (optional. default: hotload)")

	// --filename
	fileNameOpt := fs.String("file-name", "", "Specicy create enma.toml file-name.")
	fs.StringVar(fileNameOpt, "f", "", "Specicy create enma.toml file-name.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = common.UsageErrorFunc("enma init") // parse errors; replaced by the help after parsing

	// Parse flags
	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	fs.Usage = common.EnmaInitHelpFunc

	options := &InitOption{
		ModeOpt:     *modeOpt,
		FileNameOpt: *fileNameOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	if options.HelpFlag { // the help needs no file name check (the default file may exist)
		return options, nil
	}

	if err := options.Normalize(); err != nil {
		return nil, err
	}

	return options, nil
}

func (cr *InitOption) Normalize() error {

	if cr.FileNameOpt == "" {
		switch cr.ModeOpt {
		case "enmaignore":
			cr.FileNameOpt = ".enmaignore"
		default:
			cr.FileNameOpt = "Enma.toml"
		}
	}

	if stat, err := common.FileExists(cr.FileNameOpt); err != nil {
		return fmt.Errorf("cannot create %s: %w", cr.FileNameOpt, err)
	} else if stat {
		return fmt.Errorf("cannot create %s: it already exists", cr.FileNameOpt)
	}

	return nil
}
