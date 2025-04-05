package model

import (
	"fmt"
	"strings"
)

const (
	Foreground = "foreground"
	Background = "background"
)

type Scenery string

func (m *Scenery) Set(value string) error {
	switch strings.ToLower(value) {
	case Foreground, Background:
		*m = Scenery(value)
		return nil
	default:
		return fmt.Errorf("invalid value: %s. Allowed values are 'foreground', 'background'", value)
	}
}
func (m *Scenery) String() string {
	return string(*m)
}
