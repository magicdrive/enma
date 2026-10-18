package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/common"
	"github.com/magicdrive/enma/internal/cronspec"
)

// DefaultSchedulerMaxSleep bounds one wait of the scheduler loop, so that the
// wall clock is looked at again at least this often: a timer runs on the
// monotonic clock and would not notice the system clock being set.
const DefaultSchedulerMaxSleep = 15 * time.Second

const (
	// jumpTolerance: the wall clock may run ahead of the monotonic clock by this
	// much (NTP steps, reading lag) before the fire times in between count as
	// jumped over.
	jumpTolerance = time.Second
	// lateLimit: a fire found due later than this (the loop could not run, e.g.
	// the process was stopped) is not run any more: it is missed.
	lateLimit = 30 * time.Second
	// maxCatchUpSteps bounds the fire times looked at one by one in one wake-up;
	// beyond it the job jumps to the recent ones (counted as missed).
	maxCatchUpSteps = 10000
	// maxGapSteps bounds the search through civil times that do not exist.
	maxGapSteps = 10000
)

// Scheduler event kinds (the `event=` field of the log lines).
const (
	EventSchedulerStart          = "scheduler_start"
	EventSchedulerStop           = "scheduler_stop"
	EventJobStart                = "job_start"
	EventJobSuccess              = "job_success"
	EventJobFailed               = "job_failed"
	EventJobTimeout              = "job_timeout"
	EventJobCanceled             = "job_canceled"
	EventRetryScheduled          = "retry_scheduled"
	EventRetryStart              = "retry_start"
	EventRetrySuccess            = "retry_success"
	EventRetriesExhausted        = "retries_exhausted"
	EventRetryCapacityUnavailble = "retry_capacity_unavailable"
	EventRetryCanceled           = "retry_canceled" // a retry wait dropped by shutdown
	EventSkip                    = "skip"
)

// Skip reasons (the `reason=` field of a skip event).
const (
	SkipJobBusy             = "job_busy"             // the job's previous run (or its retry wait) is not over
	SkipCapacityUnavailable = "capacity_unavailable" // max_concurrent attempts are running
	SkipClockJump           = "clock_jump"           // the system clock was set forward past it (or the machine slept)
	SkipMissed              = "missed"               // due too long ago, or an older one of several due (no catch-up)
)

// Failure kinds (the `error=` field of job_failed).
const (
	FailExit     = "exit"         // non-zero exit status
	FailStart    = "start_failed" // the command could not be started
	FailTimeout  = "timeout"      // the attempt ran out of its time limit
	FailLeftover = "leftover"     // processes it left behind could not be stopped
)

// SchedulerEvent is one scheduler event, also logged as a line.
type SchedulerEvent struct {
	Kind      string
	Job       string
	Attempt   int
	Scheduled time.Time // the cron time of the run (zero for scheduler events)
	Start     time.Time
	End       time.Time
	RetryAt   time.Time
	Until     time.Time // clock_jump skips: fire times up to here were skipped
	Count     int       // skips: how many fire times (from Scheduled on)
	// CountAtLeast: Count is a lower bound (a very long stall was cut short)
	CountAtLeast bool
	ExitCode     int // -1 when there is none
	Error        string
	Reason       string
}

// SchedulerRunner runs jobs on cron schedules.
//
// One goroutine (the loop in Serve) owns every piece of state and decides
// everything: which fire or retry starts, in which order, and what is
// skipped. Attempts run in their own goroutines through the same command
// machinery as hotload / watch (own process group, stopped with the scope)
// and report back to the loop once their process group is gone.
//
// Two independent kinds of occupation:
//   - a job is busy from the cron fire that starts a run until the run's
//     final result, including retry waits: a job never runs twice at once;
//   - a slot is held by an attempt from its start until its processes are
//     reaped: at most MaxConcurrent attempts run at once. A retry wait holds
//     no slot.
type SchedulerRunner struct {
	Options     *option.SchedulerOption
	Clock       Clock
	ExecCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
	// StopGrace / KillWait: see HotloadRunner. Zero means the defaults.
	StopGrace time.Duration
	KillWait  time.Duration
	// MaxSleep bounds one wait of the loop. Zero means DefaultSchedulerMaxSleep.
	MaxSleep time.Duration
	// OnEvent, when set, is called for every event in order, never concurrently.
	OnEvent func(SchedulerEvent)

	onWait      func() // test hook: the loop is about to wait
	beforeAbort func() // test hook: shutdown, between the loop's end and the abort

	scope        commandScope
	initOnce     sync.Once
	stopOnce     sync.Once
	stop         chan struct{}
	done         chan struct{} // closed when Serve returns
	serving      atomic.Bool
	results      chan attemptResult
	attempts     sync.WaitGroup // attempt goroutines that have not reported yet
	jobs         []*jobState    // the loop's state; read by shutdown only after the loop has returned
	started      time.Time      // wall clock when Serve started: repeats of local date-times met after it do not fire
	shutdownOnce sync.Once
	shutdownErr  error
}

