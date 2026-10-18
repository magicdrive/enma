//go:build !windows

package core_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
	"github.com/magicdrive/enma/internal/cronspec"
)

// Scheduler invariants:
//   C1 no run at start, not even when started at a fire second; no catch-up of
//      fire times before the start, jumped over by the clock, or (DST) skipped;
//   C2 a civil time repeated after the clocks go back fires once;
//   C3 retries: exactly `retries` more attempts after failures (exit, start
//      failure, timeout), each `retry_delay` after the previous one ended;
//      none after success or shutdown;
//   C4 a job is busy through its whole run (retry waits included): fires meanwhile
//      are skipped (job_busy), never queued;
//   C5 at most max_concurrent attempts run; a retry wait holds no slot; a fire or
//      retry without a free slot is skipped (capacity_unavailable /
//      retry_capacity_unavailable), never queued;
//   C6 at the same instant: due retries first, then fires; each in config order;
//   C7 shutdown stops all attempts together (SIGTERM, grace, SIGKILL), drops retry
//      waits and starts nothing more; no process is left.

var t0 = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

type harness struct {
	t      *testing.T
	r      *core.SchedulerRunner
	clock  *core.VirtualClock
	waits  chan struct{}
	mu     sync.Mutex
	events []core.SchedulerEvent
	served chan error
	// quiesce: advance lets every started attempt finish before moving time
	// on (attempts take no virtual time). Off for tests whose jobs keep running.
	quiesce bool
}

type jobSpec struct {
	name, cron, command string
	timeout             time.Duration
	retries             int
	retryDelay          time.Duration
	dir                 string
}

func newHarness(t *testing.T, start time.Time, loc *time.Location, maxConcurrent int, specs ...jobSpec) *harness {
	t.Helper()
	return newHarnessWith(t, start, loc, maxConcurrent, nil, specs...)
}

// newHarnessWith lets setup adjust the runner before it starts.
func newHarnessWith(t *testing.T, start time.Time, loc *time.Location, maxConcurrent int, setup func(*core.SchedulerRunner), specs ...jobSpec) *harness {
	t.Helper()
	dir := t.TempDir()
	// run in a temporary directory: a job that ignores its working directory
	// (a broken build) writes there, not into the source tree
	t.Chdir(t.TempDir())
	var jobs []option.SchedulerJob
	for _, s := range specs {
		sched, err := cronspec.Parse(s.cron)
		if err != nil {
			t.Fatal(err)
		}
		wd := s.dir
		if wd == "" {
			wd = dir
		}
		delay := s.retryDelay
		if delay == 0 {
			delay = 10 * time.Second
		}
		jobs = append(jobs, option.SchedulerJob{Name: s.name, CronExpr: s.cron, Cron: sched, Command: s.command,
			WorkingDir: wd, Timeout: s.timeout, Retries: s.retries, RetryDelay: delay})
	}
	if loc == nil {
		loc = time.UTC
	}
	opt := &option.SchedulerOption{Jobs: jobs, Location: loc, MaxConcurrent: maxConcurrent}
	h := &harness{t: t, clock: core.NewVirtualClockForTest(start), waits: make(chan struct{}, 1), served: make(chan error, 1), quiesce: true}
	h.r = core.NewSchedulerRunner(opt)
	h.r.Clock = h.clock
	h.r.StopGrace, h.r.KillWait = testGrace, testKill
	h.r.OnEvent = func(ev core.SchedulerEvent) {
		h.mu.Lock()
		h.events = append(h.events, ev)
		h.mu.Unlock()
	}
	core.SetSchedulerOnWaitForTest(h.r, func() {
		select {
		case h.waits <- struct{}{}:
		default:
		}
	})
	if setup != nil {
		setup(h.r)
	}
	go func() { h.served <- h.r.Serve() }()
	h.waitLoop()
	t.Cleanup(func() { _ = core.SchedulerShutdownForTest(h.r) })
	return h
}

// waitLoop waits until the loop is waiting again.
func (h *harness) waitLoop() {
	h.t.Helper()
	select {
	case <-h.waits:
	case <-time.After(10 * time.Second):
		h.t.Fatal("scheduler loop did not come back to wait")
	}
}

// advance moves the virtual clock by d, stopping at every deadline of the
// loop's timer and waiting for the loop to handle it, so the loop sees every
// instant it asked for (as with a real clock).
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	target := h.clock.Mono() + d
	for {
		dl := h.pendingDeadline()
		cur := h.clock.Mono()
		if cur >= target { // something else (a job) moved the clock past it
			return
		}
		if dl > target {
			h.clock.Advance(target - cur)
			return
		}
		select { // forget a stale wake-up signal
		case <-h.waits:
		default:
		}
		h.clock.Advance(dl - cur)
		h.waitLoop()
		if h.quiesce {
			h.waitEvents("running attempts to finish", func(evs []core.SchedulerEvent) bool { return running(evs) == 0 })
		}
	}
}

// running counts attempts started and not finished.
func running(evs []core.SchedulerEvent) int {
	n := 0
	for _, ev := range evs {
		switch ev.Kind {
		case core.EventJobStart, core.EventRetryStart:
			n++
		case core.EventJobSuccess, core.EventRetrySuccess, core.EventJobFailed, core.EventJobTimeout, core.EventJobCanceled:
			n--
		}
	}
	return n
}

// last returns the last event of kind.
func last(t *testing.T, evs []core.SchedulerEvent, kind string) core.SchedulerEvent {
	t.Helper()
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == kind {
			return evs[i]
		}
	}
	t.Fatalf("no %s event", kind)
	return core.SchedulerEvent{}
}

// pendingDeadline waits until the loop has a timer pending (it is waiting) and returns its deadline.
func (h *harness) pendingDeadline() time.Duration {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if dl, ok := h.clock.NextDeadlineForTest(); ok {
			return dl
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the scheduler loop has no timer pending")
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) snapshot() []core.SchedulerEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]core.SchedulerEvent(nil), h.events...)
}

