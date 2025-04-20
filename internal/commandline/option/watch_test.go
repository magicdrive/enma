package option_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParseWatch_MinimalArgs(t *testing.T) {
	args := []string{
		"--command", "echo hello",
	}

	opt, err := option.ParseWatch(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if opt.Cmd != "echo hello" {
		t.Errorf("expected Cmd to be 'echo hello', got '%s'", opt.Cmd)
	}

	if opt.CheckContentDiff.String() != "on" {
		t.Errorf("expected CheckContentDiff to be 'on', got '%s'", opt.CheckContentDiff.String())
	}

	if opt.WorkingDir == "" {
		t.Error("expected WorkingDir to be set")
	}
}

//var osExit = os.Exit
//func TestParseWatch_MissingRequiredArgs(t *testing.T) {
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
//	option.ParseWatch([]string{})
//}

func TestParseWatch_InvalidTimeout(t *testing.T) {
	args := []string{
		"--command", "run",
		"--timeout", "invalid",
	}

	_, err := option.ParseWatch(args)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}
}

func TestParseWatch_InvalidRegexp(t *testing.T) {
	args := []string{
		"--command", "run",
		"--pattern-regex", "[[invalid",
	}

	_, err := option.ParseWatch(args)
	if err == nil || !strings.Contains(err.Error(), "pattern-regexp") {
		t.Fatalf("expected pattern-regexp compile error, got %v", err)
	}
}

func TestParseWatch_CommaSeparatedParsing(t *testing.T) {
	args := []string{
		"--command", "run",
		"--watch-dir", "src,test",
		"--include-ext", ".go,.mod",
		"--exclude-ext", ".tmp,.log",
		"--exclude-dir", "tmp,vendor",
		"--enmaignore", ".enmaignore,custom.ignore",
	}

	opt, err := option.ParseWatch(args)
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
