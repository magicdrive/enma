package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var timeUnitMap = map[string]string{
	"ns":  "ns",
	"us":  "us",
	"ms":  "ms",
	"s":   "s",
	"sec": "s",
	"m":   "m",
	"min": "m",
	"h":   "h",
	"d":   "h",
}

const TimeStringRegexpString = `^(\d+(\.\d+)?)(ns|us|ms|s|sec|m|min|h|d)$`

var TimeStringRegexp = regexp.MustCompile(TimeStringRegexpString)

type TimeString string

func (m *TimeString) Set(value string) error {
	if res := TimeStringRegexp.MatchString(value); res {
		*m = TimeString(value)
		return nil
	} else {
		return fmt.Errorf("invalid value: %q. Allowed values must match regexp %s", value, TimeStringRegexpString)
	}
}

func (m *TimeString) String() string {
	return string(*m)
}

func (m *TimeString) TimeDuration() (time.Duration, error) {
	matches := TimeStringRegexp.FindStringSubmatch(m.String())
	if len(matches) != 4 {
		return 0, errors.New("invalid duration format")
	}

	numStr := matches[1] // e.g. "1.5"
	unit := matches[3]   // e.g. "s"

	normalizedUnit, ok := timeUnitMap[unit]
	if !ok {
		return 0, errors.New("unsupported unit")
	}

	if unit == "d" {
		fval, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		hours := fval * 24
		return time.ParseDuration(fmt.Sprintf("%fh", hours))
	}

	return time.ParseDuration(fmt.Sprintf("%s%s", numStr, normalizedUnit))
}
