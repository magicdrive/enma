package option

import (
	"flag"
	"fmt"
	"os"

	"github.com/magicdrive/enma/internal/common"
)

// MaxPreviewNext bounds --next: the fire times listed per scheduler job.
const MaxPreviewNext = 100

type ConfigValidateOption struct {
	ConfigFilePath string // "" when not given: the default config file is searched
	Next           int    // scheduler configs: fire times listed per job (0: none)
	FlagSet        *flag.FlagSet
}

// ParseConfigValidate parses `enma config validate` flags.
func ParseConfigValidate(args []string) (*ConfigValidateOption, error) {
	fs := flag.NewFlagSet("config validate", flag.ExitOnError)
	configOpt := fs.String("config", "", "Config file to check (optional. default: the file a plain `enma` would run)")
	fs.StringVar(configOpt, "c", "", "Config file to check")
	fs.StringVar(configOpt, "f", "", "Config file to check")
	nextOpt := fs.Int("next", 0, "Scheduler configs: list the next N fire times of each job (optional)")
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = common.UsageErrorFunc("enma config validate") // parse errors; replaced by the help after parsing
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	fs.Usage = common.EnmaConfigHelpFunc
	if *helpFlagOpt {
		fs.Usage()
		os.Exit(0)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("config validate: unexpected argument %q (name the file with --config)\nRun 'enma config validate --help' for usage", fs.Arg(0))
	}
	if err := checkNext(*nextOpt); err != nil {
		return nil, fmt.Errorf("config validate: %w", err)
	}
	return &ConfigValidateOption{ConfigFilePath: *configOpt, Next: *nextOpt, FlagSet: fs}, nil
}

func checkNext(n int) error {
	if n < 0 || n > MaxPreviewNext {
		return fmt.Errorf("--next %d: must be between 0 and %d", n, MaxPreviewNext)
	}
	return nil
}