// waitEvents waits (in real time) until pred holds for the events.
func (h *harness) waitEvents(what string, pred func([]core.SchedulerEvent) bool) []core.SchedulerEvent {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		evs := h.snapshot()
		if pred(evs) {
			return evs
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s; events:\n%s", what, dump(evs))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitCount waits until there are n events of kind (for job, "" = any), then lets the loop settle.
func (h *harness) waitCount(kind, job string, n int) []core.SchedulerEvent {
	h.t.Helper()
	evs := h.waitEvents(fmt.Sprintf("%d %s %s", n, kind, job), func(evs []core.SchedulerEvent) bool {
		return count(evs, kind, job) >= n
	})
	return evs
}

func count(evs []core.SchedulerEvent, kind, job string) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == kind && (job == "" || ev.Job == job) {
			n++
		}
	}
	return n
}

func dump(evs []core.SchedulerEvent) string {
	var b strings.Builder
	for _, ev := range evs {
		fmt.Fprintln(&b, core.FormatSchedulerEventForTest(ev, time.UTC))
	}
	return b.String()
}

// kinds lists "kind:job[:reason]" of the job events, in order.
func kinds(evs []core.SchedulerEvent) []string {
	var out []string
	for _, ev := range evs {
		if ev.Job == "" {
			continue
		}
		s := ev.Kind + ":" + ev.Job
		if ev.Kind == core.EventSkip {
			s += ":" + ev.Reason
		}
		out = append(out, s)
	}
	return out
}

func (h *harness) settleQuiet(d time.Duration) []core.SchedulerEvent {
	time.Sleep(d)
	return h.snapshot()
}

// syncLog collects log output written from several goroutines.
type syncLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// --- S1: start, single job, time zone ---------------------------------------

func TestScheduler_NoRunAtStart_EvenOnAFireSecond(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	if evs := h.settleQuiet(200 * time.Millisecond); count(evs, core.EventJobStart, "") != 0 {
		t.Fatalf("C1 violated: ran at start:\n%s", dump(evs))
	}
	h.advance(time.Second)
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if got := evs[1].Scheduled; !got.Equal(t0.Add(time.Second)) {
		t.Fatalf("first fire scheduled %s, want %s", got, t0.Add(time.Second))
	}
}

func TestScheduler_StartJustBeforeAFire(t *testing.T) {
	h := newHarness(t, t0.Add(-500*time.Millisecond), nil, 4, jobSpec{name: "a", cron: "0 0 10 * * *", command: "true"})
	h.advance(400 * time.Millisecond)
	if n := count(h.snapshot(), core.EventJobStart, ""); n != 0 {
		t.Fatal("fired early")
	}
	h.advance(100 * time.Millisecond)
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if ev := evs[1]; ev.Kind != core.EventJobStart || !ev.Scheduled.Equal(t0) || ev.Attempt != 1 {
		t.Fatalf("got %+v", ev)
	}
}

func TestScheduler_JobRunsInItsWorkingDirectory(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "pwd -P > wd", dir: dir})
	cwd, _ := os.Getwd()
	h.advance(time.Second)
	h.waitCount(core.EventJobSuccess, "a", 1)
	if b, _ := os.ReadFile(filepath.Join(dir, "wd")); strings.TrimSpace(string(b)) != dir {
		t.Fatalf("ran in %q, want %q", b, dir)
	}
	if now, _ := os.Getwd(); now != cwd {
		t.Fatalf("the process directory changed to %q", now)
	}
}

func TestScheduler_TimeZone(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	// 2026-10-16 00:59:59Z is 09:59:59 in Tokyo
	h := newHarness(t, time.Date(2026, 10, 16, 0, 59, 59, 0, time.UTC), tokyo, 4, jobSpec{name: "a", cron: "0 0 10 * * *", command: "true"})
	h.advance(time.Second)
	evs := h.waitCount(core.EventJobStart, "a", 1)
	if want := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC); !evs[1].Scheduled.Equal(want) {
		t.Fatalf("scheduled %s, want %s", evs[1].Scheduled, want)
	}
}

func TestScheduler_ShutdownStopsFiring(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.advance(time.Second)
	h.waitCount(core.EventJobSuccess, "a", 1)
	if err := core.SchedulerShutdownForTest(h.r); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-h.served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	h.clock.Advance(5 * time.Second)
	evs := h.settleQuiet(200 * time.Millisecond)
	if count(evs, core.EventJobStart, "") != 1 || count(evs, core.EventSchedulerStop, "") != 1 {
		t.Fatalf("after shutdown:\n%s", dump(evs))
	}
}

// --- S2: results, timeout, retries ------------------------------------------

func TestScheduler_ExitCodesAndNoRetryByDefault(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "exit 3"})
	h.advance(time.Second)
	evs := h.waitCount(core.EventJobFailed, "a", 1)
	ev := last(t, evs, core.EventJobFailed)
	if ev.ExitCode != 3 || ev.Reason != core.FailExit || ev.Attempt != 1 {
		t.Fatalf("got %+v", ev)
	}
	for i := 2; i <= 3; i++ { // new fires run, but no retry
		h.advance(time.Second)
		evs = h.waitCount(core.EventJobFailed, "a", i)
	}
	if count(evs, core.EventRetryScheduled, "") != 0 || count(evs, core.EventRetryStart, "") != 0 {
		t.Fatalf("retried with retries=0:\n%s", dump(evs))
	}
}

