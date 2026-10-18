//go:build !windows

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// H6B-02: a step stopped by --timeout says so, instead of only
// "signal: killed".
func TestE2E_TimeoutIsNamed(t *testing.T) {
	const want = "timed out after 1s (--timeout)"

	t.Run("watch", func(t *testing.T) {
		dir := isolated(t, "")
		src := filepath.Join(dir, "src")
		if err := os.Mkdir(src, 0o755); err != nil {
			t.Fatal(err)
		}
		p := startEnma(t, dir, nil, "watch", "--watch-dir", "src", "--command", "sleep 30", "--timeout", "1sec")
		if !waitFor(10*time.Second, monitoring(p)) {
			t.Fatalf("watch did not start:\n%s", p.out.String())
		}
		if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !waitFor(15*time.Second, func() bool { return strings.Contains(p.out.String(), want) }) {
			t.Fatalf("no %q:\n%s", want, p.out.String())
		}
		p.interrupt(t)
	})

	t.Run("hotload", func(t *testing.T) {
		dir := isolated(t, "")
		p := startEnma(t, dir, nil, "hotload", "--build", "sleep 30", "--daemon", "sleep 60", "--timeout", "1sec")
		if !waitFor(15*time.Second, func() bool { return strings.Contains(p.out.String(), want) }) {
			t.Fatalf("no %q:\n%s", want, p.out.String())
		}
		p.interrupt(t)
	})

	t.Run("shutdown is not a timeout", func(t *testing.T) {
		dir := isolated(t, "")
		p := startEnma(t, dir, nil, "hotload", "--build", "sleep 30", "--daemon", "sleep 60", "--timeout", "60sec")
		if !waitFor(10*time.Second, func() bool { return strings.Contains(p.out.String(), "Watching") }) {
			t.Fatalf("hotload did not start:\n%s", p.out.String())
		}
		time.Sleep(300 * time.Millisecond) // the first build is running
		p.interrupt(t)
		if strings.Contains(p.out.String(), "timed out") {
			t.Fatalf("a build stopped by Ctrl-C reported a timeout:\n%s", p.out.String())
		}
	})
}
