package model_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/model"
	"github.com/magicdrive/enma/internal/textbank"
)

func TestIgnoreType_Set(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
		expected    model.IgnoreType
	}{
		{"maximum", false, model.IgnoreType("maximum")},
		{"max", false, model.IgnoreType("maximum")},
		{"minimal", false, model.IgnoreType("minimal")},
		{"min", false, model.IgnoreType("minimal")},
		{"mini", false, model.IgnoreType("minimal")},
		{"nothing", false, model.IgnoreType("nothing")},
		{"none", false, model.IgnoreType("nothing")},
		{"no", false, model.IgnoreType("nothing")},
		{"aaaaaaaaa", true, ""},
		{"MAX|MIN", true, ""},
		{"", true, ""},
	}

	for _, tt := range tests {
		var s model.IgnoreType
		err := s.Set(tt.input)
		if (err != nil) != tt.expectError {
			t.Errorf("Set(%q) error = %v, want error: %v", tt.input, err, tt.expectError)
		}
		if !tt.expectError && s != tt.expected {
			t.Errorf("Set(%q) = %v, want %v", tt.input, s, tt.expected)
		}
	}
}

func TestIgnoreType_String(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"maximum", textbank.MaximumEnmaIgnore},
		{"max", textbank.MaximumEnmaIgnore},
		{"minimal", textbank.MinimalEnmaIgnore},
		{"min", textbank.MinimalEnmaIgnore},
		{"mini", textbank.MinimalEnmaIgnore},
		{"nothing", ""},
		{"none", ""},
		{"no", ""},
	}
	for _, tt := range tests {
		var s model.IgnoreType
		if err := s.Set(tt.input); err != nil {
			t.Errorf("unexpected error, input: %v, error: %v", tt.input, err)
		}
		if s.EnmaignoreText() != tt.expected {
			t.Errorf("EnmaignoreText() = %q, want %q", s.EnmaignoreText(), tt.expected)
		}
	}
}
