package option_test

import (
	"os"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParseInit_Defaults(t *testing.T) {
	args := []string{}

	opt, err := option.ParseInit(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.ModeOpt != "hotload" {
		t.Errorf("expected ModeOpt to be 'hotload', got '%s'", opt.ModeOpt)
	}

	if opt.FileNameOpt != "Enma.toml" {
		t.Errorf("expected default FileNameOpt to be 'Enma.toml', got '%s'", opt.FileNameOpt)
	}
}

func TestParseInit_ModeEnmaIgnoreSetsCorrectFilename(t *testing.T) {
	args := []string{"--mode", "enmaignore"}

	opt, err := option.ParseInit(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.ModeOpt != "enmaignore" {
		t.Errorf("expected ModeOpt to be 'enmaignore', got '%s'", opt.ModeOpt)
	}

	if opt.FileNameOpt != ".enmaignore" {
		t.Errorf("expected FileNameOpt to be '.enmaignore', got '%s'", opt.FileNameOpt)
	}
}

func TestParseInit_CustomFilename(t *testing.T) {
	args := []string{"--file-name", "custom.toml"}

	opt, err := option.ParseInit(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.FileNameOpt != "custom.toml" {
		t.Errorf("expected FileNameOpt to be 'custom.toml', got '%s'", opt.FileNameOpt)
	}
}

func TestParseInit_FileAlreadyExists(t *testing.T) {
	tmp := "test_exists.toml"
	err := os.WriteFile(tmp, []byte("dummy"), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp)

	args := []string{"--file-name", tmp}
	_, err = option.ParseInit(args)
	if err == nil || err.Error() != "file already exists.: "+tmp {
		t.Errorf("expected file already exists error, got %v", err)
	}
}
