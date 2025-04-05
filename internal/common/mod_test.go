package common_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/common"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"one line", 1},
		{"two\nlines", 2},
		{"", 1},
	}

	for _, tt := range tests {
		got := common.CountLines(tt.input)
		if got != tt.want {
			t.Errorf("CountLines(%q) = %d; want %d", tt.input, got, tt.want)
		}
	}
}

func TestGetExecutableDir(t *testing.T) {
	dir, err := common.GetExecutableDir()
	if err != nil {
		t.Fatalf("GetExecutableDir failed: %v", err)
	}
	if dir == "" {
		t.Error("GetExecutableDir returned empty path")
	}
}

func TestCreateFileWithDirs(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "nested", "test.txt")
	content := "hello"

	err := common.CreateFileWithDirs(targetPath, content)
	if err != nil {
		t.Fatalf("CreateFileWithDirs failed: %v", err)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(data) != content {
		t.Errorf("File content = %q; want %q", string(data), content)
	}
}

func TestCreateFileWithDirs_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "test.txt")

	err := common.CreateFileWithDirs(targetPath, "")
	if err != nil {
		t.Fatalf("CreateFileWithDirs with empty content failed: %v", err)
	}

	if _, err := os.Stat(targetPath); err == nil {
		t.Errorf("File %s should not exist when content is empty", targetPath)
	}
}

func TestRemoveFileIfContains(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "sample.txt")
	content := "Hello, keyword!"
	os.WriteFile(tmpFile, []byte(content), 0644)

	err := common.RemoveFileIfContains(tmpFile, "keyword")
	if err != nil {
		t.Fatalf("RemoveFileIfContains failed: %v", err)
	}

	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("Expected file to be removed, but it still exists")
	}
}

func TestRemoveFileIfContains_NoMatch(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "sample.txt")
	content := "Hello, world!"
	os.WriteFile(tmpFile, []byte(content), 0644)

	err := common.RemoveFileIfContains(tmpFile, "nomatch")
	if err != nil {
		t.Fatalf("RemoveFileIfContains failed: %v", err)
	}

	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("Expected file to remain, but got error: %v", err)
	}
}

func TestCreateAndDeletePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "pidfile")

	err := common.CreatePidFile(pidPath)
	if err != nil {
		t.Fatalf("CreatePidFile failed: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	if !strings.Contains(string(data), os.Args[0]) && string(data) == "" {
		t.Errorf("PID file should contain the process ID, got: %s", string(data))
	}

	err = common.DeletePidFile(pidPath)
	if err != nil {
		t.Fatalf("DeletePidFile failed: %v", err)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("Expected PID file to be deleted")
	}
}
