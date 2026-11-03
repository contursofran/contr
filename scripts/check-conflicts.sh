#!/bin/sh
# Fails when a tracked text file holds merge-conflict markers.
# A lone "=======" line is not tested: it is valid Markdown. Every conflict
# block also has a start and an end marker, and those are tested.
# Usage: check-conflicts.sh [repo-dir]  (default: this repository)
set -eu
cd "${1:-$(dirname "$0")/..}"
found=$(git grep -nIE '^(<{7} |\|{7} |>{7} )' || true)
if [ -n "$found" ]; then
	echo "merge-conflict markers found:"
	echo "$found"
	exit 1
fi