func TestScheduler_RetriesAfterExitFailure_FixedDelay(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: "exit 1", retries: 3, retryDelay: 10 * time.Second})
	h.advance(time.Hour)
	for attempt := 1; attempt <= 4; attempt++ {
		h.waitCount(core.EventJobFailed, "a", attempt)
		if attempt == 4 {
			break
		}
		evs := h.waitCount(core.EventRetryScheduled, "a", attempt)
		if rs := last(t, evs, core.EventRetryScheduled); rs.Attempt != attempt+1 {
			t.Fatalf("retry_scheduled %+v", rs)
		}
		h.advance(9 * time.Second)
		if n := count(h.snapshot(), core.EventRetryStart, "a"); n != attempt-1 {
			t.Fatalf("C3 violated: retry %d started before retry_delay", attempt+1)
		}
		h.advance(time.Second)
		h.waitCount(core.EventRetryStart, "a", attempt)
	}
	evs := h.waitCount(core.EventRetriesExhausted, "a", 1)
	if got := count(evs, core.EventJobFailed, "a"); got != 4 {
		t.Fatalf("C3 violated: %d attempts for retries=3", got)
	}
	// the run is over: the next fire starts a new run
	h.advance(time.Hour)
	h.waitCount(core.EventJobStart, "a", 2)
}

func TestScheduler_RetrySucceeds(t *testing.T) {
	cmd := `if [ -f done ]; then exit 0; fi; touch done; exit 1`
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: cmd, retries: 2, retryDelay: 5 * time.Second})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.advance(5 * time.Second)
	evs := h.waitCount(core.EventRetrySuccess, "a", 1)
	if count(evs, core.EventRetriesExhausted, "") != 0 || count(evs, core.EventRetryStart, "a") != 1 {
		t.Fatalf("C3: %s", dump(evs))
	}
}

func TestScheduler_TimeoutKillsAndRetries(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	cmd := `sleep 1000 & echo $! >> '` + pids + `'; echo $$ >> '` + pids + `'; wait`
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: cmd, timeout: 200 * time.Millisecond, retries: 1, retryDelay: time.Second})
	h.advance(time.Hour)
	evs := h.waitCount(core.EventJobTimeout, "a", 1)
	if ev := last(t, evs, core.EventJobTimeout); ev.Reason != core.FailTimeout {
		t.Fatalf("got %+v", ev)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("process %d survived the timeout", pid)
		}
	}
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.advance(time.Second)
	h.waitCount(core.EventJobTimeout, "a", 2)
	h.waitCount(core.EventRetriesExhausted, "a", 1)
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("process %d survived the second timeout", pid)
		}
	}
}

func TestScheduler_StartFailureIsRetried(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: "true", retries: 1, retryDelay: time.Second})
	h.r.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "no-such-shell"), args...)
	}
	h.advance(time.Hour)
	evs := h.waitCount(core.EventJobFailed, "a", 1)
	if ev := last(t, evs, core.EventJobFailed); ev.Reason != core.FailStart || ev.ExitCode != -1 {
		t.Fatalf("got %+v", ev)
	}
	h.advance(time.Second)
	h.waitCount(core.EventJobFailed, "a", 2)
	h.waitCount(core.EventRetriesExhausted, "a", 1)
}

func TestScheduler_FiresDuringRetryWaitAreSkipped(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second})
	h.advance(time.Second)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.advance(3 * time.Second)
	evs := h.snapshot()
	if n := count(evs, core.EventSkip, "a"); n != 3 {
		t.Fatalf("C4: want 3 job_busy skips during the retry wait:\n%s", dump(evs))
	}
	for _, ev := range evs {
		if ev.Kind == core.EventSkip && ev.Reason != core.SkipJobBusy {
			t.Fatalf("skip reason %q", ev.Reason)
		}
	}
	if count(evs, core.EventJobStart, "a") != 1 {
		t.Fatal("C4 violated: a fire started during the retry wait")
	}
}

func TestScheduler_RetryWaitFreesTheSlot(t *testing.T) {
	// max_concurrent = 1: while a waits for its retry, b may run
	h := newHarness(t, t0, nil, 1,
		jobSpec{name: "a", cron: "0 0 11 * * *", command: "exit 1", retries: 1, retryDelay: 30 * time.Second},
		jobSpec{name: "b", cron: "5 0 11 * * *", command: "true"})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.advance(5 * time.Second)
	h.waitCount(core.EventJobSuccess, "b", 1)
	h.advance(25 * time.Second)
	evs := h.waitCount(core.EventRetriesExhausted, "a", 1)
	if count(evs, core.EventRetryStart, "a") != 1 || count(evs, core.EventSkip, "") != 0 {
		t.Fatalf("C5:\n%s", dump(evs))
	}
}

func TestScheduler_RetryWithoutSlotIsDropped(t *testing.T) {
	// max_concurrent = 1: b holds the slot when a's retry is due
	h := newHarness(t, t0, nil, 1,
		jobSpec{name: "a", cron: "0 0 11 * * *", command: "exit 1", retries: 2, retryDelay: 10 * time.Second},
		jobSpec{name: "b", cron: "5 0 11 * * *", command: "sleep 30"})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.quiesce = false // b keeps running
	h.advance(5 * time.Second)
	h.waitCount(core.EventJobStart, "b", 1)
	h.advance(5 * time.Second)
	evs := h.waitCount(core.EventRetryCapacityUnavailble, "a", 1)
	if count(evs, core.EventRetryStart, "a") != 0 || count(evs, core.EventRetryScheduled, "a") != 1 {
		t.Fatalf("C5 violated:\n%s", dump(evs))
	}
	// a's run is over (not busy): its next fire is not skipped as busy
	_ = core.SchedulerShutdownForTest(h.r)
}

