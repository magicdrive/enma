#!/bin/sh
# Reports the known limitation G4-04 after the tests (README "What is watched":
# on macOS / BSD a directory created while enma runs can rarely miss files).
# The tests append one line per run that creates directories to
# $ENMA_G404_LOG: "... observed=0" or "... observed=1 <details>". This prints
# the count to the job log and the job summary, and adds a warning annotation
# when it was observed. It never fails the job: the limitation is accepted and
# monitored.
set -u
log=${ENMA_G404_LOG:-}
runs=0
seen=0
if [ -n "$log" ] && [ -f "$log" ] && [ -r "$log" ] && [ -s "$log" ]; then
	runs=$(grep -c '' "$log" || true) # also counts a last line without a newline
	seen=$(grep -c ' observed=1 ' "$log" || true)
fi
case "$runs" in '' | *[!0-9]*) runs=0 ;; esac
case "$seen" in '' | *[!0-9]*) seen=0 ;; esac
if [ "$runs" -eq 0 ]; then
	msg="G4-04 (known limitation on macOS / BSD, see README \"What is watched\"): no test run recorded (the tests were skipped, cached or did not run)"
else
	msg="G4-04 (known limitation on macOS / BSD, see README \"What is watched\"): observed in $seen of $runs test runs"
fi
echo "$msg"
if [ "$runs" -eq 0 ] && [ "$(uname -s)" = Darwin ]; then
	echo "::warning title=G4-04 not recorded::$msg"
fi
if [ "$seen" -gt 0 ]; then
	grep ' observed=1 ' "$log"
	echo "::warning title=G4-04 known limitation::$msg"
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo "### $msg"
		if [ "$seen" -gt 0 ]; then
			echo '```'
			grep ' observed=1 ' "$log"
			echo '```'
		fi
	} >>"$GITHUB_STEP_SUMMARY"
fi
exit 0
