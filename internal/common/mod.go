package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/enma/internal/text"
)

func CountLines(message string) int {
	return len(strings.Split(message, "\n"))
}

func GracefulPrintOut(message string, noPagerFlag bool) {

	if noPagerFlag {
		fmt.Print(message)
		return
	}

	height, _, err := GetTerminalSize()
	if err != nil {
		//Unable to get terminal size
		fmt.Print(message)
		return
	}

	lines := CountLines(message)

	if lines > height {
		ShowWithLess(message)
	} else {
		fmt.Print(message)
	}
}

func GetExecutableDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)
	return exeDir, nil
}

func EnmaHelpFunc() {
	GracefulPrintOut(text.HelpMessage, false)
}

func CreateFileWithDirs(path string, content string) error {
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	if content == "" {
		return nil
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func RemoveFileIfContains(path string, keyword string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if keyword != "" && strings.Contains(string(data), keyword) {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove file: %w", err)
		}
	}

	return nil
}

func CreatePidFile(path string) error {
	pid := os.Getpid()
	return CreateFileWithDirs(path, fmt.Sprintf("%d", pid))
}

func DeletePidFile(path string) error {
	pid := os.Getpid()
	return RemoveFileIfContains(path, fmt.Sprintf("%d", pid))
}

func ToAbsolutePath(relPath string) string {
	result, _ := filepath.Abs(relPath)
	return result
}

func ToRelativePath(absPath string) string {
	baseDir, _ := os.Getwd()
	result, _ := filepath.Rel(baseDir, absPath)
	return result
}

func ReplacePlaceholders(original, target, replacement string) string {
	if original == "" {
		return ""
	}
	result := strings.ReplaceAll(original, target, replacement)
	return result
}

func Fallback(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}

func CommaSeparated2StringList(s string) []string {
	if s == "" {
		return nil
	}

	seen := make(map[string]struct{}, 16)
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			if _, exists := seen[trimmed]; !exists {
				seen[trimmed] = struct{}{}
				result = append(result, trimmed)
			}
		}
	}

	return result
}

func TrimDotSlash(path string) string {
    return strings.TrimPrefix(path, "./")
}
