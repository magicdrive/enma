package configure

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func fallback(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}

type Optioner interface {
	Mode() string
}

type TomlConfig struct {
	Subcommand struct {
		Use     string          `toml:"use"`
		Hotload tomlHotloadConf `toml:"hotload"`
		Watch   tomlWatchConf   `toml:"watch"`
	} `toml:"subcommand"`
}

func FindEnmaToml() (string, error) {

	return "", errors.New("Enma.toml not found")

}

func LoadToml(path string) (*TomlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg *TomlConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return cfg, nil

}
