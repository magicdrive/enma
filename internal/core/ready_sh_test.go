//go:build !windows

package core_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// awaitReadySh is a shell fragment that waits until the file ready exists. A
// helper process creates it once it is set up (e.g. its SIGTERM trap is in
// place), and the command goes on only then: the test waits for that report,
// not for a fixed time, so it does not depend on the speed of the machine.
// The bound (about 10s) only keeps a broken helper from hanging the test.
func awaitReadySh(ready string) string {
	return `i=0; while [ ! -e '` + ready + `' ] && [ $i -lt 1000 ]; do sleep 0.01; i=$((i+1)); done; ` +
		`[ -e '` + ready + `' ] || echo "awaitReadySh: ` + ready + ` did not appear (the helper process is not ready)" >&2`
}

// waitExeced waits until process pid runs the program name (its fork has
// finished exec). A background child of a shell that traps SIGTERM with a
// handler can lose a SIGTERM that reaches it after the fork but before the
// exec (it then still has the parent's handler; seen with bash on macOS and
// with dash on Linux). It is then stopped only by SIGKILL after the grace
// period, so a test that checks how a command reacts to SIGTERM signals it
// only once it really runs name. (A helper that resets the trap itself,
// `trap - TERM`, before it reports ready does not have the window.)
func waitExeced(t *testing.T, pid int, name string) {
	t.Helper()
	if !waitFor(t, 5*time.Second, func() bool {
		out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
		return err == nil && filepath.Base(strings.TrimSpace(string(out))) == name
	}) {
		t.Fatalf("process %d does not run %s", pid, name)
	}
}
