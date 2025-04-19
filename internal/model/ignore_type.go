package model

import (
	"fmt"

	"github.com/magicdrive/enma/internal/textbank"
)

const (
	Minimal = "minimal"
	Maximum = "maximum"
	Nothing = "nothing"
)

var ignoreTypeUnitMap = map[string]string{
	"minimal": Minimal,
	"min":     Minimal,
	"mini":    Minimal,
	"maximum": Maximum,
	"max":     Maximum,
	"nothing": Nothing,
	"none":    Nothing,
	"no":      Nothing,
}

type IgnoreType string

func (m *IgnoreType) Set(value string) error {
	if unit, ok := ignoreTypeUnitMap[value]; ok {
		*m = IgnoreType(unit)
		return nil
	} else {
		return fmt.Errorf("invalid value: %q. Allowed values are 'max', 'min', 'none'", value)
	}
}

func (m *IgnoreType) EnmaignoreText() string {
	value := m.String()
	switch value {
	case Maximum:
		return textbank.MaximumEnmaIgnore
	case Minimal:
		return textbank.MinimalEnmaIgnore
	case Nothing:
		return ""
	default:
		panic(fmt.Sprintf("invalid value: %q. Allowed values are 'max', 'min', 'none'", value))
	}
}

func (m *IgnoreType) String() string {
	return string(*m)
}
