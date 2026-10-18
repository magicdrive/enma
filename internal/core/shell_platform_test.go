package core_test

import (
	"os/exec"
	"testing"

	"github.com/magicdrive/enma/internal/core"
)

// The command keeps the environment it inherits, and enma's values replace
// stale ones of the same name (audit B-2): checked on every platform with the
// Windows way of passing a command.
func TestShellEnv_KeepsTheInheritedEnvironment(t *testing.T) {
	t.Setenv("ENMA_T_INHERITED", "kept")
	t.Setenv("__ENMA_ARG1", "stale")
	t.Setenv("__ENMA_SCRIPT", "echo stale")
	cmd := exec.Command("sh", core.ShellCommandArgs(`printf '%s|%s|%s|%s' "$ENMA_T_INHERITED" {} "${__ENMA_ARG1-unset}" "${__ENMA_SCRIPT-unset}"`, "{}", "p q")...)
	core.EnvShellForTest(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if string(out) != "kept|p q|unset|unset" {
		t.Fatalf("got %q, want %q", out, "kept|p q|unset|unset")
	}
}

// The daemon runs as `sh -c <daemon>`: braces, quotes and backslashes in it
// reach sh as written, as enma passes it on this platform (audit M-1, B-3).
func TestShellDaemonForm_ReachesShAsWritten(t *testing.T) {
	cmd := exec.Command("sh", "-c", `printf '[%s]' './x{,.exe}' 'a b' "q" 'C:\\data'`)
	core.AdaptShellForTest(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if want := `[./x{,.exe}][a b][q][C:\\data]`; string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
