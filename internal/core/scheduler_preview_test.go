//go:build !windows

package core_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
	"github.com/magicdrive/enma/internal/cronspec"
)

// The preview (enma config validate --next, enma scheduler --next) lists the
// fire times the scheduler itself would use: the same calculation, so the
// times match what a scheduler started at the same moment runs, also across
// time-zone transitions.

// findGap returns a local date-time (minute precision) skipped when the clocks
// of loc go forward after from, and the transition instant.
func findGap(t *testing.T, loc *time.Location, from time.Time) (time.Time, time.Time) {
	t.Helper()
	cur := from.In(loc)
	for range 20 {
		_, end := cur.ZoneBounds()
		if end.IsZero() {
			break
		}
		_, before := cur.Zone()
		_, after := end.In(loc).Zone()
		if after > before {
			shift := time.Duration(after-before) * time.Second
			c := cronspec.Civil(end.In(loc)).Add(-shift / 2).Truncate(time.Minute)
			if len(cronspec.Instants(c, loc)) != 0 {
				t.Fatalf("%s: %s should not exist", loc, c)
			}
			return c, end
		}
		cur = end.In(loc)
	}
	t.Fatalf("%s: no spring-forward transition after %s", loc, from)
	return time.Time{}, time.Time{}
}

// matchEngine checks that the preview from start equals the first attempts the
// scheduler (virtual clock, started at start) makes, job by job.
func matchEngine(t *testing.T, start time.Time, loc *time.Location, n int, specs ...jobSpec) {
	t.Helper()
	h := newHarness(t, start, loc, 4, specs...)
	preview := core.PreviewFires(h.r.Options, start, n)
	if len(preview) != len(specs) {
		t.Fatalf("preview has %d jobs, want %d", len(preview), len(specs))
	}
	var last time.Time
	for i, p := range preview {
		if p.Job != specs[i].name || len(p.Fires) != n {
			t.Fatalf("preview %d: job %q with %d fires, want %q with %d", i, p.Job, len(p.Fires), specs[i].name, n)
		}
		if l := p.Fires[n-1]; l.After(last) {
			last = l
		}
	}
	h.advance(last.Sub(start) + time.Minute)
	evs := h.snapshot()
	for _, p := range preview {
		var ran []time.Time
		for _, ev := range evs {
			if ev.Kind == core.EventJobStart && ev.Job == p.Job && !ev.Scheduled.After(p.Fires[n-1]) {
				ran = append(ran, ev.Scheduled)
			}
		}
		if len(ran) != len(p.Fires) {
			t.Fatalf("job %s: preview %v, scheduler ran %v:\n%s", p.Job, p.Fires, ran, dump(evs))
		}
		for i := range ran {
			if !ran[i].Equal(p.Fires[i]) {
				t.Fatalf("job %s fire %d: preview %s, scheduler %s:\n%s", p.Job, i, p.Fires[i], ran[i], dump(evs))
			}
		}
	}
}

func TestPreview_MatchesSchedulerAcrossFallBack(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		// the repeated local time fires once (its first instance); hourly fires
		// go on by the instant, through the repeated hour
		matchEngine(t, o.first.Add(-3*time.Hour), o.loc, 4,
			jobSpec{name: "daily", cron: dailyCron(o.civil), command: "true"},
			jobSpec{name: "hourly", cron: "0 0 * * * *", command: "true"})
	})
}

func TestPreview_MatchesSchedulerAcrossSpringForward(t *testing.T) {
	for _, z := range overlapZones {
		t.Run(z, func(t *testing.T) {
			loc, err := time.LoadLocation(z)
			if err != nil {
				t.Fatal(err)
			}
			gap, transition := findGap(t, loc, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			matchEngine(t, transition.Add(-3*time.Hour), loc, 3,
				jobSpec{name: "in-gap", cron: dailyCron(gap), command: "true"},
				jobSpec{name: "hourly", cron: "0 0 * * * *", command: "true"})
		})
	}
}

// A scheduler started between the two instances of a repeated local time fires
// the second one; so does the preview from that moment.
func TestPreview_StartedInsideOverlapListsSecondInstance(t *testing.T) {
	forZones(t, func(t *testing.T, o overlap) {
		matchEngine(t, o.first.Add(time.Minute), o.loc, 2, jobSpec{name: "daily", cron: dailyCron(o.civil), command: "true"})
	})
}

// Fires every second or two, and a start just before or exactly on a fire time:
// the preview lists exactly what the scheduler runs (audits H5A-03, H5B-02).
func TestPreview_MatchesSchedulerEverySecondAndAtTheBoundary(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 30, 0, time.UTC)
	for name, start := range map[string]time.Time{
		"half a second before a fire": base.Add(500 * time.Millisecond),
		"exactly on a fire":           base, // nothing runs at start, even then
		"a nanosecond after a fire":   base.Add(time.Nanosecond),
	} {
		t.Run(name, func(t *testing.T) {
			matchEngine(t, start, time.UTC, 6,
				jobSpec{name: "every-second", cron: "* * * * * *", command: "true"},
				jobSpec{name: "every-2s", cron: "*/2 * * * * *", command: "true"})
		})
	}
	p := core.PreviewFires(schedOpt(t, time.UTC, "* * * * * *"), base.Add(500*time.Millisecond), 1)
	if got := p[0].Fires[0]; !got.Equal(base.Add(time.Second)) {
		t.Fatalf("first fire after 10:00:30.5 is %s, want 10:00:31", got)
	}
}

