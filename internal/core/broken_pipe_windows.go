//go:build windows

package core

// keepRunningOnBrokenPipe: Windows has no SIGPIPE; a failed write to a closed
// pipe is an error and nothing more.
func keepRunningOnBrokenPipe() {}
