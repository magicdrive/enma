//go:build !windows
// +build !windows

package model

import "syscall"

const (
	SignalTerm = "SIGTERM"
	SignalKill = "SIGKILL"
	SignalHup  = "SIGHUP"
	SignalUsr1 = "SIGUSR1"
	SignalUsr2 = "SIGUSR2"
	SignalInt  = "SIGINT"
)

var signalMap = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGKILL": syscall.SIGKILL,
	"SIGHUP":  syscall.SIGHUP,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
	"SIGINT":  syscall.SIGINT,
}
