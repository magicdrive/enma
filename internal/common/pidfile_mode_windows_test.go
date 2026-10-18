package common_test

import (
	"os"
	"testing"

	"github.com/magicdrive/enma/internal/common"
)

// checkPidFileMode: Windows has no Unix modes. The pid file gets the ACL of
// its directory (SECURITY.md), and os.FileMode shows only the read-only
// attribute: 0666 without it, 0444 with it. The file must not be read-only,
// or the next start could not replace it and shutdown could not remove it.
func checkPidFileMode(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o666 {
		t.Fatalf("pid file mode %v, want -rw-rw-rw- (not read-only)", st.Mode())
	}
	if err := common.CreatePidFile(path); err != nil { // the next start replaces it
		t.Fatalf("pid file cannot be replaced: %v", err)
	}
}
