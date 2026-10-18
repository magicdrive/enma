//go:build !windows

package common_test

import (
	"os"
	"testing"
)

// checkPidFileMode: on Unix the pid file is 0644 whatever the umask: only its
// owner can write it.
func checkPidFileMode(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("pid file mode %v, want -rw-r--r--", st.Mode())
	}
}
