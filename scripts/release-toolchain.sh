#!/bin/sh
# Reports the Go toolchain a release binary was built with, next to the newest
# Go release (https://go.dev/dl/), in the job log and the job summary.
#
# The Hephaestus action sets up the Go of its own go.mod (its `toolchain`
# line) and that Go builds enma, so the release toolchain follows the pinned
# Hephaestus version, not `go-version: stable`. Until v2.1.0 (GoReleaser),
# releases were built with the newest Go release. An older toolchain is a
# warning annotation, not a failure: update Hephaestus (Renovate proposes it)
# to move the release toolchain forward.
#
# usage: scripts/release-toolchain.sh BINARY
set -u
bin=${1:?usage: scripts/release-toolchain.sh BINARY}
built=$(go version "$bin" | awk '{print $NF}') || exit 1
case "$built" in go1.*) ;; *) echo "release-toolchain: cannot read the Go version of $bin" >&2; exit 1 ;; esac
newest=$(curl -fsSL --max-time 30 'https://go.dev/dl/?mode=json' | sed -n 's/.*"version": *"\(go[0-9.]*\)".*/\1/p' | head -1)
msg="release binaries are built with $built (newest Go release: ${newest:-unknown})"
echo "$msg"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	echo "### Go toolchain: $msg" >>"$GITHUB_STEP_SUMMARY"
fi
if [ -n "$newest" ] && [ "$built" != "$newest" ]; then
	echo "::warning title=Release toolchain::$msg"
fi
exit 0
