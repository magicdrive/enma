//go:build linux

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const prSetChildSubreaper = 36 // PR_SET_CHILD_SUBREAPER (linux/prctl.h)

// PRA-01: when enma is pid 1 (a container without an init) or a subreaper,
// the members of a command's group that outlive the command are re-parented to
// enma and stay zombies once they exit. A group with a zombie still exists for
// kill(-pgid, 0), so stopping it must collect the zombies, or every stop times
// out (and a daemon is never restarted). The scenario runs in a child test
// process that is a subreaper (setting it in this process would affect the
// other tests).
func TestReap_SubreaperZombiesDoNotKeepTheGroupAlive(t *testing.T) {
	if os.Getenv("ENMA_REAP_HELPER") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReap_SubreaperZombiesDoNotKeepTheGroupAlive$", "-test.v")
		cmd.Env = append(os.Environ(), "ENMA_REAP_HELPER=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper: %v\n%s", err, out)
		}
		return
	}
	if _, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		t.Fatalf("cannot become a subreaper: %v", errno)
	}

	t.Run("leftovers of a command", func(t *testing.T) {
		// the leftover exits by itself shortly after the command: it becomes our zombie
		cmd := exec.Command("sh", "-c", "sleep 0.3 & exit 0")
		setProcessGroup(cmd)
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		var scope commandScope
		start := time.Now()
		if err := stopLeftovers(cmd, cmd.Process.Pid, &scope, 2*time.Second, time.Second, "test"); err != nil {
			t.Fatalf("stopLeftovers: %v (zombies kept the group alive)", err)
		}
		if el := time.Since(start); el > 1500*time.Millisecond {
			t.Fatalf("stopLeftovers took %v: the group did not look gone once the leftover exited", el)
		}
	})

	t.Run("daemon", func(t *testing.T) {
		// a background member that exits on SIGTERM becomes our zombie; the
		// daemon goes on once the member runs (ready)
		ready := filepath.Join(t.TempDir(), "ready")
		p, err := startManaged(exec.Command("sh", "-c", `(sh -c ': > "$0"; exec sleep 30' '`+ready+`' &); `+
			awaitReadySh(ready)+`; exec sleep 31`))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for _, err := os.Stat(ready); err != nil; _, err = os.Stat(ready) {
			if time.Now().After(deadline) {
				t.Fatal("the daemon's member did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := p.stop(syscall.SIGTERM, 2*time.Second, time.Second); err != nil {
			t.Fatalf("daemon stop: %v (zombies kept the group alive)", err)
		}
		if !p.gone() {
			t.Fatal("the daemon's group is not gone after the stop")
		}
	})
}
