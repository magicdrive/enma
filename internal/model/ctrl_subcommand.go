package model

import (
	"fmt"
	"strings"
)

const (
	KillCmd    = "kill"
	RestartCmd = "restart"
	ShowCmd    = "show"
	PidCmd     = "pid"
	TailCmd    = "tail"
	ListCmd    = "list"
)

type CtrlSubcommand string

func (m *CtrlSubcommand) Set(value string) error {
	switch strings.ToLower(value) {
	case KillCmd, RestartCmd, ShowCmd, PidCmd, TailCmd, ListCmd:
		*m = CtrlSubcommand(value)
		return nil
	default:
		return fmt.Errorf("invalid value: %s. Allowed values are 'kill', 'restart', 'show', 'pid', 'tail', 'list'", value)
	}
}
func (m *CtrlSubcommand) String() string {
	return string(*m)
}
