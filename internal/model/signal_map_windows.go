//go:build windows
// +build windows

package model

import "syscall"

const (
	SignalTerm = "SIGTERM"
	SignalKill = "SIGKILL"
	SignalHup  = "SIGHUP"
	SignalInt  = "SIGINT"
)

var signalMap = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGKILL": syscall.SIGKILL,
	"SIGHUP":  syscall.SIGHUP,
	"SIGINT":  syscall.SIGINT,
}
