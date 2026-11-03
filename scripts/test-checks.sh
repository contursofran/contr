#!/bin/bash
# Test of check-conflicts.sh and check-decisions.sh on small temporary repos.
set -euo pipefail
unset CDPATH
repo="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$repo/tmp"
tmp="$(cd "$(mktemp -d "$repo/tmp/test-checks.XXXXXX")" && pwd)"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "test-checks.sh: FAIL: $*" >&2
	exit 1
}

# new_repo <name>: a clean repo with one decisions file.
new_repo() {
	local d="$tmp/$1"
	mkdir -p "$d/docs"
	git -C "$d" init -q
	printf '| D1 | a |\n| D2 | b |\n' >"$d/docs/DECISIONS.md"
	printf 'Title\n=======\n' >"$d/notes.md"
	git -C "$d" add -A
	echo "$d"
}

expect_ok() { "$@" >/dev/null 2>&1 || fail "expected pass: $*"; }
expect_fail() { if "$@" >/dev/null 2>&1; then fail "expected failure: $*"; fi; }

c="$repo/scripts/check-conflicts.sh"
d="$repo/scripts/check-decisions.sh"

clean=$(new_repo clean)
expect_ok "$c" "$clean"
expect_ok "$d" "$clean"

# Markers are built here so this file holds no marker line itself.
for m in '<<<<<<<' '|||||||' '>>>>>>>'; do
	r=$(new_repo "m${#m}${m:0:1}x$RANDOM")
	printf 'a\n%s HEAD\nb\n' "$m" >"$r/code.go"
	git -C "$r" add -A
	expect_fail "$c" "$r"
done

# An untracked file is not checked; a lone ======= line is allowed (clean has one).
r=$(new_repo untracked)
printf '%s x\n' '<<<<<<<' >"$r/new.txt"
expect_ok "$c" "$r"

r=$(new_repo dup)
printf '| D2 | again |\n' >>"$r/docs/DECISIONS.md"
expect_fail "$d" "$r"

# A D1 in a later column is not a row id.
r=$(new_repo notdup)
printf '| D3 | see | D1 | x |\n' >>"$r/docs/DECISIONS.md"
expect_ok "$d" "$r"

echo "test-checks.sh: ok"