type attemptResult struct {
	job, attempt int
	exitCode     int
	err          error
	failure      string // "" on success
	canceled     bool
	start, end   time.Time
	endMono      time.Duration
}

type jobState struct {
	job          *option.SchedulerJob
	nextCivil    time.Time
	nextAt       time.Time // instant of nextCivil
	hasNext      bool
	busy         bool // a run (attempts and retry waits) is in progress
	running      bool // an attempt holds a slot
	attempt      int
	scheduled    time.Time // cron time of the current run
	retryPending bool
	retryMono    time.Duration
}

func NewSchedulerRunner(opt *option.SchedulerOption) *SchedulerRunner {
	return &SchedulerRunner{
		Options:     opt,
		Clock:       NewRealClock(),
		ExecCommand: exec.CommandContext,
		StopGrace:   DefaultStopGrace,
		KillWait:    DefaultKillWait,
	}
}

func (r *SchedulerRunner) stopGrace() time.Duration { return orDefault(r.StopGrace, DefaultStopGrace) }
func (r *SchedulerRunner) killWait() time.Duration  { return orDefault(r.KillWait, DefaultKillWait) }
func (r *SchedulerRunner) maxSleep() time.Duration {
	return orDefault(r.MaxSleep, DefaultSchedulerMaxSleep)
}

func (r *SchedulerRunner) init() {
	r.initOnce.Do(func() {
		r.stop = make(chan struct{})
		r.done = make(chan struct{})
		r.results = make(chan attemptResult, len(r.Options.Jobs)) // one attempt per job at most: sends never block
		if r.Clock == nil {
			r.Clock = NewRealClock()
		}
		if r.ExecCommand == nil {
			r.ExecCommand = exec.CommandContext
		}
	})
}

// Start runs the scheduler until a signal stops it, like the other modes:
// pid file, log file, first signal → graceful shutdown (exit 0, or 1 when it
// is incomplete), second signal → kill at once (exit 1).
func (r *SchedulerRunner) Start() (err error) {
	env, err := openStartEnv(r.Options.PidPathOpt, r.Options.LogPathOpt)
	if err != nil {
		return err
	}
	defer env.failed(&err)

	fmt.Println(common.StartMessage())
	fmt.Printf("Start Scheduler mode.\n\n\n")

	env.handleSignals("🛑 Stopping scheduler...", r.shutdown, r.forceKill)

	if err := r.Serve(); err != nil {
		return err
	}
	select {} // stopped by a signal: the handler owns the exit
}

