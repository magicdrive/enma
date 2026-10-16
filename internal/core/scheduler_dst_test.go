//go:build !windows

package core_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/core"
	"github.com/magicdrive/enma/internal/cronspec"
)

// DST overlap semantics (owner's specification, 2026-10-17):
//
//	A local date-time (job, calendar date and wall-clock time in the scheduler's
//	time zone, whatever the UTC offset) is one fire opportunity. When the clocks
//	go back it happens twice. Once the running scheduler has met the first
//	instance — run, failed, skipped (job_busy, capacity_unavailable) or missed
//	(late, or jumped over by a clock change while running) — the second instance
//	does not fire. An instance that passed before the scheduler started is not
//	"met": a scheduler started between the two instances fires the second one.
//	Retries are not fires and are not affected.

// overlap describes a fall-back transition found in a zone, without zone-specific constants.
type overlap struct {
	loc           *time.Location
	civil         time.Time // a local date-time that happens twice (minute precision)
	first, second time.Time // its two instants
	shift         time.Duration
	transition    time.Time
}

// findOverlap returns the first fall-back transition of loc after from.
func findOverlap(t *testing.T, name string, from time.Time) overlap {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	cur := from.In(loc)
	for range 20 {
		_, end := cur.ZoneBounds()
		if end.IsZero() {
			break
		}
		_, before := cur.Zone()
		_, after := end.In(loc).Zone()
		if after < before {
			shift := time.Duration(before-after) * time.Second
			// the middle of the repeated period, on a whole minute
			c := cronspec.Civil(end.In(loc)).Add(shift / 2).Truncate(time.Minute)
			in := cronspec.Instants(c, loc)
			if len(in) != 2 {
				t.Fatalf("%s: %s should happen twice, got %v", name, c, in)
			}
			return overlap{loc: loc, civil: c, first: in[0], second: in[1], shift: shift, transition: end}
		}
		cur = end.In(loc)
	}
	t.Fatalf("%s: no fall-back transition after %s", name, from)
	return overlap{}
}

func dailyCron(c time.Time) string { return fmt.Sprintf("0 %d %d * * *", c.Minute(), c.Hour()) }

var overlapZones = []string{"America/New_York", "Europe/Berlin", "Australia/Sydney"}

func forZones(t *testing.T, f func(t *testing.T, o overlap)) {
	for _, z := range overlapZones {
		t.Run(z, func(t *testing.T) { f(t, findOverlap(t, z, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))) })
	}
}

// startsFor returns the scheduled instants of the job's starts at the overlap civil time.
func startsFor(evs []core.SchedulerEvent, job string, o overlap) []time.Time {
	var out []time.Time
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart && ev.Job == job && cronspec.Civil(ev.Scheduled.In(o.loc)).Equal(o.civil) {
			out = append(out, ev.Scheduled)
		}
	}
	return out
}

func eventsAt(evs []core.SchedulerEvent, kind, job string, at time.Time) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == kind && ev.Job == job && ev.Scheduled.Equal(at) {
			n++
		}
	}
	return n
}

func TestDSTOverlap_RunAtFirstNotAgain(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		h := newHarness(t, o.first.Add(-10*time.Minute), o.loc, 4, jobSpec{name: "a", cron: dailyCron(o.civil), command: "true"})
		h.advance(o.second.Sub(o.first) + 30*time.Minute)
		if got := startsFor(h.snapshot(), "a", o); len(got) != 1 || !got[0].Equal(o.first) {
			t.Fatalf("runs at %s: %v, want only the first instance %s:\n%s", o.civil, got, o.first, dump(h.snapshot()))
		}
	})
}

func TestDSTOverlap_FailureAtFirstNotAgain_RetryIndependent(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		h := newHarness(t, o.first.Add(-10*time.Minute), o.loc, 4,
			jobSpec{name: "a", cron: dailyCron(o.civil), command: "exit 1", retries: 1, retryDelay: time.Minute})
		h.advance(o.second.Sub(o.first) + 30*time.Minute)
		evs := h.snapshot()
		if got := startsFor(evs, "a", o); len(got) != 1 || !got[0].Equal(o.first) {
			t.Fatalf("fires: %v:\n%s", got, dump(evs))
		}
		if count(evs, core.EventRetryStart, "a") != 1 { // the retry is its own run step, not a fire
			t.Fatalf("retry:\n%s", dump(evs))
		}
	})
}