func TestScheduler_RetryBeforeFireAtTheSameInstant(t *testing.T) {
	// max_concurrent = 1. a's retry and b's fire are due at the same instant:
	// the retry gets the slot, the fire is skipped.
	h := newHarness(t, t0, nil, 1,
		jobSpec{name: "a", cron: "0 0 11 * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second},
		jobSpec{name: "b", cron: "10 0 11 * * *", command: "true"})
	h.advance(time.Hour)
	evs := h.waitCount(core.EventRetryScheduled, "a", 1)
	if at := last(t, evs, core.EventRetryScheduled).RetryAt; !at.Equal(t0.Add(time.Hour + 10*time.Second)) {
		t.Fatalf("retry at %s: the attempt took virtual time", at)
	}
	h.quiesce = false // the retry fails at once; b's fire is decided in the same wake-up
	h.advance(10 * time.Second)
	evs = h.waitCount(core.EventSkip, "b", 1)
	got := kinds(evs)
	want := []string{"job_start:a", "job_failed:a", "retry_scheduled:a", "retry_start:a", "skip:b:capacity_unavailable"}
	if strings.Join(got[:5], " ") != strings.Join(want, " ") {
		t.Fatalf("C6 violated:\n got %v\nwant %v", got, want)
	}
}

func TestScheduler_ShutdownDuringRetryWait(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: "exit 1", retries: 3, retryDelay: 10 * time.Second})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	if err := core.SchedulerShutdownForTest(h.r); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(time.Minute)
	evs := h.settleQuiet(200 * time.Millisecond)
	if count(evs, core.EventRetryStart, "") != 0 {
		t.Fatalf("C7 violated: retried after shutdown:\n%s", dump(evs))
	}
}

// --- S2: daylight saving and clock changes ----------------------------------

func TestScheduler_DSTGapIsNotRun(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 2026-03-08: 02:00 EST -> 03:00 EDT, so 02:30 does not exist that day
	start := time.Date(2026, 3, 8, 1, 0, 0, 0, ny)
	h := newHarness(t, start, ny, 4, jobSpec{name: "a", cron: "0 30 2 * * *", command: "true"})
	h.advance(25 * time.Hour)
	evs := h.waitCount(core.EventJobStart, "a", 1)
	want := time.Date(2026, 3, 9, 2, 30, 0, 0, ny)
	if got := evs[1].Scheduled; !got.Equal(want) || count(evs, core.EventJobStart, "") != 1 {
		t.Fatalf("C1: first run scheduled %s, want %s (none on the gap day):\n%s", got.In(ny), want, dump(evs))
	}
}

func TestScheduler_DSTOverlapRunsOnce(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 2026-11-01: 02:00 EDT -> 01:00 EST, so 01:00-01:59 happens twice
	start := time.Date(2026, 11, 1, 0, 59, 0, 0, ny) // EDT
	h := newHarness(t, start, ny, 4,
		jobSpec{name: "daily", cron: "0 30 1 * * *", command: "true"},
		jobSpec{name: "minutely", cron: "0 * 1 * * *", command: "true"})
	h.advance(3 * time.Hour)
	time.Sleep(200 * time.Millisecond)
	evs := h.waitCount(core.EventJobSuccess, "minutely", 60)
	if n := count(evs, core.EventJobStart, "daily"); n != 1 {
		t.Fatalf("C2 violated: 01:30 ran %d times", n)
	}
	if n := count(evs, core.EventJobStart, "minutely"); n != 60 {
		t.Fatalf("C2 violated: hour 1 minutes ran %d times, want 60", n)
	}
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart && ev.Job == "daily" {
			if _, off := ev.Scheduled.In(ny).Zone(); off != -4*3600 {
				t.Fatalf("ran at the second 01:30 (offset %d), want the first", off)
			}
		}
	}
}

func TestScheduler_ClockSetForwardIsNotCaughtUp(t *testing.T) {
	h := newHarness(t, t0.Add(500*time.Millisecond), nil, 4, jobSpec{name: "a", cron: "0 * * * * *", command: "true"})
	h.clock.StepWall(3 * time.Hour) // the system clock is set forward
	h.advance(core.DefaultSchedulerMaxSleep)
	evs := h.settleQuiet(200 * time.Millisecond)
	if count(evs, core.EventJobStart, "") != 0 {
		t.Fatalf("C1 violated: caught up after the clock jump:\n%s", dump(evs))
	}
	// one event for the whole jump, from the first fire time jumped over
	if n := count(evs, core.EventSkip, "a"); n != 1 {
		t.Fatalf("want one clock_jump skip, got %d:\n%s", n, dump(evs))
	}
	if ev := last(t, evs, core.EventSkip); ev.Reason != core.SkipClockJump || !ev.Scheduled.Equal(t0.Add(time.Minute)) ||
		!ev.Until.Equal(t0.Add(3*time.Hour+500*time.Millisecond)) { // the jump is placed when the sleep began
		t.Fatalf("got %+v", ev)
	}
	// the next minute after the new time runs
	h.advance(time.Minute)
	h.waitCount(core.EventJobStart, "a", 1)
}

func TestScheduler_ClockSetBackDoesNotRepeat(t *testing.T) {
	h := newHarness(t, t0.Add(-500*time.Millisecond), nil, 4, jobSpec{name: "a", cron: "0 * * * * *", command: "true"})
	h.advance(500 * time.Millisecond)
	h.waitCount(core.EventJobSuccess, "a", 1) // 10:00:00 ran
	h.clock.StepWall(-30 * time.Minute)       // back to 09:30:00
	h.advance(31 * time.Minute)               // wall 10:01:00
	evs := h.waitCount(core.EventJobStart, "a", 2)
	starts := 0
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart {
			starts++
			if starts == 2 && !ev.Scheduled.Equal(t0.Add(time.Minute)) {
				t.Fatalf("second run scheduled %s, want 10:01:00 (09:31..10:00 must not run again)", ev.Scheduled)
			}
		}
	}
}

func TestScheduler_StoppedTimeIsNotCaughtUp(t *testing.T) {
	// a scheduler started after fire times (as after a restart) does not run them
	h := newHarness(t, t0.Add(90*time.Minute), nil, 4, jobSpec{name: "a", cron: "0 0 * * * *", command: "true"})
	h.advance(10 * time.Minute)
	if evs := h.settleQuiet(100 * time.Millisecond); count(evs, core.EventJobStart, "") != 0 || count(evs, core.EventSkip, "") != 0 {
		t.Fatalf("C1:\n%s", dump(evs))
	}
	h.advance(20 * time.Minute)
	evs := h.waitCount(core.EventJobStart, "a", 1)
	if !evs[1].Scheduled.Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("scheduled %s", evs[1].Scheduled)
	}
}

// --- S3: several jobs, max_concurrent, order --------------------------------

