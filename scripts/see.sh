#!/bin/sh
# Shows the app in a known state (#457, D178). It starts the app on a fixture
# against a scratch hc core, presses the given labels in order, and writes
# the window as tmp/see/<fixture>-<theme>.png and its elements as
# tmp/see/<fixture>-<theme>.txt. Check the data in the .txt; open the PNG
# for layout and color. It never touches the owner's hc core.
#
#   scripts/see.sh                                    list the fixtures
#   scripts/see.sh <fixture> [dark|light|both] [label ...]
#
# A label matches an element's whole label, else the start of one.
# "type:<text>" types text; "wheel:<dy>" scrolls the chat by dy points once it
# rests at its end; "scroll:<label>:<dy>" scrolls that element by dy points;
# "wait:<label>" fails the run when that element does not show.
# CONTR_E2E_HINTS=1 shows the key chips; CONTR_E2E_WIDTH=<n> opens the window that wide.
# The default theme is both.
set -eu
cd "$(dirname "$0")/.."

fixture=${1:-}
[ $# -gt 0 ] && shift
theme=both
case "${1:-}" in
dark | light | both)
	theme=$1
	shift
	;;
esac
press=$(printf '%s\n' "$@")

mkdir -p tmp/see
log=tmp/see/log.txt
status=0
CONTR_SEE="$fixture" CONTR_SEE_THEME="$theme" CONTR_SEE_PRESS="$press" \
	go test -tags e2e -count=1 -run '^TestSee$' -v ./cmd/contr >"$log" 2>&1 || status=$?

# The "see:" lines of the test, without the file:line prefix, and its errors.
sed -n 's/^ *e2e_see_test\.go:[0-9]*: see: //p' "$log"
if [ "$status" -ne 0 ]; then
	grep -v '^=== RUN\|^ *e2e_see_test\.go:[0-9]*: see: ' "$log" | tail -40 >&2
	exit "$status"
fi
if grep -q -- '--- SKIP' "$log" && [ -n "$fixture" ]; then
	grep -A2 -- '--- SKIP' "$log" >&2
	exit 1
fi