// Serve runs the scheduler loop until shutdown.
func (r *SchedulerRunner) Serve() error {
	r.init()
	r.serving.Store(true)
	defer close(r.done)
	loc := r.Options.Location
	if loc == nil {
		loc = time.Local
	}
	jobs := make([]*jobState, len(r.Options.Jobs))
	now, startMono := r.Clock.Read()
	r.started = now
	for i := range r.Options.Jobs {
		js := &jobState{job: &r.Options.Jobs[i]}
		r.advance(js, now, loc)
		jobs[i] = js
	}
	r.jobs = jobs
	r.emit(SchedulerEvent{Kind: EventSchedulerStart, ExitCode: -1,
		Reason: fmt.Sprintf("jobs=%d max_concurrent=%d timezone=%s", len(jobs), r.Options.MaxConcurrent, loc)})
	running := 0
	prevWall, prevMono := now, startMono

	for {
		now, mono := r.Clock.Read()
		wait := r.maxSleep()
		for _, js := range jobs {
			if js.hasNext {
				wait = min(wait, js.nextAt.Sub(now))
			}
			if js.retryPending {
				wait = min(wait, js.retryMono-mono)
			}
		}
		timer := r.Clock.NewTimer(max(wait, 0))
		if r.onWait != nil {
			r.onWait()
		}
		select {
		case <-r.stop:
			timer.Stop()
			return nil
		case res := <-r.results:
			timer.Stop()
			running -= r.finish(jobs[res.job], res)
		case <-timer.C():
		}
	drain:
		for {
			select {
			case res := <-r.results:
				running -= r.finish(jobs[res.job], res)
			default:
				break drain
			}
		}
		select {
		case <-r.stop:
			return nil
		default:
		}

		now, mono = r.Clock.Read()
		// Wall time that should have passed by the monotonic clock: fire times up
		// to it are due; when the wall clock is further ahead than jumpTolerance,
		// the ones beyond it were jumped over (clock set forward, machine asleep)
		// and are not caught up.
		elapsed := mono - prevMono
		expected := prevWall.Add(elapsed)
		prevWall, prevMono = now, mono
		jumped := now.Sub(expected) > jumpTolerance

		// 1. retries of runs already in progress, in config order
		for _, js := range jobs {
			if !js.retryPending || mono < js.retryMono {
				continue
			}
			js.retryPending = false
			if running >= r.Options.MaxConcurrent {
				r.emit(r.jobEvent(js, EventRetryCapacityUnavailble, js.attempt+1))
				js.busy = false
				continue
			}
			running++
			r.startAttempt(js, indexOf(jobs, js), js.attempt+1)
		}
		// 2. cron fires, in config order
		for i, js := range jobs {
			running += r.fireDue(js, i, now, expected, elapsed, jumped, running, loc)
		}
	}
}

// dueFires is what collect found: the newest fire time due and the older ones.
type dueFires struct {
	newest    time.Time
	has       bool
	olderFrom time.Time
	older     int
	capped    bool // older is a lower bound (a long stall was cut short)
}

// collect moves js past its fire times up to limit.
func (r *SchedulerRunner) collect(js *jobState, limit time.Time, loc *time.Location) dueFires {
	var d dueFires
	for steps := 0; js.hasNext && !js.nextAt.After(limit); steps++ {
		if d.has {
			if d.older == 0 {
				d.olderFrom = d.newest
			}
			d.older++
		}
		d.newest, d.has = js.nextAt, true
		if steps == maxCatchUpSteps { // a long stall: go straight to the recent fire times
			from := limit.Add(-lateLimit)
			if from.After(d.newest) {
				d.capped = true
				r.advance(js, from, loc)
				continue
			}
		}
		r.advance(js, js.nextAt, loc)
	}
	return d
}

// fireDue handles js's fire times up to now and returns the slots it took (0 or 1).
//
// Without a jump every fire time up to now is due. With one (the wall clock got
// ahead of the monotonic clock by more than jumpTolerance), the jump is taken to
// have happened when the loop's sleep began: fire times up to `expected` came
// before it, those after it up to now-elapsed were jumped over (clock_jump), and
// those in the last `elapsed` came on the new time line and are due as well.
//
// Of the due fire times only the newest may run, and only if it is not more than
// lateLimit late on its own time line; the others are missed. Events come in
// the order of the fire times; each kind of skip is one event.
func (r *SchedulerRunner) fireDue(js *jobState, idx int, now, expected time.Time, elapsed time.Duration, jumped bool,
	running int, loc *time.Location) int {
	name := js.job.Name
	preLimit := now
	if jumped {
		preLimit = expected
	}
	pre := r.collect(js, preLimit, loc)
	var post dueFires
	var jumpFrom, jumpEnd time.Time
	jumpedOver := false
	if jumped {
		jumpEnd = now.Add(-elapsed)
		if jumpEnd.Before(expected) {
			jumpEnd = expected
		}
		if js.hasNext && !js.nextAt.After(jumpEnd) {
			jumpFrom, jumpedOver = js.nextAt, true
			r.advance(js, jumpEnd, loc)
		}
		post = r.collect(js, now, loc)
	}

	// the candidate to run: the newest due fire time, late or not on its own time line
	candidate, lateRef := pre, preLimit
	if post.has {
		candidate, lateRef = post, now
		pre.olderFrom, pre.older = firstOf(pre), pre.older+1 // pre's newest is superseded
		if !pre.has {
			pre.older = 0
		}
	}
	run := candidate.has && lateRef.Sub(candidate.newest) <= lateLimit
	if candidate.has && !run { // too late: missed too
		if candidate.older == 0 {
			candidate.olderFrom = candidate.newest
		}
		candidate.older++
	}
	if post.has {
		post = candidate
	} else {
		pre = candidate
	}

	missedEvent := func(d dueFires) {
		if d.older > 0 {
			r.emit(SchedulerEvent{Kind: EventSkip, Job: name, Scheduled: d.olderFrom, Count: d.older, CountAtLeast: d.capped,
				ExitCode: -1, Reason: SkipMissed})
		}
	}
	took := 0
	decide := func(at time.Time) {
		switch {
		case js.busy:
			r.emit(SchedulerEvent{Kind: EventSkip, Job: name, Scheduled: at, Count: 1, ExitCode: -1, Reason: SkipJobBusy})
		case running >= r.Options.MaxConcurrent:
			r.emit(SchedulerEvent{Kind: EventSkip, Job: name, Scheduled: at, Count: 1, ExitCode: -1, Reason: SkipCapacityUnavailable})
		default:
			took = 1
			js.busy, js.scheduled = true, at
			r.startAttempt(js, idx, 1)
		}
	}
	missedEvent(pre)
	if run && !post.has {
		decide(pre.newest)
	}
	if jumpedOver {
		r.emit(SchedulerEvent{Kind: EventSkip, Job: name, Scheduled: jumpFrom, Until: jumpEnd, ExitCode: -1, Reason: SkipClockJump})
	}
	missedEvent(post)
	if run && post.has {
		decide(post.newest)
	}
	return took
}

