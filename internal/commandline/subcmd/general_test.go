package subcmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/magicdrive/enma/internal/configure"
)

// A4-01: go-toml maps keys to fields ignoring case, so a key that differs from a
// field's tag only in case is used, not unknown; a real typo is unknown.
func TestKnownKey(t *testing.T) {
	for key, want := range map[string]bool{
		"daemon": true, "Daemon": true, "DAEMON": true, "start-at-once": true, "start_at_once": true,
		"daemonx": false, "timout": false, "": false,
	} {
		if got := knownKey(configure.TomlHotloadConf{}, key); got != want {
			t.Errorf("knownKey(%q) = %v, want %v", key, got, want)
		}
	}
}

// A4-03: every line of a validation error names the file, and a CLI flag the
// shared validation reports is shown with its TOML key.
func TestInConfigFile(t *testing.T) {
	err := inConfigFile("a.toml", errors.New("--timeout invalid value\n--signal invalid signal name"), true)
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	for i, want := range []string{"a.toml: timeout (--timeout) invalid value", "a.toml: signal (--signal) invalid signal name"} {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
}

// H5R2-01/02: only a flag at the start of a line is named by its key; values
// the user wrote stay as they are; blank lines are dropped; without keys
// (scheduler, start checks) nothing is changed.
func TestInConfigFile_OnlyLeadingFlags(t *testing.T) {
	err := inConfigFile("a.toml", errors.New("--delay bad \"x --timeout\"\n\nfailed to compile pattern-regexp: missing )\n--signalx no"), true)
	want := "a.toml: delay (--delay) bad \"x --timeout\"\n" +
		"a.toml: failed to compile pattern_regex (--pattern-regex): missing )\n" +
		"a.toml: --signalx no"
	if err.Error() != want {
		t.Fatalf("got\n%s\nwant\n%s", err, want)
	}
	err = inConfigFile("s.toml", errors.New(`scheduler job "x --delay" (#1): cron: bad`), false)
	if err.Error() != `s.toml: scheduler job "x --delay" (#1): cron: bad` {
		t.Fatalf("without keys: %s", err)
	}
}
