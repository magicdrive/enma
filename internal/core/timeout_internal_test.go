//go:build !windows

package core

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// H6B-02 / H6A2-01: a step is reported as timed out only when its own
// deadline stopped the command while it was running — not on a shutdown, not
// when the command failed by itself before the deadline, and not because
// stopping what it left behind took past the deadline.
func TestTimeoutError(t *testing.T) {
	killed := errors.New("signal: killed")
	if err := timeoutError(&stepTimeoutError{err: killed}, 5*time.Second); err == nil ||
		err.Error() != "timed out after 5s (--timeout): signal: killed" || !errors.Is(err, killed) {
		t.Fatalf("marked: %v", err)
	}
	if err := timeoutError(nil, time.Second); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := timeoutError(killed, time.Second); err != killed {
		t.Fatalf("plain failure: %v", err)
	}
}

func runStep(t *testing.T, scope *commandScope, timeout, grace time.Duration, script string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(scope.context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	killGroupOnCancel(cmd, scope, grace, time.Second)
	return timeoutError(runInScope(cmd, scope, grace, time.Second, "cmd"), timeout)
}

func TestRunInScope_TimeoutOnlyWhileRunning(t *testing.T) {
	t.Run("deadline stops the command", func(t *testing.T) {
		var scope commandScope
		err := runStep(t, &scope, 300*time.Millisecond, time.Second, "sleep 30")
		if err == nil || !strings.HasPrefix(err.Error(), "timed out after 300ms (--timeout): ") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("failure before the deadline, leftovers stopped after it", func(t *testing.T) {
		var scope commandScope
		// the leftover ignores SIGTERM (its trap is set before the command
		// exits: ready), so stopping it takes the grace period (1.5s), which
		// ends after the 1s deadline
		ready := t.TempDir() + "/ready"
		start := time.Now()
		err := runStep(t, &scope, time.Second, 1500*time.Millisecond,
			`sh -c 'trap "" TERM; : > "$0"; sleep 30' '`+ready+`' & `+awaitReadySh(ready)+`; exit 3`)
		if err == nil || strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "exit status 3") {
			t.Fatalf("got %v", err)
		}
		if el := time.Since(start); el < 1500*time.Millisecond {
			t.Fatalf("precondition: stopping the leftover took %v, not the grace period (the scenario did not happen)", el)
		}
	})

	t.Run("deadline over before the start (--timeout 0sec)", func(t *testing.T) {
		var scope commandScope
		err := runStep(t, &scope, 0, time.Second, "true")
		if err == nil || !strings.HasPrefix(err.Error(), "timed out after 0s (--timeout): ") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("shutdown is not a timeout", func(t *testing.T) {
		var scope commandScope
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = scope.abort(5 * time.Second)
		}()
		err := runStep(t, &scope, time.Hour, time.Second, "sleep 30")
		if err == nil || strings.Contains(err.Error(), "timed out") {
			t.Fatalf("got %v", err)
		}
	})
}
