// Package cronspec parses enma's cron expressions and finds their next civil
// (wall-clock) time.
//
// An expression has exactly six fields, separated by spaces or tabs:
//
//	second minute hour day-of-month month day-of-week
//	0-59   0-59   0-23 1-31         1-12  0-6 (0 = Sunday)
//
// Each field is a comma-separated list of items; an item is `*`, `*/step`,
// `n`, `a-b` or `a-b/step` (decimal numbers only). Nothing else is accepted:
// no 5-field form, no names (JAN, SUN), no `?`, `L`, `W`, `#`, no `@every` or
// other descriptors, no `n/step`.
//
// When both day-of-month and day-of-week are restricted (neither starts with
// `*`), a day matches if either matches (standard cron OR); otherwise both must.
//
// Times here are civil: a time.Time in UTC stands for a wall-clock reading in
// whatever time zone the caller uses, so daylight saving never shows up.
package cronspec

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed expression.
type Schedule struct {
	second, minute, hour, dom, month, dow uint64
	domStar, dowStar                      bool
}

type field struct {
	name     string
	min, max int
}

var fields = [6]field{
	{"second", 0, 59},
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 6},
}

// searchYears bounds NextCivil. Every satisfiable expression fires within it
// (Feb 29 recurs within 8 years); one that does not never fires.
const searchYears = 9

// Parse parses a 6-field expression. It also rejects an expression that can
// never fire (such as February 30).
func Parse(expr string) (*Schedule, error) {
	parts := strings.FieldsFunc(expr, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(parts) != 6 {
		return nil, fmt.Errorf("cron %q: want 6 fields (second minute hour day-of-month month day-of-week), got %d", expr, len(parts))
	}
	var s Schedule
	bits := [6]*uint64{&s.second, &s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, p := range parts {
		b, err := parseField(p, fields[i])
		if err != nil {
			return nil, fmt.Errorf("cron %q: %w", expr, err)
		}
		*bits[i] = b
	}
	s.domStar = strings.HasPrefix(parts[3], "*")
	s.dowStar = strings.HasPrefix(parts[5], "*")
	if _, ok := s.NextCivil(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)); !ok {
		return nil, fmt.Errorf("cron %q: never fires", expr)
	}
	return &s, nil
}

func parseField(text string, f field) (uint64, error) {
	var bits uint64
	for item := range strings.SplitSeq(text, ",") {
		b, err := parseItem(item, f)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", f.name, err)
		}
		bits |= b
	}
	return bits, nil
}

func parseItem(item string, f field) (uint64, error) {
	if item == "" {
		return 0, errors.New("empty list item")
	}
	rangePart, stepPart, hasStep := strings.Cut(item, "/")
	lo, hi := f.min, f.max
	switch {
	case rangePart == "*":
	case strings.Contains(rangePart, "-"):
		a, b, _ := strings.Cut(rangePart, "-")
		var err error
		if lo, err = number(a, f); err != nil {
			return 0, err
		}
		if hi, err = number(b, f); err != nil {
			return 0, err
		}
		if lo > hi {
			return 0, fmt.Errorf("range %q: start is after end", rangePart)
		}
	default:
		if hasStep {
			return 0, fmt.Errorf("%q: a step needs `*` or a range before it", item)
		}
		n, err := number(rangePart, f)
		if err != nil {
			return 0, err
		}
		lo, hi = n, n
	}
	step := 1
	if hasStep {
		n, err := strconv.Atoi(stepPart)
		if err != nil || !isDigits(stepPart) || n < 1 {
			return 0, fmt.Errorf("step %q: want a positive number", stepPart)
		}
		step = n
	}
	var bits uint64
	for v := lo; v <= hi; v += step {
		bits |= 1 << uint(v)
	}
	return bits, nil
}

