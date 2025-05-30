package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/enma/internal/textbank"
)

func StartMessage() string {
	return ReplacePlaceholders(textbank.BareStartMessage, "{version}", Version())
}

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

func GetCurrentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
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
	GracefulPrintOut(textbank.HelpMessage, false)
}

func EnmaHotloadHelpFunc() {
	GracefulPrintOut(textbank.HotloadHelpMessage, false)
}

func EnmaWatchHelpFunc() {
	GracefulPrintOut(textbank.WatchHelpMessage, false)
}

func EnmaInitHelpFunc() {
	GracefulPrintOut(textbank.InitHelpMessage, false)
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

func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

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

func CreateNewFileWithContent(filename string, content string) error {
	if stat, err := FileExists(filename); err != nil || stat {
		return fmt.Errorf("already exists or permission error: %s. %v", filename, err)
	}
	return CreateFileWithDirs(filename, content)
}