func TestDSTOverlap_BusyAtFirstNotAgain(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		dir := t.TempDir()
		release := filepath.Join(dir, "go")
		cron := fmt.Sprintf("0 %d,%d %d * * *", o.civil.Add(-time.Minute).Minute(), o.civil.Minute(), o.civil.Hour())
		if o.civil.Minute() == 0 { // the busy fire one minute earlier is built in the same hour
			t.Fatalf("precondition: the repeated local time %s is on a full hour", o.civil)
		}
		h := newHarness(t, o.first.Add(-10*time.Minute), o.loc, 4,
			jobSpec{name: "a", cron: cron, command: `while [ ! -f '` + release + `' ]; do sleep 0.01; done`, dir: dir})
		h.quiesce = false
		h.advance(9 * time.Minute) // the fire one minute before starts and keeps running
		h.waitCount(core.EventJobStart, "a", 1)
		h.advance(time.Minute) // first instance: busy
		evs := h.waitCount(core.EventSkip, "a", 1)
		if eventsAt(evs, core.EventSkip, "a", o.first) != 1 {
			t.Fatalf("first instance not skipped as busy:\n%s", dump(evs))
		}
		_ = os.WriteFile(release, nil, 0o644)
		h.waitCount(core.EventJobSuccess, "a", 1)
		h.quiesce = true
		h.advance(o.second.Sub(o.first) + 10*time.Minute)
		if got := startsFor(h.snapshot(), "a", o); len(got) != 0 {
			t.Fatalf("second instance ran after a busy skip: %v:\n%s", got, dump(h.snapshot()))
		}
	})
}

func TestDSTOverlap_CapacityAtFirstNotAgain(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		dir := t.TempDir()
		release := filepath.Join(dir, "go")
		blockerAt := o.civil.Add(-time.Minute)
		h := newHarness(t, o.first.Add(-10*time.Minute), o.loc, 1,
			jobSpec{name: "blocker", cron: dailyCron(blockerAt), command: `while [ ! -f '` + release + `' ]; do sleep 0.01; done`, dir: dir},
			jobSpec{name: "a", cron: dailyCron(o.civil), command: "true"})
		h.quiesce = false
		h.advance(9 * time.Minute)
		h.waitCount(core.EventJobStart, "blocker", 1)
		h.advance(time.Minute)
		evs := h.waitCount(core.EventSkip, "a", 1)
		if eventsAt(evs, core.EventSkip, "a", o.first) != 1 {
			t.Fatalf("first instance not skipped for capacity:\n%s", dump(evs))
		}
		_ = os.WriteFile(release, nil, 0o644)
		h.waitCount(core.EventJobSuccess, "blocker", 1)
		h.quiesce = true
		h.advance(o.second.Sub(o.first) + 10*time.Minute)
		if got := startsFor(h.snapshot(), "a", o); len(got) != 0 {
			t.Fatalf("second instance ran after a capacity skip: %v:\n%s", got, dump(h.snapshot()))
		}
	})
}

func TestDSTOverlap_MissedAtFirstNotAgain(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		h := newHarness(t, o.first.Add(-10*time.Second), o.loc, 4, jobSpec{name: "a", cron: dailyCron(o.civil), command: "true"})
		h.clock.Advance(time.Minute) // the loop could not run: the first instance is found 50s late
		h.waitLoop()
		evs := h.snapshot()
		if eventsAt(evs, core.EventSkip, "a", o.first) != 1 {
			t.Fatalf("first instance not missed:\n%s", dump(evs))
		}
		h.advance(o.second.Sub(o.first) + 10*time.Minute)
		if got := startsFor(h.snapshot(), "a", o); len(got) != 0 {
			t.Fatalf("second instance ran after a miss: %v:\n%s", got, dump(h.snapshot()))
		}
	})
}

