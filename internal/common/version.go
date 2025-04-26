package common

import (
	"runtime/debug"
)

var version string

func Version() string {
	if version != "" {
		return version
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		return buildInfo.Main.Version
	}
	return "version unknown"
}