func number(s string, f field) (int, error) {
	if !isDigits(s) {
		return 0, fmt.Errorf("%q: want a number %d-%d", s, f.min, f.max)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < f.min || n > f.max {
		return 0, fmt.Errorf("%q: out of range %d-%d", s, f.min, f.max)
	}
	return n, nil
}

func isDigits(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func has(bits uint64, v int) bool { return bits&(1<<uint(v)) != 0 }

func (s *Schedule) dayMatches(t time.Time) bool {
	dom, dow := has(s.dom, t.Day()), has(s.dow, int(t.Weekday()))
	if s.domStar || s.dowStar {
		return dom && dow
	}
	return dom || dow
}

// Matches reports whether the civil time c (seconds precision) matches.
func (s *Schedule) Matches(c time.Time) bool {
	return has(s.month, int(c.Month())) && s.dayMatches(c) && has(s.hour, c.Hour()) &&
		has(s.minute, c.Minute()) && has(s.second, c.Second())
}

// NextCivil returns the first matching civil time strictly after the civil
// time after (sub-seconds dropped), and false if there is none within
// searchYears.
func (s *Schedule) NextCivil(after time.Time) (time.Time, bool) {
	a := after.UTC()
	t := time.Date(a.Year(), a.Month(), a.Day(), a.Hour(), a.Minute(), a.Second(), 0, time.UTC).Add(time.Second)
	limit := t.Year() + searchYears
	for t.Year() <= limit {
		switch {
		case !has(s.month, int(t.Month())):
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
		case !has(s.hour, t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
		case !has(s.minute, t.Minute()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()+1, 0, 0, time.UTC)
		case !has(s.second, t.Second()):
			t = t.Add(time.Second)
		default:
			return t, true
		}
	}
	return time.Time{}, false
}

// Civil returns the wall-clock reading of t in its location as a civil time.
func Civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// Instants returns the instants at which the wall clock of loc reads the civil
// time c, earliest first: none when c falls in a daylight-saving gap, two when
// it is repeated after the clocks go back.
func Instants(c time.Time, loc *time.Location) []time.Time {
	naive := time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, time.UTC)
	var out []time.Time
	seen := map[int]bool{}
	// zone offsets in effect around c (a transition is never more than a day away from both)
	for _, probe := range []time.Time{naive.Add(-36 * time.Hour), naive, naive.Add(36 * time.Hour)} {
		_, off := probe.In(loc).Zone()
		if seen[off] {
			continue
		}
		seen[off] = true
		u := naive.Add(-time.Duration(off) * time.Second)
		if _, uo := u.In(loc).Zone(); uo == off && Civil(u.In(loc)).Equal(naive) {
			out = append(out, u)
		}
	}
	for i := 1; i < len(out); i++ { // at most 3 entries
		for j := i; j > 0 && out[j].Before(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// GapEnd returns the first civil time after the gap that the non-existent
// civil time c falls in (the wall clock reading at the instant the gap ends).
// For a c that exists it returns c.
func GapEnd(c time.Time, loc *time.Location) time.Time {
	naive := time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, time.UTC)
	if len(Instants(c, loc)) > 0 {
		return naive
	}
	// read c with the offset in effect before the gap: that instant is after the
	// gap, and the wall clock there is where the gap ends (or later)
	_, before := naive.Add(-36 * time.Hour).In(loc).Zone()
	u := naive.Add(-time.Duration(before) * time.Second)
	// step back to the transition, so that nothing after the gap is skipped
	for d := time.Duration(1<<17) * time.Second; d >= time.Second; d /= 2 { // 2^17 s > 36h, down to 1s
		if v := u.Add(-d); Civil(v.In(loc)).After(naive) {
			u = v
		}
	}
	return Civil(u.In(loc))
}

// MaxCivilBetween returns the latest wall-clock reading of loc at the instants
// in (a, b] (b's reading when the clocks never go back in between). Every
// local date-time up to it happened in (a, b] or before.
func MaxCivilBetween(a, b time.Time, loc *time.Location) time.Time {
	best := Civil(b.In(loc))
	t := b
	for range 10000 { // zone periods walked back; enough for centuries
		start, _ := t.In(loc).ZoneBounds()
		if start.IsZero() || !start.After(a) {
			break
		}
		before := start.Add(-time.Second) // the last whole second of the previous period
		if !before.After(a) {
			break
		}
		if c := Civil(before.In(loc)); c.After(best) {
			best = c
		}
		t = before
	}
	return best
}

// MinCivilFrom returns the earliest wall-clock reading of loc at the instants
// in [t, t+36h): when the clocks go back soon after t, local date-times
// earlier than t's reading still lie ahead.
func MinCivilFrom(t time.Time, loc *time.Location) time.Time {
	best := Civil(t.In(loc))
	cur := t
	for range 4 {
		_, end := cur.In(loc).ZoneBounds()
		if end.IsZero() || end.Sub(t) >= 36*time.Hour {
			break
		}
		if c := Civil(end.In(loc)); c.Before(best) {
			best = c
		}
		cur = end
	}
	return best
}
