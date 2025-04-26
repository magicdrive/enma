package model

import (
	"fmt"
	"syscall"
)

type SignalName string

// Set validates and sets the signal name
func (s *SignalName) Set(value string) error {
	if _, ok := signalMap[value]; ok {
		*s = SignalName(value)
		return nil
	}
	return fmt.Errorf("invalid signal name: %q. Allowed values are %v", value, AllowedSignals())
}

// Signal returns the syscall.Signal corresponding to the SignalName
func (s *SignalName) Signal() syscall.Signal {
	if sig, ok := signalMap[s.String()]; ok {
		return sig
	}
	panic(fmt.Sprintf("invalid signal name: %q. Allowed values are %v", s.String(), AllowedSignals()))
}

// String returns the string representation of the signal name
func (s *SignalName) String() string {
	return string(*s)
}

// allowedSignals returns a list of valid signal names
func AllowedSignals() []string {
	var list []string
	for name := range signalMap {
		list = append(list, name)
	}
	return list
}