func TestScheduler_SameInstantFiresInConfigOrder(t *testing.T) {
	for _, mc := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("max_concurrent=%d", mc), func(t *testing.T) {
			h := newHarness(t, t0, nil, mc,
				jobSpec{name: "c", cron: "0 0 * * * *", command: "sleep 30"},
				jobSpec{name: "a", cron: "0 0 * * * *", command: "sleep 30"},
				jobSpec{name: "b", cron: "0 0 * * * *", command: "sleep 30"})
			h.quiesce = false
			h.advance(time.Hour)
			evs := h.waitEvents("3 decisions", func(evs []core.SchedulerEvent) bool {
				return count(evs, core.EventJobStart, "")+count(evs, core.EventSkip, "") == 3
			})
			var want []string
			for i, name := range []string{"c", "a", "b"} {
				if i < mc {
					want = append(want, "job_start:"+name)
				} else {
					want = append(want, "skip:"+name+":capacity_unavailable")
				}
			}
			if got := kinds(evs); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("C6 violated:\n got %v\nwant %v", got, want)
			}
		})
	}
}

func TestScheduler_SameJobNeverOverlaps(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "sleep 30"})
	h.quiesce = false // the jobs keep running
	h.advance(4 * time.Second)
	evs := h.snapshot()
	if count(evs, core.EventJobStart, "a") != 1 || count(evs, core.EventSkip, "a") != 3 {
		t.Fatalf("C4 violated:\n%s", dump(evs))
	}
}

func TestScheduler_FailureIsIsolated(t *testing.T) {
	h := newHarness(t, t0, nil, 4,
		jobSpec{name: "bad", cron: "* * * * * *", command: "exit 2"},
		jobSpec{name: "good", cron: "* * * * * *", command: "true"})
	for i := 1; i <= 3; i++ {
		h.advance(time.Second)
		h.waitCount(core.EventJobSuccess, "good", i)
		h.waitCount(core.EventJobFailed, "bad", i)
	}
}

func TestScheduler_MaxConcurrentIsNeverExceeded(t *testing.T) {
	var specs []jobSpec
	for i := range 6 {
		specs = append(specs, jobSpec{name: fmt.Sprintf("j%d", i), cron: "* * * * * *",
			command: fmt.Sprintf("sleep 0.0%d", 2+i), retries: 1, retryDelay: time.Second})
	}
	specs[1].command, specs[4].command = "exit 1", "exit 1"
	for _, mc := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("max_concurrent=%d", mc), func(t *testing.T) {
			h := newHarness(t, t0, nil, mc, specs...)
			h.quiesce = false // attempts overlap on purpose
			for range 20 {
				h.advance(time.Second)
				time.Sleep(15 * time.Millisecond)
			}
			_ = core.SchedulerShutdownForTest(h.r)
			running, peak, starts := 0, 0, 0
			for _, ev := range h.snapshot() {
				switch ev.Kind {
				case core.EventJobStart, core.EventRetryStart:
					running++
					starts++
				case core.EventJobSuccess, core.EventRetrySuccess, core.EventJobFailed, core.EventJobTimeout, core.EventJobCanceled:
					running--
				}
				peak = max(peak, running)
				if running > mc {
					t.Fatalf("C5 violated: %d attempts running with max_concurrent=%d:\n%s", running, mc, dump(h.snapshot()))
				}
			}
			if starts == 0 || peak == 0 {
				t.Fatal("nothing ran")
			}
		})
	}
}

func TestScheduler_ParallelShutdown(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	ignoreTerm := `trap '' TERM; echo $$ >> '` + pids + `'; sleep 1000 & echo $! >> '` + pids + `'; while :; do sleep 0.05; done`
	var specs []jobSpec
	for i := range 4 {
		specs = append(specs, jobSpec{name: fmt.Sprintf("j%d", i), cron: "0 0 * * * *", command: ignoreTerm})
	}
	h := newHarness(t, t0, nil, 4, specs...)
	h.quiesce = false // the jobs keep running
	h.advance(time.Hour)
	h.waitCount(core.EventJobStart, "", 4)
	if !waitFor(t, 3*time.Second, func() bool { return len(readPids(t, pids)) == 8 }) {
		t.Fatal("jobs did not start")
	}
	began := time.Now()
	if err := stopWithin(t, stopBound, func() error { return core.SchedulerShutdownForTest(h.r) }); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(began); d > testGrace+testKill {
		t.Fatalf("C7 violated: shutdown took %v; it must not grow with the number of jobs (grace %v)", d, testGrace)
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("C7 violated: %d survived shutdown", pid)
		}
	}
	if n := count(h.snapshot(), core.EventJobCanceled, ""); n != 4 {
		t.Fatalf("want 4 job_canceled, got %d", n)
	}
}

func TestScheduler_ShutdownSendsTermFirst(t *testing.T) {
	dir := t.TempDir()
	mark, ready := filepath.Join(dir, "term"), filepath.Join(dir, "ready")
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 * * * *",
		command: `trap 'echo term >> "` + mark + `"; exit 0' TERM; : > '` + ready + `'; while :; do sleep 0.05; done`})
	h.quiesce = false // the jobs keep running
	h.advance(time.Hour)
	h.waitCount(core.EventJobStart, "a", 1)
	// shut down once the job's trap is set (it reports ready), not after a fixed sleep
	if !waitFor(t, 5*time.Second, func() bool { _, err := os.Stat(ready); return err == nil }) {
		t.Fatal("the job did not report ready")
	}
	if err := core.SchedulerShutdownForTest(h.r); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mark); !strings.Contains(string(b), "term") {
		t.Fatal("C7 violated: no SIGTERM before the kill")
	}
	evs := h.snapshot()
	if count(evs, core.EventJobCanceled, "a") != 1 || count(evs, core.EventRetryScheduled, "") != 0 {
		t.Fatalf("a canceled attempt is not a failure:\n%s", dump(evs))
	}
	if ev := last(t, evs, core.EventJobCanceled); ev.Error != "shutting down" || !ev.Scheduled.Equal(t0.Add(time.Hour)) {
		t.Fatalf("canceled event: %+v", ev)
	}
}

