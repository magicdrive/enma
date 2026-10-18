package core

import (
	"errors"
	"os/exec"
	"time"
)

// Test hooks for the unexported process lifecycle (compiled only into tests).

var ErrShuttingDownForTest = errShuttingDown

// AdaptShellForTest prepares a `sh -c` command as enma does for its commands.
func AdaptShellForTest(cmd *exec.Cmd) { adaptShell(cmd) }

// EnvShellForTest applies the Windows way of passing a command (envShell) on
// any platform.
func EnvShellForTest(cmd *exec.Cmd) { envShell(cmd) }

func StartDaemonForTest(r *HotloadRunner) error { return r.startDaemon() }
func StopDaemonForTest(r *HotloadRunner) error  { return r.stopDaemon() }
func SetStopTimeoutsForTest(r *HotloadRunner, grace, kill time.Duration) {
	r.StopGrace, r.KillWait = grace, kill
}
func SetWatchStopTimeoutsForTest(w *WatchRunner, grace, kill time.Duration) {
	w.StopGrace, w.KillWait = grace, kill
}

// HandleChangeForTest runs the real change handler: build sequence, stop, start.
func HandleChangeForTest(r *HotloadRunner, path string) { r.handleChangeDirect(path) }

func ShutdownForTest(r *HotloadRunner) error    { return r.shutdown() }
func ForceKillForTest(r *HotloadRunner)         { r.forceKill() }
func WatchShutdownForTest(w *WatchRunner) error { return w.shutdown() }
func WatchForceKillForTest(w *WatchRunner)      { w.forceKill() }

// WatchHandleChangeForTest runs the real change handler: command sequence with retries.
func WatchHandleChangeForTest(w *WatchRunner, path string) { w.handleChangeDirect(path) }

func RunBuildSequenceForTest(r *HotloadRunner, path string) error {
	if !r.RunBuildSequence(0, path) {
		return errors.New("build sequence failed")
	}
	return nil
}

func WatchRunSequenceForTest(w *WatchRunner, path string) error {
	if !w.RunBuildSequence(0, path) {
		return errors.New("command sequence failed")
	}
	return nil
}

// RunStartAtOnceForTest runs the runner's start-at-once step (once per runner).
func RunStartAtOnceForTest(r *HotloadRunner) error    { return r.runStartAtOnce() }
func WatchRunStartAtOnceForTest(w *WatchRunner) error { return w.runStartAtOnce() }

// StartAtOnceInStartForTest is Start's start-at-once step (pid file handling on failure).
func StartAtOnceInStartForTest(run func() error, pidPath string) error {
	return startAtOnceInStart(run, pidPath)
}

// Scheduler test hooks.
type VirtualClock = virtualClock

func NewVirtualClockForTest(wall time.Time) *VirtualClock          { return newVirtualClock(wall) }
func (c *virtualClock) NextDeadlineForTest() (time.Duration, bool) { return c.nextDeadline() }
func SetSchedulerOnWaitForTest(r *SchedulerRunner, f func())       { r.onWait = f }
func SchedulerShutdownForTest(r *SchedulerRunner) error            { return r.shutdown() }
func SetSchedulerBeforeAbortForTest(r *SchedulerRunner, f func())  { r.beforeAbort = f }
func SchedulerPendingResultsForTest(r *SchedulerRunner) int        { return len(r.results) }
func SchedulerForceKillForTest(r *SchedulerRunner)                 { r.forceKill() }
func FormatSchedulerEventForTest(ev SchedulerEvent, loc *time.Location) string {
	return formatSchedulerEvent(ev, loc)
}