func TestZoneLabel(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got := core.ZoneLabel(tokyo, at); got != "Asia/Tokyo" {
		t.Fatalf("named zone: %q", got)
	}
	local := time.FixedZone("Local", 9*3600) // what time.Local looks like: named "Local"
	if got := core.ZoneLabel(local, at); got != "Local (Local, +0900)" {
		t.Fatalf("local zone: %q", got)
	}
	if got := core.ZoneLabel(time.Local, at); !strings.HasPrefix(got, time.Local.String()) {
		t.Fatalf("time.Local: %q", got)
	}
}

func schedOpt(t *testing.T, loc *time.Location, crons ...string) *option.SchedulerOption {
	t.Helper()
	opt := &option.SchedulerOption{Location: loc, MaxConcurrent: 4}
	for i, c := range crons {
		s, err := cronspec.Parse(c)
		if err != nil {
			t.Fatal(err)
		}
		opt.Jobs = append(opt.Jobs, option.SchedulerJob{Name: string(rune('a' + i)), CronExpr: c, Cron: s, Command: "true"})
	}
	return opt
}

func TestPreview_OrderAndCount(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 0, 30, 0, time.UTC)
	got := core.PreviewFires(schedOpt(t, time.UTC, "0 0 12 * * *", "0 * * * * *", "0 0 0 29 2 *"), from, 3)
	want := [][]string{
		{"2026-03-01T12:00:00Z", "2026-03-02T12:00:00Z", "2026-03-03T12:00:00Z"},
		{"2026-03-01T10:01:00Z", "2026-03-01T10:02:00Z", "2026-03-01T10:03:00Z"},
		{"2028-02-29T00:00:00Z", "2032-02-29T00:00:00Z", "2036-02-29T00:00:00Z"}, // leap years only
	}
	if len(got) != 3 {
		t.Fatalf("jobs: %v", got)
	}
	for i, p := range got {
		if p.Job != string(rune('a'+i)) {
			t.Fatalf("job %d is %q: config order expected", i, p.Job)
		}
		var s []string
		for _, f := range p.Fires {
			s = append(s, f.UTC().Format(time.RFC3339))
		}
		if strings.Join(s, ",") != strings.Join(want[i], ",") {
			t.Fatalf("job %s: %v, want %v", p.Job, s, want[i])
		}
	}
	if p := core.PreviewFires(schedOpt(t, time.UTC, "0 * * * * *"), from, 0); len(p[0].Fires) != 0 {
		t.Fatalf("n=0 listed %v", p[0].Fires)
	}
	// the same input gives the same output
	a := core.PreviewFires(schedOpt(t, time.UTC, "*/7 * * * * *"), from, 50)
	b := core.PreviewFires(schedOpt(t, time.UTC, "*/7 * * * * *"), from, 50)
	for i := range a[0].Fires {
		if !a[0].Fires[i].Equal(b[0].Fires[i]) {
			t.Fatal("preview is not deterministic")
		}
	}
}

func TestWritePreview_NamesTimeZoneAndOffsets(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	core.WritePreview(&b, schedOpt(t, loc, "0 30 9 * * *", "0 0 0 29 2 *"), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 2)
	out := b.String()
	for _, s := range []string{"Next 2 fire times per job (timezone Asia/Tokyo, from 2026-03-01 09:00:00 +0900 JST)", `job "a" (cron "0 30 9 * * *")`,
		"2026-03-01 09:30:00 +0900 JST", "2026-03-02 09:30:00 +0900 JST", `job "b"`, "2028-02-29 00:00:00 +0900 JST"} {
		if !strings.Contains(out, s) {
			t.Fatalf("missing %q in:\n%s", s, out)
		}
	}
	b.Reset()
	core.WritePreview(&b, schedOpt(t, loc, "0 30 9 * * *"), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 1)
	if !strings.HasPrefix(b.String(), "Next 1 fire time per job (") {
		t.Fatalf("singular: %q", b.String())
	}
}
