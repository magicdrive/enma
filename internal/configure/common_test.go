
package configure_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/enma/internal/configure"
)

func TestFallback(t *testing.T) {
	if got := configure.Fallback("value", "default"); got != "value" {
		t.Errorf("expected 'value', got '%s'", got)
	}
	if got := configure.Fallback("", "default"); got != "default" {
		t.Errorf("expected 'default', got '%s'", got)
	}
}

func TestJoinCommaAndTrimUniq(t *testing.T) {
	input := []string{" a", "b ", "a", "c", "b"}
	expected := "a,b,c"

	result := configure.JoinComma(input)
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestFallbackArray(t *testing.T) {
	defaults := []string{"x", "y"}

	got := configure.FallbackArray([]string{}, defaults)
	if len(got) != 2 || got[0] != "x" {
		t.Errorf("expected fallback to defaults")
	}

	got2 := configure.FallbackArray([]string{" a ", "b"}, defaults)
	if len(got2) != 2 || got2[0] != " a " {
		t.Errorf("expected original values when non-empty")
	}
}

func TestFindEnmaConfigFile(t *testing.T) {
	tmpFile := "Enma.toml"
	err := os.WriteFile(tmpFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Remove(tmpFile)

	found, err := configure.FindEnmaConfigFile()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if found != tmpFile {
		t.Errorf("expected '%s', got '%s'", tmpFile, found)
	}
}

func TestLoadToml_InvalidPath(t *testing.T) {
	_, err := configure.LoadToml("nonexistent.toml")
	if err == nil {
		t.Errorf("expected error for nonexistent file")
	}
}

func TestLoadToml_InvalidFormat(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "invalid.toml")
	os.WriteFile(tmpFile, []byte("invalid = = ="), 0644)
	defer os.Remove(tmpFile)

	_, err := configure.LoadToml(tmpFile)
	if err == nil {
		t.Errorf("expected parse error for invalid TOML")
	}
}
