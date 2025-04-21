package option_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParseHotload_MinimalArgs(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "go build -o ./myapp ./cmd/myapp",
	}

	opt, err := option.ParseHotload(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.Daemon != "./myapp" {
		t.Errorf("expected Daemon to be './myapp', got '%s'", opt.Daemon)
	}

	if opt.Build != "go build -o ./myapp ./cmd/myapp" {
		t.Errorf("expected Build to be correct, got '%s'", opt.Build)
	}

	if opt.BuildAtStart.String() != "on" {
		t.Errorf("expected BuildAtStart to be 'on', got '%s'", opt.BuildAtStart.String())
	}

	if opt.WorkingDir == "" {
		t.Error("expected WorkingDir to be set")
	}
}

//var osExit = os.Exit
//func TestParseHotload_MissingRequiredArgs(t *testing.T) {
//	defer func() {
//		if r := recover(); r == nil {
//			t.Errorf("expected os.Exit to be called due to missing required flags")
//		}
//	}()
//	oldExit := osExit
//	osExit = func(code int) {
//		panic("os.Exit called")
//	}
//	defer func() { osExit = oldExit }()
//
//	option.ParseHotload([]string{})
//}

func TestParseHotload_InvalidTimeout(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "go build -o ./myapp ./cmd/myapp",
		"--timeout", "notaduration",
	}

	_, err := option.ParseHotload(args)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}
}

func TestParseHotload_InvalidRegexp(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "go build",
		"--pattern-regex", "[[invalid",
	}

	_, err := option.ParseHotload(args)
	if err == nil || !strings.Contains(err.Error(), "pattern-regexp") {
		t.Fatalf("expected pattern-regexp compile error, got %v", err)
	}
}

func TestParseHotload_CommaSeparatedParsing(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "build",
		"--watch-dir", "dir1,dir2",
		"--include-ext", ".go,.mod",
		"--exclude-ext", ".tmp,.log",
		"--exclude-dir", "tmp,vendor",
		"--enmaignore", ".enmaignore,custom.ignore",
	}

	opt, err := option.ParseHotload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(opt.WatchDirList) != 2 {
		t.Errorf("expected 2 watch dirs, got %v", opt.WatchDirList)
	}
	if len(opt.IncludeExtList) != 2 {
		t.Errorf("expected 2 include extensions, got %v", opt.IncludeExtList)
	}
	if len(opt.ExcludeExtList) != 2 {
		t.Errorf("expected 2 exclude extensions, got %v", opt.ExcludeExtList)
	}
	if len(opt.ExcludeDirList) != 2 {
		t.Errorf("expected 2 exclude dirs, got %v", opt.ExcludeDirList)
	}
	if len(opt.EnmaIgnoreList) != 2 {
		t.Errorf("expected 2 enmaignore files, got %v", opt.EnmaIgnoreList)
	}
}

func TestParseHotload_HasPlaceholderBuildDetection(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "go build {}",
		"--placeholder", "{}",
	}

	opt, err := option.ParseHotload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !opt.HasPlaceholderBuild {
		t.Errorf("expected HasPlaceholder to be true")
	}
}

func TestParseHotload_HasPlaceholderPreBuildDetection(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--pre-build", "echo pre-build {}",
		"--build", "go build {}",
		"--placeholder", "{}",
	}

	opt, err := option.ParseHotload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !opt.HasPlaceholderPreBuild {
		t.Errorf("expected HasPlaceholderPreBuild to be true")
	}
}

func TestParseHotload_HasPlaceholderPostBuildDetection(t *testing.T) {
	args := []string{
		"--daemon", "./myapp",
		"--build", "go build main.go",
		"--post-build", "echo post-build {}",
		"--placeholder", "{}",
	}

	opt, err := option.ParseHotload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !opt.HasPlaceholderPostBuild {
		t.Errorf("expected HasPlaceholderPostBuild to be true")
	}
}
