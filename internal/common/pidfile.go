package common

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A pid file holds the decimal pid of the enma process that wrote it, nothing
// else. The process that writes it owns it: CreatePidFile writes it whole (a
// reader never sees a partial file) and DeletePidFile removes it only while it
// still holds this process's pid, so a file another enma has taken over since
// is left alone.
//
// Limits: a process killed with SIGKILL (or a crash, or a power loss) cannot
// remove its pid file; the next start replaces it. The pid in a stale file may
// have been reused by an unrelated process, so a live pid does not prove that
// enma still runs. DeletePidFile reads, then removes: another enma replacing
// the file in between (a very short window) loses it.

// CreatePidFile writes this process's pid to path, creating parent directories.
// An existing file is replaced; when it names another process that is still
// alive, a warning says so (two enma processes may share the pid file).
func CreatePidFile(path string) error {
	pid := os.Getpid()
	if old, ok := readPid(path); ok && old != pid && processAlive(old) {
		log.Printf("⚠️  pid file %s names process %d, which is still running; replacing it", path, old)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("pid file %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("pid file %s: %w", path, err)
	}
	_, werr := tmp.WriteString(strconv.Itoa(pid))
	cerr := tmp.Close()
	if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), 0644)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("pid file %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("pid file %s: %w", path, err)
	}
	return nil
}

// DeletePidFile removes path if it holds exactly this process's pid. A missing
// file or one holding another pid is not an error: it is not ours to remove.
func DeletePidFile(path string) error {
	pid, ok := readPid(path)
	if !ok || pid != os.Getpid() {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pid file %s: %w", path, err)
	}
	return nil
}

// readPid returns the pid held by the pid file at path.
func readPid(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