// firstOf returns the earliest fire time d covers.
func firstOf(d dueFires) time.Time {
	if d.older > 0 {
		return d.olderFrom
	}
	return d.newest
}

func indexOf(jobs []*jobState, js *jobState) int {
	for i, j := range jobs {
		if j == js {
			return i
		}
	}
	return -1
}

// advance moves js to its first fire opportunity after the instant `after`.
//
// A fire opportunity is a local date-time matching the job's cron expression,
// at an instant of it: none in a daylight-saving gap, two when the clocks go back
// (the same local date-time, whatever the UTC offset). The second instant is an
// opportunity only if the first one came before the scheduler started: every
// instant after the start is met by the loop (run, failed, skipped, missed or
// jumped over), and a local date-time met once does not fire again. This needs
// no history: it follows from the instants and the start time.
//
// Fire opportunities are taken in the order of their instants, which is not the
// order of their local date-times inside a repeated hour: the search covers the
// local date-times that are still ahead (from the earliest wall reading in the
// next hours) and keeps the earliest instant.
func (r *SchedulerRunner) advance(js *jobState, after time.Time, loc *time.Location) {
	reading := cronspec.Civil(after.In(loc))
	from := cronspec.MinCivilFrom(after, loc).Add(-time.Second)
	var best, bestCivil time.Time
	found := false
	var limit time.Time // with a candidate: no later local date-time can come earlier
	for range maxGapSteps {
		c, ok := js.job.Cron.NextCivil(from)
		if !ok || (found && c.After(limit)) {
			break
		}
		instants := cronspec.Instants(c, loc)
		for k, at := range instants {
			if !at.After(after) {
				continue
			}
			if k > 0 && instants[0].After(r.started) {
				// a repeat whose first instant was met while running; so were the ones
				// after it whose first instant came by now: continue after the latest
				// reading up to now (the current reading on the first pass; the end of
				// the first pass when `after` is already on the second one)
				if c.Before(reading) {
					c = reading
				}
				if e := cronspec.MaxCivilBetween(instants[0], after, loc); c.Before(e) {
					c = e
				}
				break
			}
			if !found || at.Before(best) {
				best, bestCivil, found = at, c, true
				limit = cronspec.MaxCivilBetween(after, best, loc)
			}
			if k > 0 && c.Before(reading) {
				// a repeat before the current reading: the local date-times after it up to
				// the reading had their first instant by now and repeat later than this
				// one, so none of them can come earlier: continue from the reading
				c = reading
			}
			break
		}
		from = c
		if len(instants) == 0 { // c is in a gap: continue from the gap's end
			if end := cronspec.GapEnd(c, loc); end.After(from) {
				from = end.Add(-time.Second)
			}
		}
	}
	js.nextCivil, js.nextAt, js.hasNext = bestCivil, best, found
	if !found {
		log.Printf("⚠️  scheduler job %q: no fire time found; it will not run again", js.job.Name)
	}
}

