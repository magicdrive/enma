package subcmd

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/configure"
)

func General(version string, args []string) error {
	opt, err := option.ParseGeneral(args)

	if err != nil {
		return err
	}

	if opt.VersionFlag {
		fmt.Printf("enma version %s\n", version)
		os.Exit(0)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	return RunViaOptionConfigfile(opt)
}

func RunViaConfigfile(config_file_path string) error {
	data, err := os.ReadFile(config_file_path)
	if err != nil {
		return err
	}
	var cfg configure.TomlConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	switch cfg.Subcommand.Use {
	case "hotload":
		if hotloadOpt, err := configure.NewHotloadOptionFromTOMLConfig(cfg.Subcommand.Hotload); err != nil {
			return err
		} else {
			return RunHotload(hotloadOpt)
		}
	case "watch":
		if watchOpt, err := configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch); err != nil {
			return err
		} else {
			return RunWatch(watchOpt)
		}
	default:
		return fmt.Errorf("unsupported subcommand: %s", cfg.Subcommand.Use)
	}
}

func RunViaOptionConfigfile(opt *option.GeneralOption) error {
	return RunViaConfigfile(opt.ConfigFilePath)
}
