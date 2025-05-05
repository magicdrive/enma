package common

var version string

func Version() string {
	if version != "" {
		return version
	}

	return "version unknown"
}

func SetVersion(v string) {
	version = v
}
