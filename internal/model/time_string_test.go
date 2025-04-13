package model_test

import (
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/model"
)

func TestTimeString_Set_Valid(t *testing.T) {
	cases := []string{
		"100ms", "1.5s", "2sec", "10m", "3min", "4h", "0.5d", "10ns", "200us",
	}

	for _, c := range cases {
		t.Run("Valid Set: "+c, func(t *testing.T) {
			var ts model.TimeString
			err := ts.Set(c)
			if err != nil {
				t.Errorf("Set(%q) failed unexpectedly: %v", c, err)
			}
			if ts.String() != c {
				t.Errorf("expected String() to return %q, got %q", c, ts.String())
			}
		})
	}
}

func TestTimeString_Set_Invalid(t *testing.T) {
	cases := []string{
		"", "10", "h", "123x", "1.2.3s", "ms100", "100ms ", " 100ms", "100 ms",
	}

	for _, c := range cases {
		t.Run("Invalid Set: "+c, func(t *testing.T) {
			var ts model.TimeString
			err := ts.Set(c)
			if err == nil {
				t.Errorf("Set(%q) should have failed but did not", c)
			}
		})
	}
}

func TestTimeString_TimeDuration_Valid(t *testing.T) {
	type testCase struct {
		input    string
		expected time.Duration
	}

	cases := []testCase{
		{"100ms", 100 * time.Millisecond},
		{"1.5s", 1500 * time.Millisecond},
		{"2sec", 2 * time.Second},
		{"10m", 10 * time.Minute},
		{"3min", 3 * time.Minute},
		{"4h", 4 * time.Hour},
		{"0.5d", 12 * time.Hour},
		{"10ns", 10 * time.Nanosecond},
		{"200us", 200 * time.Microsecond},
	}

	for _, c := range cases {
		t.Run("Valid Duration: "+c.input, func(t *testing.T) {
			var ts model.TimeString
			err := ts.Set(c.input)
			if err != nil {
				t.Fatalf("Set(%q) failed: %v", c.input, err)
			}

			dur, err := ts.TimeDuration()
			if err != nil {
				t.Fatalf("TimeDuration() failed: %v", err)
			}
			if dur != c.expected {
				t.Errorf("expected duration %v, got %v", c.expected, dur)
			}
		})
	}
}

func TestTimeString_TimeDuration_Invalid_NoSet(t *testing.T) {
	var ts model.TimeString
	_, err := ts.TimeDuration()
	if err == nil {
		t.Error("TimeDuration() should have failed on unset TimeString")
	}
}

func TestTimeString_TimeDuration_Invalid_UnsupportedUnit(t *testing.T) {
	ts := model.TimeString("10xyz") // 不正なユニット
	_, err := ts.TimeDuration()
	if err == nil {
		t.Error("TimeDuration() should have failed for unsupported unit")
	}
}

func TestTimeString_TimeDuration_InvalidFloat(t *testing.T) {
	ts := model.TimeString("abc1d") // 数値が不正
	_, err := ts.TimeDuration()
	if err == nil {
		t.Error("TimeDuration() should have failed for invalid float number in day unit")
	}
}
