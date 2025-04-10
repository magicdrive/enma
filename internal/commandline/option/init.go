package option

import (
	"flag"
	"fmt"

	"github.com/magicdrive/enma/internal/common"
)

type InitOption struct {
	ModeOpt        string
	FileNameOpt    string
	EnmaIgnoreName string
	FlagSet        *flag.FlagSet
	HelpFlag       bool
}

func ParseInit(args []string) (*InitOption, error) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)

	// --config
	modeOpt := fs.String("mode", "hotload", "Defines create enma.toml support mode. (optional. default: hotload)")
	fs.StringVar(modeOpt, "m", "hotload", "Defines create enma.toml support mode. (optional. default: hotload)")

	// --filename
	fileNameOpt := fs.String("file-name", "./Enma.toml", "Specicy create enma.toml file-name.")
	fs.StringVar(fileNameOpt, "f", "./Enma.toml", "Specicy create enma.toml file-name.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = common.EnmaHelpFunc

	fs.Parse(args)

	fs.Usage = common.EnmaInitHelpFunc

	options := &InitOption{
		ModeOpt:     *modeOpt,
		FileNameOpt: *fileNameOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	if err := options.Normalize(); err != nil {
		return nil, err
	}

	return options, nil
}

func (cr *InitOption) Normalize() error {

	if stat, err := common.FileExists(cr.FileNameOpt); err != nil {
		return fmt.Errorf("File parmission error: %v", err)
	} else if stat {
		return fmt.Errorf("file already exists.: %s", cr.FileNameOpt)
	}
	cr.EnmaIgnoreName = ".enmaignore"

	return nil
}
