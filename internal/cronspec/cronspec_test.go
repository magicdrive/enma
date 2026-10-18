package cronspec_test

import (
	"strings"
	"testing"
	"time"

	_ "time/tzdata" // fixed zone data for the DST cases

	"github.com/magicdrive/enma/internal/cronspec"
)

func civil(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

func mustParse(t *testing.T, expr string) *cronspec.Schedule {
	t.Helper()
	s, err := cronspec.Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return s
}

// nexts returns the first n civil times after from.
func nexts(t *testing.T, expr, from string, n int) []string {
	t.Helper()
	s := mustParse(t, expr)
	var out []string
	c := civil(from)
	for range n {
		next, ok := s.NextCivil(c)
		if !ok {
			t.Fatalf("%q: no next after %s", expr, c)
		}
		out = append(out, next.Format("2006-01-02 15:04:05 Mon"))
		c = next
	}
	return out
}

func TestParse_Accepts(t *testing.T) {
	for _, expr := range []string{
		"* * * * * *",
		"0 */10 * * * *",
		"0 0 2 * * *",
		"0,30 0-5,10 1-23/2 1,15,31 1-12/3 0-6",
		"59 59 23 31 12 6",
		"0 0 0 29 2 *",
		"  0\t0 0 * * 0  ",
		"*/1 * * * * *",
		"0 0 12 1-7 * 1",
	} {
		if _, err := cronspec.Parse(expr); err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
		}
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]string{
		"":                       "want 6 fields",
		"* * * * *":              "want 6 fields", // 5-field cron
		"* * * * * * *":          "want 6 fields",
		"60 * * * * *":           "out of range",
		"* 60 * * * *":           "out of range",
		"* * 24 * * *":           "out of range",
		"* * * 0 * *":            "out of range",
		"* * * 32 * *":           "out of range",
		"* * * * 0 *":            "out of range",
		"* * * * 13 *":           "out of range",
		"* * * * * 7":            "out of range",  // Sunday is 0 only
		"* * * ? * *":            "want a number", // Quartz
		"* * * * * ?":            "want a number",
		"* * * L * *":            "want a number",
		"* * * 15W * *":          "want a number",
		"* * * * * 1#2":          "want a number",
		"* * * * * 5L":           "want a number",
		"@every 1m":              "want 6 fields",
		"@hourly":                "want 6 fields",
		"* * * * JAN *":          "want a number",
		"* * * * * SUN":          "want a number",
		"5/15 * * * * *":         "needs `*` or a range",
		"*/0 * * * * *":          "positive",
		"*/x * * * * *":          "positive",
		"1-2/-1 * * * * *":       "positive",
		"10-5 * * * * *":         "start is after end",
		"1,,2 * * * * *":         "empty list item",
		", * * * * *":            "empty list item",
		"-1 * * * * *":           "want a number",
		"+1 * * * * *":           "want a number",
		"1- * * * * *":           "want a number",
		"0 0 0 30 2 *":           "never fires",
		"0 0 0 31 4,6,9,11 *":    "never fires",
		"0x1 * * * * *":          "want a number",
		"1.5 * * * * *":          "want a number",
		"99999999999 * * * * *":  "want a number",
		"* * * * * * extra-text": "want 6 fields",
	}
	for expr, want := range cases {
		_, err := cronspec.Parse(expr)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want error containing %q", expr, err, want)
		}
	}
}

