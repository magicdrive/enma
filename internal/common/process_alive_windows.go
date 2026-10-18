//go:build windows

package common

import "os"

// processAlive reports whether a process with this pid exists (FindProcess
// opens the process on Windows and fails when there is none). Approximate: an
// exited process someone still holds a handle to counts as alive, one enma may
// not open (elevated) as gone. It only decides whether a warning is printed.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
