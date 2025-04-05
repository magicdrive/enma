package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dlclark/regexp2"
)

var unitMap = map[string]string{
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

const TimeStringRegexpString = `^(?<num>\d+(\.\d+)?)(?<unit>ns|us|ms|s|sec|m|min|h|d)$`

var TimeStringRegexp = regexp2.MustCompile(TimeStringRegexpString, 0)

type TimeString string

func (m *TimeString) Set(value string) error {
	if res, _ := TimeStringRegexp.MatchString(value); res {
		*m = TimeString(value)
		return nil
	} else {
		return fmt.Errorf("invalid value: %s. Allowed values must match regexp %s", value, TimeStringRegexpString)
	}
}

func (m *TimeString) String() string {
	return string(*m)
}

func (m *TimeString) TimeDuration() (time.Duration, error) {
	match, err := TimeStringRegexp.FindStringMatch(m.String())
	if err != nil {
		return 0, err
	}
	if match == nil {
		return 0, errors.New("invalid duration format")
	}

	numStr := match.GroupByName("num").String()
	unit := match.GroupByName("unit").String()

	normalizedUnit, ok := unitMap[unit]
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