func (r *SchedulerRunner) jobEvent(js *jobState, kind string, attempt int) SchedulerEvent {
	return SchedulerEvent{Kind: kind, Job: js.job.Name, Attempt: attempt, Scheduled: js.scheduled, ExitCode: -1}
}

// startAttempt starts attempt n of js's run; the caller has taken a slot.
func (r *SchedulerRunner) startAttempt(js *jobState, idx, n int) {
	js.running, js.attempt = true, n
	kind := EventJobStart
	if n > 1 {
		kind = EventRetryStart
	}
	ev := r.jobEvent(js, kind, n)
	ev.Start = r.Clock.Now()
	r.emit(ev)
	r.attempts.Add(1)
	go func() {
		defer r.attempts.Done() // after the report
		r.runAttempt(idx, n, *js.job, ev.Start)
	}()
}

// runAttempt runs one attempt and reports it once its process group is gone.
// The report is sent before the attempt leaves the scope, so a shutdown (which
// waits for the scope) always finds it.
func (r *SchedulerRunner) runAttempt(idx, n int, job option.SchedulerJob, start time.Time) {
	res := attemptResult{job: idx, attempt: n, exitCode: -1, start: start}
	report := func() {
		res.end, res.endMono = r.Clock.Read()
		r.results <- res // buffered for one attempt per job: never blocks
	}
	ctx := r.scope.context()
	if job.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, job.Timeout)
		defer cancel()
	}
	cmd := r.ExecCommand(ctx, "sh", plainShellArgs(job.Command)...)
	cmd.Dir = job.WorkingDir
	killGroupOnCancel(cmd, &r.scope, r.stopGrace(), r.killWait())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	release, err := r.scope.enter()
	if err != nil {
		res.canceled, res.err = true, err
		report()
		return
	}
	defer release()
	defer report() // runs before release
	runErr := cmd.Run()
	var leftErr error
	if cmd.Process != nil {
		leftErr = stopLeftovers(cmd, cmd.Process.Pid, &r.scope, r.stopGrace(), r.killWait(),
			fmt.Sprintf("scheduler job %q (attempt %d)", job.Name, n))
	}
	var exitErr *exec.ExitError
	switch {
	case r.scope.context().Err() != nil:
		res.canceled, res.err = true, errShuttingDown
	case runErr != nil && ctx.Err() == context.DeadlineExceeded:
		res.failure, res.err = FailTimeout, fmt.Errorf("timed out after %s", job.Timeout)
	case errors.As(runErr, &exitErr):
		res.failure, res.err, res.exitCode = FailExit, runErr, exitErr.ExitCode()
	case runErr != nil:
		res.failure, res.err = FailStart, runErr
	case leftErr != nil:
		res.failure, res.err, res.exitCode = FailLeftover, leftErr, 0
	default:
		res.exitCode = 0
	}
}

// finish records an attempt's result and returns the number of slots freed (1).
func (r *SchedulerRunner) finish(js *jobState, res attemptResult) int {
	js.running = false
	r.emit(r.resultEvent(js, res))
	switch {
	case res.canceled, res.failure == "":
		js.busy = false
	default:
		if res.attempt <= js.job.Retries {
			js.retryPending, js.retryMono = true, res.endMono+js.job.RetryDelay
			rev := r.jobEvent(js, EventRetryScheduled, res.attempt+1)
			rev.RetryAt = res.end.Add(js.job.RetryDelay)
			r.emit(rev)
		} else {
			if js.job.Retries > 0 {
				r.emit(r.jobEvent(js, EventRetriesExhausted, res.attempt))
			}
			js.busy = false
		}
	}
	return 1
}

// resultEvent is the event of an attempt's result.
func (r *SchedulerRunner) resultEvent(js *jobState, res attemptResult) SchedulerEvent {
	ev := r.jobEvent(js, "", res.attempt)
	ev.Start, ev.End, ev.ExitCode = res.start, res.end, res.exitCode
	if res.err != nil {
		ev.Error = res.err.Error()
	}
	switch {
	case res.canceled:
		ev.Kind = EventJobCanceled
	case res.failure == "":
		ev.Kind = EventJobSuccess
		if res.attempt > 1 {
			ev.Kind = EventRetrySuccess
		}
	case res.failure == FailTimeout:
		ev.Kind, ev.Reason = EventJobTimeout, res.failure
	default:
		ev.Kind, ev.Reason = EventJobFailed, res.failure
	}
	return ev
}