func TestNextCivil_Sequences(t *testing.T) {
	cases := []struct {
		expr, from string
		want       []string
	}{
		{"0 */10 * * * *", "2026-10-16 10:05:30", []string{"2026-10-16 10:10:00 Fri", "2026-10-16 10:20:00 Fri", "2026-10-16 10:30:00 Fri"}},
		{"*/20 * * * * *", "2026-10-16 10:00:00", []string{"2026-10-16 10:00:20 Fri", "2026-10-16 10:00:40 Fri", "2026-10-16 10:01:00 Fri"}},
		{"0,30 15-16 * * * *", "2026-10-16 15:59:30", []string{"2026-10-16 16:15:00 Fri", "2026-10-16 16:15:30 Fri", "2026-10-16 16:16:00 Fri"}},
		{"0 0 2 * * *", "2026-10-16 02:00:00", []string{"2026-10-17 02:00:00 Sat", "2026-10-18 02:00:00 Sun"}},
		// day of week, Sunday = 0
		{"0 0 9 * * 0", "2026-10-16 00:00:00", []string{"2026-10-18 09:00:00 Sun", "2026-10-25 09:00:00 Sun"}},
		{"0 0 9 * * 1-5/2", "2026-10-16 00:00:00", []string{"2026-10-16 09:00:00 Fri", "2026-10-19 09:00:00 Mon", "2026-10-21 09:00:00 Wed"}},
		// both restricted: day-of-month OR day-of-week
		{"0 0 0 13 * 5", "2026-10-01 00:00:00", []string{"2026-10-02 00:00:00 Fri", "2026-10-09 00:00:00 Fri", "2026-10-13 00:00:00 Tue", "2026-10-16 00:00:00 Fri"}},
		// `*/n` counts as unrestricted: AND
		{"0 0 0 */2 * 5", "2026-10-01 00:00:00", []string{"2026-10-09 00:00:00 Fri", "2026-10-23 00:00:00 Fri", "2026-11-13 00:00:00 Fri"}},
		// month ends, year end, leap years
		{"0 0 0 31 * *", "2026-01-31 00:00:00", []string{"2026-03-31 00:00:00 Tue", "2026-05-31 00:00:00 Sun", "2026-07-31 00:00:00 Fri"}},
		{"59 59 23 31 12 *", "2026-06-01 00:00:00", []string{"2026-12-31 23:59:59 Thu", "2027-12-31 23:59:59 Fri"}},
		{"0 0 0 29 2 *", "2026-01-01 00:00:00", []string{"2028-02-29 00:00:00 Tue", "2032-02-29 00:00:00 Sun"}},
		{"0 0 0 1 1 *", "2026-12-31 23:59:59", []string{"2027-01-01 00:00:00 Fri"}},
		{"0 0 0 29 2 *", "2096-03-01 00:00:00", []string{"2104-02-29 00:00:00 Fri"}}, // 2100 is not a leap year
	}
	for _, tc := range cases {
		got := nexts(t, tc.expr, tc.from, len(tc.want))
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%q from %s:\n got %v\nwant %v", tc.expr, tc.from, got, tc.want)
		}
	}
}

func TestNextCivil_IsStrictlyAfter(t *testing.T) {
	s := mustParse(t, "* * * * * *")
	from := civil("2026-10-16 10:00:00").Add(999 * time.Millisecond)
	next, _ := s.NextCivil(from)
	if want := civil("2026-10-16 10:00:01"); !next.Equal(want) {
		t.Fatalf("got %s, want %s", next, want)
	}
}

func TestInstants_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08 02:00 EST -> 03:00 EDT: 02:30 does not exist
	if got := cronspec.Instants(civil("2026-03-08 02:30:00"), ny); len(got) != 0 {
		t.Fatalf("gap: got %v, want none", got)
	}
	// 2026-11-01 02:00 EDT -> 01:00 EST: 01:30 happens twice, an hour apart
	got := cronspec.Instants(civil("2026-11-01 01:30:00"), ny)
	if len(got) != 2 || got[1].Sub(got[0]) != time.Hour {
		t.Fatalf("overlap: got %v", got)
	}
	if _, off := got[0].In(ny).Zone(); off != -4*3600 {
		t.Fatalf("first occurrence should be EDT, got offset %d", off)
	}
	// an ordinary time has exactly one instant
	if got := cronspec.Instants(civil("2026-07-01 12:00:00"), ny); len(got) != 1 || got[0].In(ny).Hour() != 12 {
		t.Fatalf("ordinary: got %v", got)
	}
	// half-hour transition (Lord Howe: +10:30 <-> +11)
	lh, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatal(err)
	}
	if got := cronspec.Instants(civil("2026-10-04 02:15:00"), lh); len(got) != 0 {
		t.Fatalf("Lord Howe gap: got %v", got)
	}
	if got := cronspec.Instants(civil("2026-04-05 01:45:00"), lh); len(got) != 2 || got[1].Sub(got[0]) != 30*time.Minute {
		t.Fatalf("Lord Howe overlap: got %v", got)
	}
}

