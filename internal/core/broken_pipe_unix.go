//go:build !windows

package core

import (
	"os"
	"os/signal"
	"syscall"
)

// keepRunningOnBrokenPipe stops SIGPIPE from killing enma when the reader of its
// stdout / stderr goes away (e.g. `enma watch ... | head`): the runtime would
// exit at once and leave the daemon and running commands (each in its own
// process group) and the pid file behind. With SIGPIPE caught, such writes just
// fail and enma goes on. Caught, not ignored: an ignored SIGPIPE would be
// inherited by the commands enma starts.
func keepRunningOnBrokenPipe() {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}