func TestDSTOverlap_JumpedOverFirstNotAgain(t *testing.T) {
	// the clock is set forward over the first instance, landing before the second
	forZones(t, func(t *testing.T, o overlap) {
		h := newHarness(t, o.first.Add(-2*time.Minute), o.loc, 4, jobSpec{name: "a", cron: dailyCron(o.civil), command: "true"})
		land := o.first.Add(o.second.Sub(o.first) / 2) // between the instances
		h.clock.StepWall(land.Sub(o.first.Add(-2 * time.Minute)))
		h.advance(core.DefaultSchedulerMaxSleep)
		evs := h.snapshot()
		if count(evs, core.EventSkip, "a") != 1 || last(t, evs, core.EventSkip).Reason != core.SkipClockJump {
			t.Fatalf("first instance not jumped over:\n%s", dump(evs))
		}
		h.advance(o.second.Sub(land) + 10*time.Minute)
		if got := startsFor(h.snapshot(), "a", o); len(got) != 0 {
			t.Fatalf("second instance ran after the first was jumped over: %v:\n%s", got, dump(h.snapshot()))
		}
	})
}

func TestDSTOverlap_StartedBetweenRunsTheSecond(t *testing.T) {
	// started after the first instance and before the second: the second is a
	// fire opportunity met while running. Both possible wall readings at start:
	// still in the first pass (reading after the civil time) or already in the
	// second pass (reading before it).
	forZones(t, func(t *testing.T, o overlap) {
		for _, start := range []time.Time{
			o.first.Add(o.shift / 4),      // first pass, reading > civil
			o.transition.Add(time.Minute), // second pass, reading < civil
		} {
			if !start.After(o.first) || !start.Before(o.second) {
				t.Fatalf("bad start %s", start)
			}
			h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: dailyCron(o.civil), command: "true"})
			h.advance(o.second.Sub(start) + 10*time.Minute)
			if got := startsFor(h.snapshot(), "a", o); len(got) != 1 || !got[0].Equal(o.second) {
				t.Fatalf("start %s: runs %v, want the second instance %s:\n%s", start.In(o.loc), got, o.second, dump(h.snapshot()))
			}
			_ = core.SchedulerShutdownForTest(h.r)
		}
	})
}

func TestDSTOverlap_StartedBeforeRunsOnce(t *testing.T) {
	// a per-minute job through the whole repeated period: each local time once
	forZones(t, func(t *testing.T, o overlap) {
		start := o.transition.Add(-o.shift - 5*time.Minute)
		h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "0 * * * * *", command: "true"})
		h.advance(2*o.shift + 10*time.Minute)
		seen := map[time.Time]int{}
		for _, ev := range h.snapshot() {
			if ev.Kind == core.EventJobStart {
				seen[cronspec.Civil(ev.Scheduled.In(o.loc))]++
			}
		}
		for c, n := range seen {
			if n != 1 {
				t.Fatalf("%s ran %d times", c, n)
			}
		}
		if len(seen) < int((o.shift+10*time.Minute)/time.Minute) {
			t.Fatalf("only %d local times ran", len(seen))
		}
	})
}

func TestDSTOverlap_ClockChangesInsideTheOverlap(t *testing.T) {
	// forward and back inside the repeated period: no local time fires twice
	forZones(t, func(t *testing.T, o overlap) {
		start := o.transition.Add(-o.shift + time.Minute)
		h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "0 * * * * *", command: "true"})
		h.advance(10 * time.Minute)
		h.clock.StepWall(o.shift / 2) // forward
		h.advance(10 * time.Minute)
		h.clock.StepWall(-o.shift / 3) // back
		h.advance(o.shift + 20*time.Minute)
		seen := map[time.Time]int{}
		for _, ev := range h.snapshot() {
			if ev.Kind == core.EventJobStart {
				seen[cronspec.Civil(ev.Scheduled.In(o.loc))]++
			}
		}
		for c, n := range seen {
			if n != 1 {
				t.Fatalf("%s ran %d times:\n%s", c, n, dump(h.snapshot()))
			}
		}
	})
}

func TestDSTOverlap_JumpIntoTheOverlapPerMinute(t *testing.T) {
	// a per-minute job; the clock is set forward from before the repeated period
	// to its second pass: the local times whose first instance was jumped over
	// must not fire at their second instance (A4-02)
	forZones(t, func(t *testing.T, o overlap) {
		start := o.transition.Add(-o.shift - 10*time.Minute) // before the repeated period
		h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "0 * * * * *", command: "true"})
		h.advance(2 * time.Minute)
		land := o.transition.Add(o.shift / 3) // second pass
		h.clock.StepWall(land.Sub(h.clock.Now()))
		h.advance(o.shift + 10*time.Minute)
		ovStart := cronspec.Civil(o.transition.In(o.loc)) // the repeated local times: [ovStart, ovStart+shift)
		seen := map[time.Time]int{}
		for _, ev := range h.snapshot() {
			if ev.Kind != core.EventJobStart {
				continue
			}
			c := cronspec.Civil(ev.Scheduled.In(o.loc))
			seen[c]++
			repeated := !c.Before(ovStart) && c.Before(ovStart.Add(o.shift))
			if repeated && !ev.Scheduled.Before(o.transition) {
				t.Fatalf("%s fired at its second instance after its first was jumped over:\n%s", c, dump(h.snapshot()))
			}
		}
		for c, n := range seen {
			if n != 1 {
				t.Fatalf("%s ran %d times", c, n)
			}
		}
	})
}