func TestScheduler_EventLogLine(t *testing.T) {
	ev := core.SchedulerEvent{Kind: core.EventJobFailed, Job: `sync "x"`, Attempt: 2, Scheduled: t0,
		Start: t0.Add(time.Second), End: t0.Add(3 * time.Second), ExitCode: 3, Reason: core.FailExit, Error: "exit status 3"}
	got := core.FormatSchedulerEventForTest(ev, time.UTC)
	want := `⏰ scheduler event=job_failed job="sync \"x\"" attempt=2 scheduled=2026-10-16T10:00:00Z start=2026-10-16T10:00:01Z end=2026-10-16T10:00:03Z duration=2s exit_code=3 reason=exit error="exit status 3"`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestScheduler_SimultaneousRetriesInConfigOrder(t *testing.T) {
	// max_concurrent = 2; "late" holds one slot when the two retries are due at
	// the same instant: the retry of the job written first gets the other slot.
	for _, order := range [][]string{{"x", "y"}, {"y", "x"}} {
		t.Run(strings.Join(order, ","), func(t *testing.T) {
			h := newHarness(t, t0, nil, 2,
				jobSpec{name: order[0], cron: "0 0 11 * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second},
				jobSpec{name: order[1], cron: "0 0 11 * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second},
				jobSpec{name: "late", cron: "5 0 11 * * *", command: "sleep 30"})
			h.advance(time.Hour)
			h.waitCount(core.EventRetryScheduled, "", 2)
			h.quiesce = false
			h.advance(5 * time.Second)
			h.waitCount(core.EventJobStart, "late", 1)
			h.advance(5 * time.Second)
			evs := h.waitCount(core.EventRetryCapacityUnavailble, "", 1)
			if count(evs, core.EventRetryStart, order[0]) != 1 || count(evs, core.EventRetryCapacityUnavailble, order[1]) != 1 {
				t.Fatalf("C6 violated: the retry of %q should win:\n%s", order[0], dump(evs))
			}
		})
	}
}

func TestScheduler_CompletionFreesTheJobForTheNextFire(t *testing.T) {
	// an attempt that ended before the next fire is handled first: the fire starts, no job_busy
	h := newHarness(t, t0, nil, 1, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	for i := 1; i <= 5; i++ {
		h.advance(time.Second)
		h.waitCount(core.EventJobSuccess, "a", i)
	}
	if evs := h.snapshot(); count(evs, core.EventSkip, "") != 0 || count(evs, core.EventJobStart, "a") != 5 {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_ShutdownRacesADueRetry(t *testing.T) {
	for i := range 15 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir := t.TempDir()
			pids := filepath.Join(dir, "pids")
			killLeftovers(t, pids)
			cmd := `if [ -f once ]; then sleep 1000 & echo $! >> '` + pids + `'; wait; else touch once; exit 1; fi`
			h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 11 * * *", command: cmd, retries: 1, retryDelay: time.Second, dir: dir})
			h.advance(time.Hour)
			h.waitCount(core.EventRetryScheduled, "a", 1)
			h.quiesce = false
			done := make(chan error, 1)
			go func() { done <- core.SchedulerShutdownForTest(h.r) }()
			h.clock.Advance(time.Second) // the retry falls due while shutting down
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			evs := h.snapshot()
			if running(evs) != 0 {
				t.Fatalf("an attempt is still running after shutdown:\n%s", dump(evs))
			}
			stopped := false
			for _, ev := range evs {
				if ev.Kind == core.EventSchedulerStop {
					stopped = true
				} else if stopped && (ev.Kind == core.EventRetryStart || ev.Kind == core.EventJobStart) {
					t.Fatalf("C7 violated: started after the stop:\n%s", dump(evs))
				}
			}
			for _, pid := range readPids(t, pids) {
				if alive(pid) {
					t.Fatalf("C7 violated: %d left behind", pid)
				}
			}
		})
	}
}

func TestScheduler_SuccessfulAttemptLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	killLeftovers(t, pids)
	var logs syncLog
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 11 * * *", command: `sleep 1000 & echo $! >> '` + pids + `'`})
	h.advance(time.Hour)
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if !strings.Contains(logs.String(), `scheduler job "a" (attempt 1) left processes behind`) {
		t.Fatalf("the leftover log line does not name the job:\n%s", logs.String())
	}
	if count(evs, core.EventJobFailed, "") != 0 {
		t.Fatalf("got:\n%s", dump(evs))
	}
	for _, pid := range readPids(t, pids) {
		if alive(pid) {
			t.Fatalf("background process %d outlived its attempt", pid)
		}
	}
}

func TestScheduler_RetryDroppedForCapacityEndsTheRun(t *testing.T) {
	// after retry_capacity_unavailable the job is free again: its next fire starts
	h := newHarness(t, t0, nil, 1,
		jobSpec{name: "a", cron: "0 0,1 11 * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second},
		jobSpec{name: "b", cron: "5 0 11 * * *", command: "sleep 20"})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	h.quiesce = false
	h.advance(5 * time.Second) // b takes the only slot
	h.waitCount(core.EventJobStart, "b", 1)
	h.advance(5 * time.Second) // a's retry: no slot
	h.waitCount(core.EventRetryCapacityUnavailble, "a", 1)
	h.advance(50 * time.Second) // 11:01:00, b still holds the slot
	evs := h.waitCount(core.EventSkip, "a", 1)
	if ev := last(t, evs, core.EventSkip); ev.Reason != core.SkipCapacityUnavailable {
		t.Fatalf("a is still busy after its run ended (reason %q):\n%s", ev.Reason, dump(evs))
	}
}

func TestScheduler_AttemptEndingDuringShutdownIsReportedAsIs(t *testing.T) {
	// the attempt ends by itself after the loop stopped and before the abort:
	// its real result is logged (not "canceled"), and it is not retried
	dir := t.TempDir()
	cmd := `touch started; while [ ! -f go ]; do sleep 0.01; done; exit 4`
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 11 * * *", command: cmd, retries: 2, retryDelay: time.Second, dir: dir})
	h.quiesce = false
	h.advance(time.Hour)
	if !waitFor(t, 5*time.Second, func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil }) {
		t.Fatal("job did not start")
	}
	core.SetSchedulerBeforeAbortForTest(h.r, func() {
		_ = os.WriteFile(filepath.Join(dir, "go"), nil, 0o644)
		waitFor(t, 5*time.Second, func() bool { return core.SchedulerPendingResultsForTest(h.r) == 1 })
	})
	if err := core.SchedulerShutdownForTest(h.r); err != nil {
		t.Fatal(err)
	}
	evs := h.snapshot()
	ev := last(t, evs, core.EventJobFailed)
	if ev.ExitCode != 4 || count(evs, core.EventJobCanceled, "") != 0 || count(evs, core.EventRetryScheduled, "") != 0 {
		t.Fatalf("got:\n%s", dump(evs))
	}
	if kinds(evs)[len(kinds(evs))-1] != "job_failed:a" || evs[len(evs)-1].Kind != core.EventSchedulerStop {
		t.Fatalf("order:\n%s", dump(evs))
	}
}

func TestScheduler_SmallClockStepIsNotAJump(t *testing.T) {
	// the loop sleeps 0.5s and the wall clock gets 0.9s ahead meanwhile (an NTP step):
	// within the 1s tolerance this is no jump, so 10:00:01 (just after `expected`
	// 10:00:00.8, before `now - elapsed` 10:00:01.2) runs instead of being skipped
	h := newHarnessWith(t, t0.Add(300*time.Millisecond), nil, 4, func(r *core.SchedulerRunner) { r.MaxSleep = 500 * time.Millisecond },
		jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.clock.StepWall(900 * time.Millisecond)
	h.advance(500 * time.Millisecond)
	evs := h.waitCount(core.EventJobStart, "a", 1)
	if count(evs, core.EventSkip, "") != 0 || !evs[1].Scheduled.Equal(t0.Add(time.Second)) {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_FireOnTimeBeforeAJumpRuns(t *testing.T) {
	// daily 10:00 is due at the end of the sleep (on time by the monotonic clock);
	// meanwhile the clock was set an hour forward. It runs: lateness counts on the
	// time line before the jump, not against the new wall time.
	h := newHarness(t, t0.Add(-time.Second), nil, 4, jobSpec{name: "a", cron: "0 0 10 * * *", command: "true"})
	h.clock.StepWall(time.Hour)
	h.advance(time.Second)
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if count(evs, core.EventSkip, "") != 0 || !evs[1].Scheduled.Equal(t0) {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_JumpBoundaries(t *testing.T) {
	// the clock is set 10s forward while the loop sleeps 1s. Before the jump:
	// 10:00:01 (= expected); jumped over: 10:00:02..10:00:10; on the new time line
	// within the sleep: 10:00:11. The newest due one runs, the other due one is missed.
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.clock.StepWall(10 * time.Second)
	h.advance(time.Second)
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	got := strings.Join(kinds(evs), " ")
	if got != "skip:a:missed skip:a:clock_jump job_start:a job_success:a" {
		t.Fatalf("got %s:\n%s", got, dump(evs))
	}
	if !evs[1].Scheduled.Equal(t0.Add(time.Second)) || !evs[2].Scheduled.Equal(t0.Add(2*time.Second)) ||
		!evs[2].Until.Equal(t0.Add(10*time.Second)) || !evs[3].Scheduled.Equal(t0.Add(11*time.Second)) {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_FireJustAfterAClockChangeRuns(t *testing.T) {
	// daily 10:00; at 09:00 the clock is set to 09:59:55. 10:00 comes 5s later on
	// the new time line: it runs (with the loop's next wake-up), it was not jumped over
	nine := t0.Add(-time.Hour)
	h := newHarness(t, nine, nil, 4, jobSpec{name: "a", cron: "0 0 10 * * *", command: "true"})
	h.clock.StepWall(59*time.Minute + 55*time.Second)
	h.advance(core.DefaultSchedulerMaxSleep) // wake: wall 10:00:10
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if count(evs, core.EventSkip, "") != 0 || !evs[1].Scheduled.Equal(t0) {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_LongStallCountIsALowerBound(t *testing.T) {
	// a 3h stall under a per-second job: one missed event; the count is cut short
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.quiesce = false
	h.clock.Advance(3 * time.Hour)
	h.waitLoop()
	evs := h.waitCount(core.EventJobStart, "a", 1)
	m := evs[1]
	if m.Kind != core.EventSkip || m.Reason != core.SkipMissed || !m.CountAtLeast || m.Count < 10000 || m.Count > 3*3600-1 {
		t.Fatalf("got %+v", m)
	}
	if !evs[2].Scheduled.Equal(t0.Add(3 * time.Hour)) {
		t.Fatalf("the newest fire time should run, got %s", evs[2].Scheduled)
	}
	if !strings.Contains(core.FormatSchedulerEventForTest(m, time.UTC), "count=") || !strings.HasSuffix(strings.Fields(core.FormatSchedulerEventForTest(m, time.UTC))[5], "+") {
		t.Fatalf("log line: %s", core.FormatSchedulerEventForTest(m, time.UTC))
	}
}

func TestScheduler_WholeDayGapDoesNotStopTheJob(t *testing.T) {
	// Pacific/Apia skipped 2011-12-30 entirely: a per-second job goes on on the 31st
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2011, 12, 29, 23, 59, 57, 0, apia)
	h := newHarness(t, start, apia, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.advance(4 * time.Second)
	evs := h.waitCount(core.EventJobSuccess, "a", 4)
	var last time.Time
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart {
			last = ev.Scheduled
		}
	}
	if want := time.Date(2011, 12, 31, 0, 0, 1, 0, apia); !last.Equal(want) {
		t.Fatalf("last run %s, want %s:\n%s", last.In(apia), want, dump(evs))
	}
}

func TestScheduler_SeveralDueFiresRunTheNewestOnly(t *testing.T) {
	// the loop sees three fire times due at once (it could not run): the newest runs,
	// the older ones are one "missed" event, in time order before it
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.clock.Advance(3 * time.Second) // one wake-up for three fire times
	h.waitLoop()
	evs := h.waitCount(core.EventJobSuccess, "a", 1)
	if got := kinds(evs); got[0] != "skip:a:missed" || got[1] != "job_start:a" {
		t.Fatalf("got %v", got)
	}
	m := evs[1]
	if m.Count != 2 || !m.Scheduled.Equal(t0.Add(time.Second)) || !evs[2].Scheduled.Equal(t0.Add(3*time.Second)) {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_TooLateIsMissed(t *testing.T) {
	// a daily fire found due 45s late (the process was stopped) does not run
	h := newHarness(t, t0.Add(-time.Second), nil, 4, jobSpec{name: "a", cron: "0 0 10 * * *", command: "true"})
	h.clock.Advance(46 * time.Second)
	h.waitLoop()
	evs := h.settleQuiet(100 * time.Millisecond)
	if count(evs, core.EventJobStart, "") != 0 || count(evs, core.EventSkip, "a") != 1 || last(t, evs, core.EventSkip).Reason != core.SkipMissed {
		t.Fatalf("got:\n%s", dump(evs))
	}
	// 25s late still runs
	h2 := newHarness(t, t0.Add(-time.Second), nil, 4, jobSpec{name: "b", cron: "0 0 10 * * *", command: "true"})
	h2.clock.Advance(26 * time.Second)
	h2.waitLoop()
	h2.waitCount(core.EventJobSuccess, "b", 1)
}

func TestScheduler_HugeJumpIsOneEvent(t *testing.T) {
	// clock set from 1970 to 2026 under a per-second job: one event, done at once
	h := newHarness(t, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), nil, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.quiesce = false
	h.clock.StepWall(t0.Sub(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)))
	began := time.Now()
	h.clock.Advance(time.Second) // the loop's timer: the next fire (1970-01-01 00:00:01)
	h.waitLoop()
	if d := time.Since(began); d > 2*time.Second {
		t.Fatalf("handling the jump took %v", d)
	}
	evs := h.snapshot()
	// the 1970 fire is missed, the jump is one skip, the fire on the new time line runs
	if count(evs, core.EventSkip, "a") != 2 || count(evs, core.EventJobStart, "a") != 1 || len(evs) > 5 {
		t.Fatalf("got %d events:\n%s", len(evs), dump(evs[:min(len(evs), 10)]))
	}
}

func TestScheduler_RetryDelayCountsFromTheAttemptEnd(t *testing.T) {
	// the attempt takes 5s of (virtual) time: the retry is due 10s after it ended
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 11 * * *", command: "exit 1", retries: 1, retryDelay: 10 * time.Second})
	h.r.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		h.clock.Advance(5 * time.Second) // time passes while the attempt runs
		return exec.CommandContext(ctx, name, args...)
	}
	h.advance(time.Hour)
	evs := h.waitCount(core.EventRetryScheduled, "a", 1)
	failed := last(t, evs, core.EventJobFailed)
	if want := t0.Add(time.Hour + 5*time.Second); !failed.End.Equal(want) {
		t.Fatalf("attempt ended %s, want %s", failed.End, want)
	}
	if rs := last(t, evs, core.EventRetryScheduled); !rs.RetryAt.Equal(failed.End.Add(10 * time.Second)) {
		t.Fatalf("retry at %s, want end + 10s", rs.RetryAt)
	}
	h.advance(5 * time.Second) // 10s after the start, 5s after the end
	if count(h.snapshot(), core.EventRetryStart, "") != 0 {
		t.Fatal("retry counted from the attempt start")
	}
	h.advance(5 * time.Second)
	h.waitCount(core.EventRetryStart, "a", 1)
}

func TestScheduler_ShutdownReportsDroppedRetryWait(t *testing.T) {
	h := newHarness(t, t0, nil, 4, jobSpec{name: "a", cron: "0 0 11 * * *", command: "exit 1", retries: 2, retryDelay: time.Minute})
	h.advance(time.Hour)
	h.waitCount(core.EventRetryScheduled, "a", 1)
	if err := core.SchedulerShutdownForTest(h.r); err != nil {
		t.Fatal(err)
	}
	evs := h.snapshot()
	if count(evs, core.EventRetryCanceled, "a") != 1 || evs[len(evs)-1].Kind != core.EventSchedulerStop {
		t.Fatalf("got:\n%s", dump(evs))
	}
}

func TestScheduler_JumpInsideDSTOverlapDoesNotRepeat(t *testing.T) {
	// 2026-11-01 New York, every 10 minutes in hour 1. 01:10..01:40 run in EDT;
	// at 01:45 EDT the clock is set 30 minutes forward (01:15 EST). The civil
	// times already run must not run again in EST.
	ny, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 11, 1, 1, 5, 0, 0, ny) // EDT
	h := newHarness(t, start, ny, 4, jobSpec{name: "a", cron: "0 */10 1 * * *", command: "true"})
	h.advance(40 * time.Minute) // 01:45 EDT
	h.waitCount(core.EventJobSuccess, "a", 4)
	h.clock.StepWall(30 * time.Minute)
	h.advance(2 * time.Hour)
	evs := h.snapshot()
	seen := map[string]int{}
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart {
			seen[ev.Scheduled.In(ny).Format("15:04")]++
		}
	}
	for civil, n := range seen {
		if n > 1 {
			t.Fatalf("C2 violated: %s ran %d times:\n%s", civil, n, dump(evs))
		}
	}
	if seen["01:10"] != 1 || seen["01:40"] != 1 {
		t.Fatalf("runs: %v\n%s", seen, dump(evs))
	}
}
