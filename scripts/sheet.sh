#!/bin/sh
# Draws each spec'd component under its spec image, in Dark and Light
# (#458, D179), and writes tmp/sheet/<part>-<theme>.png. It needs no window
# and no hc. Compare the two by eye: it is not a pixel test (D5).
#
#   scripts/sheet.sh                   every part
#   scripts/sheet.sh AgentRow ...      only these parts
#
# Each package under internal/app with spec'd parts has a TestSpecSheet in its
# sheet_test.go that gives its parts to apptest.SpecSheet (internal/app/apptest/sheet.go).
set -eu
cd "$(dirname "$0")/.."

parts=all
if [ $# -gt 0 ]; then
	parts=$(
		IFS=,
		echo "$*"
	)
fi

mkdir -p tmp/sheet
log=tmp/sheet/log.txt
status=0
CONTR_SHEET="$parts" go test -count=1 -run '^TestSpecSheet$' -v ./internal/app/... >"$log" 2>&1 || status=$?

# The "sheet:" lines of the test, without the file:line prefix.
sed -n 's/^ *sheet_test\.go:[0-9]*: sheet: //p' "$log"
if [ "$status" -ne 0 ]; then
	grep -v '^=== RUN\|^ *sheet_test\.go:[0-9]*: sheet: /' "$log" | tail -40 >&2
	exit "$status"
fi
# A part name that no package has.
if [ "$parts" != all ]; then
	for p in $(echo "$parts" | tr ',' ' '); do
		if ! grep -q "sheet: .*/$p-dark\.png\$" "$log"; then
			echo "sheet: no part \"$p\"" >&2
			status=1
		fi
	done
fi
exit "$status"