func TestDSTOverlap_LongOverlapPerSecondKeepsTheFirstPass(t *testing.T) {
	// a 3h fall-back (Antarctica/Casey, +11 -> +08) with a per-second job started 10s
	// before the transition: the remaining seconds of the first pass run, and the
	// search does not run out of steps (A5-01)
	o := findOverlap(t, "Antarctica/Casey", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	if o.shift < 3*time.Hour {
		t.Fatalf("precondition: expected a 3h overlap, got %s", o.shift)
	}
	start := o.transition.Add(-10 * time.Second)
	h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.advance(12 * time.Second)
	var got []time.Time
	for _, ev := range h.snapshot() {
		if ev.Kind == core.EventJobStart {
			got = append(got, ev.Scheduled)
		}
	}
	// the 9 remaining seconds of the first pass, then the repeats whose first
	// instant passed before the start (the second pass begins with them)
	if len(got) < 9 || !got[0].Equal(start.Add(time.Second)) || !got[8].Equal(o.transition.Add(-time.Second)) {
		t.Fatalf("runs %v:\n%s", got, dump(h.snapshot()))
	}
}

func TestDSTOverlap_LongOverlapPerSecondStartedEarly(t *testing.T) {
	// the same 3h overlap, a per-second job started long before it: near the end of
	// the first pass every repeat ahead was met while running (suppressed). The
	// search skips them in one step instead of 10800, and the first pass goes on.
	o := findOverlap(t, "Antarctica/Casey", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	start := o.transition.Add(-o.shift - 5*time.Second)
	h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.clock.Advance(o.shift) // one long stall: now 5s before the transition
	h.waitLoop()
	h.waitEvents("the stall handled", func(evs []core.SchedulerEvent) bool { return running(evs) == 0 })
	h.advance(4 * time.Second)
	evs := h.snapshot()
	for _, want := range []time.Duration{3, 2, 1} {
		if eventsAt(evs, core.EventJobStart, "a", o.transition.Add(-want*time.Second)) != 1 {
			t.Fatalf("the first pass did not go on at transition-%ds:\n%s", want, dump(evs[max(0, len(evs)-12):]))
		}
	}
}

func TestDSTOverlap_LongOverlapSecondPassDoesNotStopTheJob(t *testing.T) {
	// 3h overlap (Casey), per-second job started 5s into the first pass; a stall
	// to just before the transition, then on through the second pass: the repeats
	// met while running are skipped in one step and the job keeps running after
	// the overlap (F6-01: it used to run out of search steps and stop for good)
	o := findOverlap(t, "Antarctica/Casey", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	start := o.transition.Add(-o.shift + 5*time.Second)
	h := newHarness(t, start, o.loc, 4, jobSpec{name: "a", cron: "* * * * * *", command: "true"})
	h.clock.Advance(o.transition.Sub(start) - time.Second) // stall to the last second of the first pass
	h.waitLoop()
	h.waitEvents("the stall handled", func(evs []core.SchedulerEvent) bool { return running(evs) == 0 })
	h.clock.Advance(o.shift) // the whole second pass in one stall: to the end of the overlap
	h.waitLoop()
	h.waitEvents("the stall handled", func(evs []core.SchedulerEvent) bool { return running(evs) == 0 })
	h.advance(3 * time.Second) // after the overlap
	evs := h.snapshot()
	end := o.transition.Add(o.shift)
	after := 0
	for _, ev := range evs {
		if ev.Kind == core.EventJobStart && ev.Scheduled.After(end) {
			after++
		}
	}
	if after < 2 {
		t.Fatalf("the job stopped: %d runs after the overlap:\n%s", after, dump(evs[max(0, len(evs)-10):]))
	}
}
