package model

import (
	"fmt"
	"syscall"
)

type SignalName string

func (s *SignalName) Set(value string) error {
	if _, ok := signalMap[value]; ok {
		*s = SignalName(value)
		return nil
	}
	return fmt.Errorf("invalid signal name: %q. Allowed values are %v", value, AllowedSignals())
}

func (s *SignalName) Signal() syscall.Signal {
	if sig, ok := signalMap[s.String()]; ok {
		return sig
	}
	panic(fmt.Sprintf("invalid signal name: %q. Allowed values are %v", s.String(), AllowedSignals()))
}

func (s *SignalName) String() string {
	return string(*s)
}

func AllowedSignals() []string {
	var list []string
	for name := range signalMap {
		list = append(list, name)
	}
	return list
}
