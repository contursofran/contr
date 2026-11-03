#!/bin/sh
# Fails when docs/DECISIONS.md has two rows with the same id (| Dn |).
# Usage: check-decisions.sh [repo-dir]  (default: this repository)
set -eu
cd "${1:-$(dirname "$0")/..}"
dups=$(grep -oE '^\| D[0-9]+ \|' docs/DECISIONS.md | sort | uniq -d || true)
if [ -n "$dups" ]; then
	echo "docs/DECISIONS.md: duplicate decision ids:"
	echo "$dups"
	exit 1
fi
