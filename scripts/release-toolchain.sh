#!/bin/sh
# Verifies the Go toolchain the release binaries were built with
# (`go version -m`): every binary under the given directory must have been
# built with the toolchain pinned in hephaestus.yaml (builds[].env.GOTOOLCHAIN),
# and that toolchain must not be older than the one of the last GoReleaser
# release (go1.27.2, v2.1.0). Anything else fails.
#
# Why: the release must not depend on which Go happens to be first in PATH.
# Hephaestus v1.1.0's action put the Go of its own go.mod there (go1.26.0,
# with GOTOOLCHAIN=local) and would have built enma with an older Go than
# before; from v1.2.0 the action sets up no Go, and the job's own setup-go
# (the newest Go) would build with whatever was released last. The pin and
# this check keep the release toolchain explicit either way.
#
# The newest Go release (https://go.dev/dl/) is reported as well, as a
# warning when the pin is behind it (Renovate proposes the update); that part
# never fails, so a Go release does not block an enma release.
#
# usage: scripts/release-toolchain.sh BIN-DIR   (e.g. dist/bin)
set -eu
dir=${1:?usage: scripts/release-toolchain.sh BIN-DIR}
floor=go1.27.2 # v2.1.0
here=$(cd "$(dirname "$0")/.." && pwd)
fail() {
	echo "release-toolchain: $*" >&2
	[ -z "${GITHUB_ACTIONS:-}" ] || echo "::error title=Release toolchain::$*"
	exit 1
}

pin=$(sed -n 's/^[[:space:]]*GOTOOLCHAIN:[[:space:]]*"\{0,1\}\(go[0-9][0-9.]*\)"\{0,1\}[[:space:]]*$/\1/p' "$here/hephaestus.yaml")
case "$pin" in go1.*.*) ;; *) fail "no single GOTOOLCHAIN: go1.X.Y in hephaestus.yaml (got '$pin')" ;; esac

# ver_ge A B: Go version A (go1.X.Y) >= B
ver_ge() {
	awk -v a="${1#go}" -v b="${2#go}" 'BEGIN {
		split(a, x, "."); split(b, y, ".")
		for (i = 1; i <= 3; i++) { if (x[i] + 0 > y[i] + 0) exit 0; if (x[i] + 0 < y[i] + 0) exit 1 }
		exit 0 }'
}
ver_ge "$pin" "$floor" || fail "the pinned $pin is older than $floor, the toolchain of v2.1.0"

n=0
for bin in "$dir"/*/enma "$dir"/*/enma.exe; do
	[ -f "$bin" ] || continue
	built=$(go version -m "$bin" | awk 'NR == 1 { print $NF }')
	[ "$built" = "$pin" ] || fail "$bin was built with ${built:-an unknown Go}, not $pin (hephaestus.yaml GOTOOLCHAIN)"
	n=$((n + 1))
done
[ "$n" -gt 0 ] || fail "no enma binary under $dir"
msg="$n release binaries built with $pin (pinned; at least $floor)"
echo "ok   $msg"

newest=$(curl -fsSL --max-time 30 'https://go.dev/dl/?mode=json' 2>/dev/null | sed -n 's/.*"version": *"\(go[0-9.]*\)".*/\1/p' | head -1) || newest=
echo "     newest Go release: ${newest:-unknown}"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	echo "### Go toolchain: $msg; newest Go release: ${newest:-unknown}" >>"$GITHUB_STEP_SUMMARY"
fi
if [ -n "$newest" ] && ! ver_ge "$pin" "$newest"; then
	echo "::warning title=Release toolchain::$pin is pinned; the newest Go release is $newest (update GOTOOLCHAIN in hephaestus.yaml)"
fi
exit 0
