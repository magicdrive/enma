package configure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func FindEnmaConfigFile() (string, error) {
	candidates := []string{
		"Enma.toml",
		".enma.toml",
		filepath.Join(".enma", "enma.toml"),
		filepath.Join(".config", "enma.toml"),
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", os.ErrNotExist
}

func fallback(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}

func TrimSpaceAndUniq(values []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if _, ok := seen[trimmed]; !ok {
			seen[trimmed] = struct{}{}
			result = append(result, trimmed)
		}
	}
	return result
}

func JoinComma(values []string) string {
	uniqValues := TrimSpaceAndUniq(values)
	if uniqValues == nil || len(uniqValues) == 0 {
		return ""
	}
	return strings.Join(uniqValues, ",")
}

func fallbackArray(val, def []string) []string {
	uniqVal := TrimSpaceAndUniq(val)
	if len(uniqVal) == 0 {
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