// shutdown stops the loop (no new fires, retry waits dropped), then stops every
// running attempt at once: SIGTERM to all process groups together, SIGKILL to
// those left after StopGrace. The wait does not grow with the number of jobs.
// It runs once; later calls return its result.
func (r *SchedulerRunner) shutdown() error {
	r.shutdownOnce.Do(func() { r.shutdownErr = r.doShutdown() })
	return r.shutdownErr
}

func (r *SchedulerRunner) doShutdown() error {
	r.init()
	r.stopOnce.Do(func() { close(r.stop) })
	served := r.serving.Load()
	if served {
		<-r.done // the loop starts nothing more
	}
	if r.beforeAbort != nil {
		r.beforeAbort()
	}
	err := r.scope.abort(r.stopGrace() + r.killWait() + time.Second)
	if err != nil {
		log.Printf("❌ Failed to stop jobs: %v", err)
	}
	// every attempt the loop started reports, also one that had not yet entered
	// the scope (it fails to and reports canceled)
	reported := make(chan struct{})
	go func() { r.attempts.Wait(); close(reported) }()
	select {
	case <-reported:
	case <-time.After(r.killWait()):
		err = errors.Join(err, errors.New("an attempt did not report"))
	}
	for {
		select {
		case res := <-r.results:
			// the loop has stopped: report the result as it is, retry nothing
			js := &jobState{job: &r.Options.Jobs[res.job]}
			if served { // the loop has returned: its state is ours
				js = r.jobs[res.job]
			}
			r.emit(r.resultEvent(js, res))
			continue
		default:
		}
		break
	}
	if served {
		for _, js := range r.jobs {
			if js.retryPending {
				js.retryPending = false
				r.emit(r.jobEvent(js, EventRetryCanceled, js.attempt+1))
			}
		}
	}
	r.emit(SchedulerEvent{Kind: EventSchedulerStop, ExitCode: -1})
	return err
}

// forceKill kills every running attempt's process group at once.
func (r *SchedulerRunner) forceKill() {
	r.init()
	r.stopOnce.Do(func() { close(r.stop) })
	r.scope.forceKill()
	_ = r.scope.abort(r.killWait())
}

func (r *SchedulerRunner) emit(ev SchedulerEvent) {
	log.Print(formatSchedulerEvent(ev, r.Options.Location))
	if r.OnEvent != nil {
		r.OnEvent(ev)
	}
}

// formatSchedulerEvent renders an event as one log line of key=value fields.
// The command text is never logged (it may hold secrets); jobs are named.
func formatSchedulerEvent(ev SchedulerEvent, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var b strings.Builder
	b.WriteString("⏰ scheduler event=" + ev.Kind)
	if ev.Job != "" {
		fmt.Fprintf(&b, " job=%q", ev.Job)
	}
	if ev.Attempt > 0 {
		fmt.Fprintf(&b, " attempt=%d", ev.Attempt)
	}
	ts := func(k string, t time.Time) {
		if !t.IsZero() {
			fmt.Fprintf(&b, " %s=%s", k, t.In(loc).Format(time.RFC3339))
		}
	}
	ts("scheduled", ev.Scheduled)
	ts("start", ev.Start)
	ts("end", ev.End)
	if !ev.Start.IsZero() && !ev.End.IsZero() {
		fmt.Fprintf(&b, " duration=%s", ev.End.Sub(ev.Start).Round(time.Millisecond))
	}
	ts("retry_at", ev.RetryAt)
	ts("until", ev.Until)
	if ev.Count > 1 || ev.CountAtLeast {
		fmt.Fprintf(&b, " count=%d", ev.Count)
		if ev.CountAtLeast {
			b.WriteString("+")
		}
	}
	if ev.ExitCode >= 0 {
		fmt.Fprintf(&b, " exit_code=%d", ev.ExitCode)
	}
	if ev.Reason != "" {
		fmt.Fprintf(&b, " reason=%s", ev.Reason)
	}
	if ev.Error != "" {
		fmt.Fprintf(&b, " error=%q", ev.Error)
	}
	return b.String()
}
