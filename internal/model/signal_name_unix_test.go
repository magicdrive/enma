//go:build !windows
// +build !windows

package model_test

import (
	"slices"
	"syscall"
	"testing"

	"github.com/magicdrive/enma/internal/model"
)

func TestSignalName_Set_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGKILL", syscall.SIGKILL},
		{"SIGHUP", syscall.SIGHUP},
		{"SIGUSR1", syscall.SIGUSR1},
		{"SIGUSR2", syscall.SIGUSR2},
		{"SIGINT", syscall.SIGINT},
	}

	for _, tt := range tests {
		var s model.SignalName
		err := s.Set(tt.input)
		if err != nil {
			t.Errorf("Set(%q) returned unexpected error: %v", tt.input, err)
		}
		if s.Signal() != tt.expected {
			t.Errorf("SignalName %q: expected signal %d, got %d", tt.input, tt.expected, s.Signal())
		}
		if s.String() != tt.input {
			t.Errorf("SignalName %q: expected string %q, got %q", tt.input, tt.input, s.String())
		}
	}
}

func TestSignalName_Set_Invalid(t *testing.T) {
	invalidInputs := []string{
		"sigterm",
		"TERM",
		"KILLALL",
		"",
		"SIGNONE",
	}

	for _, input := range invalidInputs {
		var s model.SignalName
		err := s.Set(input)
		if err == nil {
			t.Errorf("Set(%q) expected error, got nil", input)
		}
	}
}

func TestSignalName_Signal_PanicOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Signal() should panic on invalid SignalName but did not panic")
		}
	}()

	s := model.SignalName("INVALID_SIGNAL")
	_ = s.Signal()
}

func TestAllowedSignals(t *testing.T) {
	supported := model.AllowedSignals()

	if len(supported) == 0 {
		t.Fatal("allowedSignals() returned empty list")
	}

	expectedSignals := []string{"SIGTERM", "SIGKILL", "SIGHUP", "SIGUSR1", "SIGUSR2", "SIGINT"}

	for _, expected := range expectedSignals {
		found := false
		if slices.Contains(supported, expected) {
			found = true
			break
		}
		if !found {
			t.Errorf("allowedSignals() missing expected signal %q", expected)
		}
	}
}
