package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A writer that fails (stdout gone) does not stop the others (the log file).
func TestTeeWriter_KeepsWritingPastAFailure(t *testing.T) {
	var a, b bytes.Buffer
	w := teeWriter{&a, failingWriter{}, &b}
	n, err := w.Write([]byte("line\n"))
	if n != 5 || err != nil {
		t.Fatalf("Write = %d, %v; want 5, nil (logging never fails)", n, err)
	}
	if a.String() != "line\n" || b.String() != "line\n" {
		t.Fatalf("a=%q b=%q", a.String(), b.String())
	}
}

// A new log file is created for its owner only; an existing one keeps its mode.
func TestOpenLogFile_Mode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Log("no Unix permissions on Windows: only creation is checked")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "enma.log")
	f, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("new log file mode %v, want no group/other access", st.Mode().Perm())
	}
	shared := filepath.Join(dir, "shared.log")
	if err := os.WriteFile(shared, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err = openLogFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if st, _ := os.Stat(shared); runtime.GOOS != "windows" && st.Mode().Perm() != 0o644 {
		t.Fatalf("existing log file mode changed to %v", st.Mode().Perm())
	}
}
