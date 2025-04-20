package configure_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/configure"
)

func TestNewWatchOptionFromTOMLConfig_Minimal(t *testing.T) {
	conf := configure.TomlWatchConf{
		Cmd: "echo watch",
	}

	opt, err := configure.NewWatchOptionFromTOMLConfig(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opt.Cmd != "echo watch" {
		t.Errorf("expected Cmd to be 'echo watch', got '%s'", opt.Cmd)
	}

	if opt.CheckContentDiff.String() != "on" {
		t.Errorf("expected CheckContentDiff to be 'on'")
	}
	if opt.AbsolutePathFlag.String() != "on" {
		t.Errorf("expected AbsolutePathFlag to be 'on'")
	}
}

func TestNewWatchOptionFromTOMLConfig_AllFields(t *testing.T) {
	checkContentDiff := false
	absolutePath := false

	conf := configure.TomlWatchConf{
		PreCmd:           "echo pre",
		Cmd:              "do stuff",
		PostCmd:          "echo post",
		WorkingDir:       "/tmp",
		Placeholder:      "{}",
		ArgsPathStyle:    "basename",
		CheckContentDiff: &checkContentDiff,
		AbsolutePath:     &absolutePath,
		Timeout:          "15s",
		Delay:            "3s",
		Retry:            2,
		DefaultIgnore:    "minimal",
		WatchDir:         []string{"a", "b"},
		PatternRegexp:    `.*\.go`,
		IncludeExt:       []string{".go"},
		IgnoreRegex:      ".*_test.go",
		ExcludeExt:       []string{".tmp"},
		ExcludeDir:       []string{"vendor"},
		EnmaIgnore:       []string{".enmaignore"},
		LogPath:          "log.txt",
		PidPath:          "pid.txt",
	}

	opt, err := configure.NewWatchOptionFromTOMLConfig(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opt.CheckContentDiff.String() != "off" {
		t.Errorf("expected CheckContentDiff to be 'off'")
	}
	if opt.AbsolutePathFlag.String() != "off" {
		t.Errorf("expected AbsolutePathFlag to be 'off'")
	}
	if opt.ArgsPathStyleStringValue != "basename" {
		t.Errorf("expected ArgsPathStyleStringValue to be 'basename'")
	}
	if opt.Retry != 2 {
		t.Errorf("expected Retry to be 2")
	}
	if opt.TimeoutValue != "15s" || opt.DelayValue != "3s" {
		t.Errorf("unexpected timeout or delay values")
	}
}

func TestNewWatchOptionFromTOMLConfig_MissingRequired(t *testing.T) {
	conf := configure.TomlWatchConf{}
	_, err := configure.NewWatchOptionFromTOMLConfig(conf)
	if err == nil {
		t.Fatal("expected error due to missing required fields")
	}
}
