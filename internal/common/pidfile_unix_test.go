//go:build !windows

package common_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/enma/internal/common"
)

// The pid file is replaced, never written through: a symlink at the pid path
// (planted in a shared directory) does not make enma write to its target.
func TestPidFile_SymlinkIsReplacedNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "enma.pid")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := common.CreatePidFile(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatalf("the symlink target was written: %q", b)
	}
	if st, err := os.Lstat(path); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the pid path is still a symlink (%v)", err)
	}
}
