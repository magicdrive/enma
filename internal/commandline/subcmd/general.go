package subcmd

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/configure"
)

func General(version string, args []string) error {
	data, err := os.ReadFile("")
	if err != nil {
		return err
	}

	var cfg configure.TomlConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	switch cfg.Subcommand.Use {
	case "hotload":
		configure.NewHotloadOptionFromTOMLConfig(cfg.Subcommand.Hotload)
	case "watch":
		configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch)
	default:
		fmt.Errorf("unsupported subcommand: %s", cfg.Subcommand.Use)
	}
	return nil
}