func TestMatches_AgreesWithNextCivil(t *testing.T) {
	s := mustParse(t, "*/7 3-50/9 * 1-10,20 * 2,4")
	c := civil("2026-01-01 00:00:00")
	next, _ := s.NextCivil(c)
	for x := c.Add(time.Second); x.Before(next); x = x.Add(time.Second) {
		if s.Matches(x) {
			t.Fatalf("%s matches but NextCivil skipped it", x)
		}
	}
	if !s.Matches(next) {
		t.Fatalf("NextCivil returned %s, which does not match", next)
	}
}

// FuzzParse: Parse never panics; an accepted expression has 6 fields, fires,
// and NextCivil's result matches and is strictly later.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"* * * * * *", "0 */10 * * * *", "0 0 0 29 2 *", "1-5/2,7 * * * * 0", "* * * ? * *", "@every 1m", "0 0 0 30 2 *"} {
		f.Add(s, int64(0))
	}
	f.Fuzz(func(t *testing.T, expr string, offset int64) {
		s, err := cronspec.Parse(expr)
		if err != nil {
			return
		}
		if n := len(strings.Fields(expr)); n != 6 {
			t.Fatalf("accepted %q with %d fields", expr, n)
		}
		for _, r := range expr {
			if !strings.ContainsRune("0123456789*,-/ \t", r) {
				t.Fatalf("accepted %q with %q", expr, r)
			}
		}
		from := civil("2026-01-01 00:00:00").Add(time.Duration(offset%(400*24*3600)) * time.Second)
		next, ok := s.NextCivil(from)
		if !ok {
			t.Fatalf("%q accepted but never fires after %s", expr, from)
		}
		if !next.After(from) || !s.Matches(next) {
			t.Fatalf("%q: NextCivil(%s) = %s", expr, from, next)
		}
	})
}

func TestGapEnd(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	apia, _ := time.LoadLocation("Pacific/Apia")
	lh, _ := time.LoadLocation("Australia/Lord_Howe")
	cases := []struct {
		loc      *time.Location
		in, want string
	}{
		{ny, "2026-03-08 02:30:00", "2026-03-08 03:00:00"},
		{ny, "2026-03-08 02:00:00", "2026-03-08 03:00:00"},
		{ny, "2026-03-08 02:59:59", "2026-03-08 03:00:00"},
		{ny, "2026-07-01 12:00:00", "2026-07-01 12:00:00"}, // exists
		{apia, "2011-12-30 13:00:00", "2011-12-31 00:00:00"},
		{lh, "2026-10-04 02:15:00", "2026-10-04 02:30:00"},
	}
	for _, tc := range cases {
		if got := cronspec.GapEnd(civil(tc.in), tc.loc); !got.Equal(civil(tc.want)) {
			t.Errorf("GapEnd(%s, %s) = %s, want %s", tc.in, tc.loc, got, tc.want)
		}
	}
}

func TestMinMaxCivil(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	syd, _ := time.LoadLocation("Australia/Sydney")
	at := func(loc *time.Location, s string, off int) time.Time {
		c := civil(s)
		return time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, time.FixedZone("", off))
	}
	// 01:45 EDT on the fall-back day: the wall clock will read 01:00 (EST) within the hour
	if got := cronspec.MinCivilFrom(at(ny, "2026-11-01 01:45:00", -4*3600), ny); !got.Equal(civil("2026-11-01 01:00:00")) {
		t.Errorf("MinCivilFrom NY = %s", got)
	}
	// an ordinary time: its own reading
	if got := cronspec.MinCivilFrom(at(ny, "2026-07-01 12:00:00", -4*3600), ny); !got.Equal(civil("2026-07-01 12:00:00")) {
		t.Errorf("MinCivilFrom ordinary = %s", got)
	}
	// spring forward ahead: readings only grow
	if got := cronspec.MinCivilFrom(at(ny, "2026-03-08 01:30:00", -5*3600), ny); !got.Equal(civil("2026-03-08 01:30:00")) {
		t.Errorf("MinCivilFrom spring = %s", got)
	}
	// from 00:50 EDT to 01:20 EST the wall clock reached 01:59:59 (EDT)
	a, b := at(ny, "2026-11-01 00:50:00", -4*3600), at(ny, "2026-11-01 01:20:00", -5*3600)
	if got := cronspec.MaxCivilBetween(a, b, ny); !got.Equal(civil("2026-11-01 01:59:59")) {
		t.Errorf("MaxCivilBetween NY = %s", got)
	}
	if got := cronspec.MaxCivilBetween(b.Add(-time.Minute), b, ny); !got.Equal(civil("2026-11-01 01:20:00")) {
		t.Errorf("MaxCivilBetween inside = %s", got)
	}
	// Sydney falls back in April (AEDT +11 -> AEST +10 at 03:00)
	a, b = at(syd, "2026-04-05 02:30:00", 11*3600), at(syd, "2026-04-05 02:10:00", 10*3600)
	if got := cronspec.MaxCivilBetween(a, b, syd); !got.Equal(civil("2026-04-05 02:59:59")) {
		t.Errorf("MaxCivilBetween Sydney = %s", got)
	}
}

