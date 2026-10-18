//go:build !windows

package core

// awaitReadySh: see ready_sh_test.go (the same helper for the internal tests).
func awaitReadySh(ready string) string {
	return `i=0; while [ ! -e '` + ready + `' ] && [ $i -lt 1000 ]; do sleep 0.01; i=$((i+1)); done; ` +
		`[ -e '` + ready + `' ] || echo "awaitReadySh: ` + ready + ` did not appear (the helper process is not ready)" >&2`
}
