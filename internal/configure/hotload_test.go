package configure_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/configure"
)

func TestNewHotloadOptionFromTOMLConfig_Minimal(t *testing.T) {
	conf := configure.TomlHotloadConf{
		Daemon: "app",
		Build:  "go build -o app ./cmd/app",
	}

	opt, err := configure.NewHotloadOptionFromTOMLConfig(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opt.Daemon != "app" {
		t.Errorf("expected Daemon to be 'app', got '%s'", opt.Daemon)
	}

	if opt.Build != "go build -o app ./cmd/app" {
		t.Errorf("unexpected Build value: %s", opt.Build)
	}

	if opt.BuildAtStart.String() != "on" {
		t.Errorf("expected BuildAtStart to be 'on'")
	}

	if opt.CheckContentDiff.String() != "on" {
		t.Errorf("expected CheckContentDiff to be 'on'")
	}
}

func TestNewHotloadOptionFromTOMLConfig_AllFields(t *testing.T) {
	buildAtStart := false
	checkContentDiff := true
	absolutePath := false

	conf := configure.TomlHotloadConf{
		Daemon:           "mydaemon",
		Signal:           "SIGKILL",
		Build:            "go build",
		PreBuild:         "echo pre",
		PostBuild:        "echo post",
		WorkingDir:       "/tmp",
		Placeholder:      "{}",
		ArgsPathStyle:    "dirname",
		BuildAtStart:     &buildAtStart,
		CheckContentDiff: &checkContentDiff,
		AbsolutePath:     &absolutePath,
		Timeout:          "10sec",
		Delay:            "3sec",
		Retry:            2,
		DefaultIgnore:    "minimal",
		WatchDir:         []string{"src", "pkg"},
		PatternRegexp:    `.*\.go`,
		IncludeExt:       []string{".go", ".mod"},
		IgnoreRegex:      "_test.go",
		ExcludeExt:       []string{".tmp"},
		ExcludeDir:       []string{"vendor"},
		EnmaIgnore:       []string{".enmaignore"},
		LogPath:          "/tmp/enma.log",
		PidPath:          "/tmp/enma.pid",
	}

	opt, err := configure.NewHotloadOptionFromTOMLConfig(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opt.SignalName.String() != "SIGKILL" {
		t.Errorf("expected BuildAtStart to be 'off'")
	}
	if opt.BuildAtStart.String() != "off" {
		t.Errorf("expected BuildAtStart to be 'off'")
	}
	if opt.CheckContentDiff.String() != "on" {
		t.Errorf("expected CheckContentDiff to be 'on'")
	}
	if opt.AbsolutePathFlag.String() != "off" {
		t.Errorf("expected AbsolutePathFlag to be 'off'")
	}
	if opt.WorkingDir != "/tmp" {
		t.Errorf("expected WorkingDir to be '/tmp', got '%s'", opt.WorkingDir)
	}
	if opt.TimeoutValue != "10sec" || opt.DelayValue != "3sec" {
		t.Errorf("unexpected Timeout/Delay values")
	}
	if opt.Retry != 2 {
		t.Errorf("expected Retry to be 2")
	}
}

func TestNewHotloadOptionFromTOMLConfig_MissingRequired(t *testing.T) {
	conf := configure.TomlHotloadConf{}
	_, err := configure.NewHotloadOptionFromTOMLConfig(conf)
	if err == nil {
		t.Fatal("expected error due to missing required fields")
	}
}