// TestDayFields pins the day-of-month / day-of-week rule with concrete dates:
// a field is restricted unless it starts with `*` (so `*/2` is unrestricted and
// `1-31/2` is restricted); when both are restricted a day matches if either does,
// otherwise both must.
func TestDayFields(t *testing.T) {
	cases := []struct {
		expr, from string
		want       string
	}{
		// only DOW restricted: Mondays
		{"0 0 9 * * 1", "2026-10-31 00:00:00", "11-02 Mon,11-09 Mon,11-16 Mon,11-23 Mon,11-30 Mon,12-07 Mon"},
		// only DOM restricted: the 1st
		{"0 0 9 1 * *", "2026-10-31 00:00:00", "11-01 Sun,12-01 Tue,01-01 Fri,02-01 Mon,03-01 Mon,04-01 Thu"},
		// both restricted: the 1st OR Mondays
		{"0 0 9 1 * 1", "2026-10-31 00:00:00", "11-01 Sun,11-02 Mon,11-09 Mon,11-16 Mon,11-23 Mon,11-30 Mon,12-01 Tue,12-07 Mon"},
		// `*/2` starts with `*`: unrestricted, so AND: odd days that are Mondays
		{"0 0 9 */2 * 1", "2026-10-31 00:00:00", "11-09 Mon,11-23 Mon,12-07 Mon,12-21 Mon,01-11 Mon,01-25 Mon,02-01 Mon,02-15 Mon"},
		// `1-31/2` is restricted: odd days OR Mondays
		{"0 0 9 1-31/2 * 1", "2026-10-31 00:00:00", "10-31 Sat,11-01 Sun,11-02 Mon,11-03 Tue,11-05 Thu,11-07 Sat,11-09 Mon,11-11 Wed"},
		// the same days, different spellings: `*/2` alone and `1-31/2` alone are equal
		{"0 0 9 */2 * *", "2026-10-31 00:00:00", "10-31 Sat,11-01 Sun,11-03 Tue,11-05 Thu,11-07 Sat,11-09 Mon"},
		{"0 0 9 1-31/2 * *", "2026-10-31 00:00:00", "10-31 Sat,11-01 Sun,11-03 Tue,11-05 Thu,11-07 Sat,11-09 Mon"},
		// leap year: Feb 29 OR Mondays (2028)
		{"0 0 9 29 2 1", "2028-01-31 00:00:00", "02-07 Mon,02-14 Mon,02-21 Mon,02-28 Mon,02-29 Tue,02-05 Mon"},
		// month end: the 31st OR Fridays across a 30-day month
		{"0 0 9 31 * 5", "2026-10-31 00:00:00", "10-31 Sat,11-06 Fri,11-13 Fri,11-20 Fri,11-27 Fri,12-04 Fri"},
		// `*/2` in day-of-week is unrestricted: with DOM restricted, AND (the 1st on Sun/Tue/Thu/Sat)
		{"0 0 9 1 * */2", "2026-10-31 00:00:00", "11-01 Sun,12-01 Tue,04-01 Thu,05-01 Sat,06-01 Tue"},
		// `*/2` in day-of-week is unrestricted too: with DOM `*` every even weekday
		{"0 0 9 * * */2", "2026-10-31 00:00:00", "10-31 Sat,11-01 Sun,11-03 Tue,11-05 Thu,11-07 Sat,11-08 Sun"},
	}
	for _, tc := range cases {
		want := strings.Split(tc.want, ",")
		got := nexts(t, tc.expr, tc.from, len(want))
		for i := range got {
			got[i] = got[i][5:10] + got[i][19:]
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q from %s:\n got %v\nwant %v", tc.expr, tc.from, got, want)
		}
	}
}
