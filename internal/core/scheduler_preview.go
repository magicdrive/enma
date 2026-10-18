package core

import (
	"fmt"
	"io"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
)

// JobPreview is the upcoming fire times of one scheduler job.
type JobPreview struct {
	Job   string
	Cron  string
	Fires []time.Time // in the scheduler's time zone
}

// PreviewFires returns the next n fire times of each job, as a scheduler started
// at from would compute them: the same calculation (advance) the scheduler runs,
// so time-zone transitions are handled the same way. Jobs keep the config order;
// each job's fire times ascend. Nothing is run.
func PreviewFires(opt *option.SchedulerOption, from time.Time, n int) []JobPreview {
	loc := previewLocation(opt)
	r := &SchedulerRunner{Options: opt, started: from}
	out := make([]JobPreview, 0, len(opt.Jobs))
	for i := range opt.Jobs {
		js := &jobState{job: &opt.Jobs[i]}
		p := JobPreview{Job: js.job.Name, Cron: js.job.CronExpr}
		r.advance(js, from, loc)
		for len(p.Fires) < n && js.hasNext {
			p.Fires = append(p.Fires, js.nextAt.In(loc))
			r.advance(js, js.nextAt, loc)
		}
		out = append(out, p)
	}
	return out
}

func previewLocation(opt *option.SchedulerOption) *time.Location {
	if opt.Location != nil {
		return opt.Location
	}
	return time.Local
}

// WritePreview prints a preview: the time zone, then each job and its fire times.
func WritePreview(w io.Writer, opt *option.SchedulerOption, from time.Time, n int) {
	loc := previewLocation(opt)
	unit := "fire times"
	if n == 1 {
		unit = "fire time"
	}
	fmt.Fprintf(w, "Next %d %s per job (timezone %s, from %s):\n", n, unit, ZoneLabel(loc, from), from.In(loc).Format(previewLayout))
	for _, p := range PreviewFires(opt, from, n) {
		fmt.Fprintf(w, "  job %q (cron %q):\n", p.Job, p.Cron)
		for _, at := range p.Fires {
			fmt.Fprintf(w, "    %s\n", at.Format(previewLayout))
		}
	}
}

// ZoneLabel names a time zone for people: its name, and for the OS local zone
// (named "Local") also the abbreviation and UTC offset in effect at t.
func ZoneLabel(loc *time.Location, t time.Time) string {
	if loc.String() != "Local" {
		return loc.String()
	}
	in := t.In(loc)
	abbr, _ := in.Zone()
	return fmt.Sprintf("Local (%s, %s)", abbr, in.Format("-0700"))
}

const previewLayout = "2006-01-02 15:04:05 -0700 MST"
