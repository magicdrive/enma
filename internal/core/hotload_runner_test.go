package core_test

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

func TestHotloadRunnerRunBuild_Success(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			Build: "echo build success",
		},
		BuildTimeout: 2 * time.Second,
		ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "echo", "build success")
		},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}

	err := r.RunBuild(context.Background(), "./main.go")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestHotloadRunnerRunBuild_Timeout(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			Build: "long-running",
		},
		BuildTimeout: 1 * time.Second,
		ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHotloadRunnerHelperProcess", "--", "sleep")
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			return cmd
		},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}

	err := r.RunBuild(context.Background(), "./main.go")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHotloadRunnerRunBuild_MockFailure(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			Build: "false",
		},
		BuildTimeout: 1 * time.Second,
		ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}

	err := r.RunBuild(context.Background(), "./main.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHotloadRunnerIsExcludedDir(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			ExcludeDir: "node_modules,tmp",
		},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}

	tests := []struct {
		path   string
		expect bool
	}{
		{"/project/node_modules/a.go", true},
		{"/project/tmp/main.go", true},
		{"/project/src/main.go", false},
	}

	for _, tt := range tests {
		if r.IsExcludedDir(tt.path) != tt.expect {
			t.Errorf("expected %v for %s", tt.expect, tt.path)
		}
	}
}

func TestHotloadRunnerShouldTrigger(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			IncludeExt: ".go",
			ExcludeExt: ".tmp",
		},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}

	tests := []struct {
		filename string
		expect   bool
	}{
		{"main.go", true},
		{"main.tmp", false},
		{"readme.md", false},
	}

	for _, tt := range tests {
		event := fsnotify.Event{Name: tt.filename}
		if r.ShouldTrigger(event) != tt.expect {
			t.Errorf("expected ShouldTrigger(%s) to be %v", tt.filename, tt.expect)
		}
	}
}

func TestHotloadRunnerShouldTrigger_Regex(t *testing.T) {
	t.Run("PatternRegexp match", func(t *testing.T) {
		re, err := regexp2.Compile(`main_.*\.go$`, 0)
		if err != nil {
			t.Fatalf("failed to compile regex: %v", err)
		}
		r := &core.HotloadRunner{
			Options: &option.HotloadOption{
				PatternRegexp: re,
			},
		}
		if err := r.Options.Normalize(); err != nil {
			t.Errorf("HotloadOption normalize error: %v", err)
		}

		event := fsnotify.Event{Name: "main_test.go"}
		if !r.ShouldTrigger(event) {
			t.Errorf("expected PatternRegexp match to trigger")
		}
	})

	t.Run("PatternRegexp no match", func(t *testing.T) {
		re, err := regexp2.Compile(`^main.*\.go$`, 0)
		if err != nil {
			t.Fatalf("failed to compile regex: %v", err)
		}
		r := &core.HotloadRunner{
			Options: &option.HotloadOption{
				PatternRegexp: re,
			},
		}
		if err := r.Options.Normalize(); err != nil {
			t.Errorf("HotloadOption normalize error: %v", err)
		}

		event := fsnotify.Event{Name: "other.txt"}
		if r.ShouldTrigger(event) {
			t.Errorf("expected PatternRegexp non-match to NOT trigger")
		}
	})

	t.Run("IgnoreDirRegexp match", func(t *testing.T) {
		re, err := regexp2.Compile(`vendor`, 0)
		if err != nil {
			t.Fatalf("failed to compile regex: %v", err)
		}
		r := &core.HotloadRunner{
			Options: &option.HotloadOption{
				IgnoreDirRegexp: re,
			},
		}
		if err := r.Options.Normalize(); err != nil {
			t.Errorf("HotloadOption normalize error: %v", err)
		}

		event := fsnotify.Event{Name: "vendor/main.go"}
		if r.ShouldTrigger(event) {
			t.Errorf("expected IgnoreDirRegexp match to prevent triggering")
		}
	})

	t.Run("IgnoreFileRegexp match", func(t *testing.T) {
		re, err := regexp2.Compile(`_gen\.go$`, 0)
		if err != nil {
			t.Fatalf("failed to compile regex: %v", err)
		}
		r := &core.HotloadRunner{
			Options: &option.HotloadOption{
				IgnoreFileRegexp: re,
			},
		}
		if err := r.Options.Normalize(); err != nil {
			t.Errorf("HotloadOption normalize error: %v", err)
		}

		event := fsnotify.Event{Name: "file_gen.go"}
		if r.ShouldTrigger(event) {
			t.Errorf("expected IgnoreFileRegexp match to prevent triggering")
		}
	})
}

