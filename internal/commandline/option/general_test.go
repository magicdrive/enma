
package option_test

import (
	"os"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParseGeneral_Defaults(t *testing.T) {
	args := []string{}

	opt, err := option.ParseGeneral(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.HelpFlag {
		t.Errorf("expected HelpFlag to be false by default")
	}
	if opt.VersionFlag {
		t.Errorf("expected VersionFlag to be false by default")
	}
}

func TestParseGeneral_WithHelpAndVersion(t *testing.T) {
	args := []string{"--help", "--version"}

	opt, err := option.ParseGeneral(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !opt.HelpFlag {
		t.Errorf("expected HelpFlag to be true")
	}
	if !opt.VersionFlag {
		t.Errorf("expected VersionFlag to be true")
	}
}

func TestParseGeneral_ConfigFileExists(t *testing.T) {
	tmp := "test_config.toml"
	err := os.WriteFile(tmp, []byte("dummy"), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp)

	args := []string{"--config", tmp}
	opt, err := option.ParseGeneral(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.ConfigFilePath != tmp {
		t.Errorf("expected ConfigFilePath to be '%s', got '%s'", tmp, opt.ConfigFilePath)
	}
}

func TestParseGeneral_ConfigFileNotExists(t *testing.T) {
	args := []string{"--config", "nonexistent.toml"}

	_, err := option.ParseGeneral(args)
	if err == nil {
		t.Errorf("expected error for non-existent config file")
	}
}