// TestHelperProcess is a fake long-running subprocess for timeout testing
func TestHotloadRunnerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	if len(args) > 3 && args[3] == "sleep" {
		select {} // block forever
	}
	os.Exit(0)
}

func TestHotloadRunnerRunPreBuild_Empty(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			PreBuild: "",
		},
		ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			t.Fatal("ExecCommand should not be called when PreBuild is empty")
			return nil
		},
	}
	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}
	err := r.RunPreBuild(context.Background(), "./file.go")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHotloadRunnerRunPostBuild_Empty(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			PostBuild: "",
		},
		ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			t.Fatal("ExecCommand should not be called when PostBuild is empty")
			return nil
		},
	}
	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}
	err := r.RunPostBuild(context.Background(), "./file.go")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHotloadRunnerReplacePlaceholders(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			Placeholder: ":path",
		},
	}
	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}
	cmd := r.ReplacePlaceholders("echo watching :path", "/tmp/foo.go")
	expected := "echo watching /tmp/foo.go"
	if cmd != expected {
		t.Errorf("expected %q, got %q", expected, cmd)
	}
}

func TestHotloadRunnerIsExcludedDir_EmptyExclude(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{
			ExcludeDir: "",
		},
	}
	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}
	if r.IsExcludedDir("/foo/bar") {
		t.Errorf("expected false when ExcludeDir is empty")
	}
}

func TestHotloadRunnerShouldTrigger_Chmod(t *testing.T) {
	r := &core.HotloadRunner{
		Options: &option.HotloadOption{},
	}
	event := fsnotify.Event{
		Name: "main.go",
		Op:   fsnotify.Chmod,
	}
	if err := r.Options.Normalize(); err != nil {
		t.Errorf("HotloadOption normalize error: %v", err)
	}
	if r.ShouldTrigger(event) {
		t.Errorf("expected false for Chmod event")
	}
}

func TestHotloadRunnerLoggingOutput(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	log.Println("hello test log")
	output := buf.String()

	if !strings.Contains(output, "hello test log") {
		t.Errorf("expected log to contain message, got: %q", output)
	}
}

func TestHotloadRunner_CollectWatchDirs_SymlinkResolution(t *testing.T) {
	tmp := t.TempDir()

	// tmp/
	//   a/ <- entity
	//   b/
	//     link_to_a -> ../a  (symlink)

	aDir := filepath.Join(tmp, "a")
	bDir := filepath.Join(tmp, "b")

	if err := os.MkdirAll(aDir, 0755); err != nil {
		t.Fatalf("failed to create aDir: %v", err)
	}
	if err := os.MkdirAll(bDir, 0755); err != nil {
		t.Fatalf("failed to create bDir: %v", err)
	}

	linkPath := filepath.Join(bDir, "link_to_a")
	if err := os.Symlink("../a", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	r := &core.HotloadRunner{
		Options: &option.HotloadOption{},
	}

	if err := r.Options.Normalize(); err != nil {
		t.Fatalf("HotloadOption normalize error: %v", err)
	}

	dirs, err := r.CollectWatchDirs(tmp)
	if err != nil {
		t.Fatalf("CollectWatchDirs failed: %v", err)
	}

	// Helper to normalize paths for consistent comparison
	normalize := func(path string) string {
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatalf("failed to Abs: %v", err)
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			t.Fatalf("failed to EvalSymlinks on %s: %v", abs, err)
		}
		return real
	}

	got := map[string]bool{}
	for _, d := range dirs {

		log.Printf("normalize(d): %s", normalize(d))
		got[normalize(d)] = true
	}

	// Expect: tmp/a and tmp/b (but NOT tmp/b/link_to_a again)
	expect := []string{
		filepath.Join(tmp),
		filepath.Join(tmp, "a"),
		filepath.Join(tmp, "b"),
	}

	for _, path := range expect {
		norm := normalize(path)
		log.Printf("norm: %s", norm)
		if !got[norm] {
			t.Errorf("expected dir missing: %s", norm)
		}
		delete(got, norm)
	}

	for extra := range got {
		t.Errorf("unexpected dir in result: %s", extra)
	}
}
