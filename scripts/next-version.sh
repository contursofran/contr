#!/bin/bash
# Prints the version of the next release (D115).
#
#   scripts/next-version.sh [VERSION]
#
# With VERSION (X.Y.Z, no "v"), prints it after a check that no tag vVERSION exists. Without it,
# prints the patch after the newest tag vX.Y.Z, or the first line of the VERSION file when the
# repository has no such tag yet. The release workflow does not commit VERSION, so a release push
# starts no second release (D115).
# Errors go to stderr as "next-version.sh: <message>" with exit 1.

set -euo pipefail
unset CDPATH

die() {
    echo "next-version.sh: $*" >&2
    exit 1
}

repo="$(cd "$(dirname "$0")/.." && pwd)"
semver='^[0-9]+\.[0-9]+\.[0-9]+$'

tags="$(git -C "$repo" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true)"

if [ $# -gt 1 ]; then die "too many arguments"; fi
if [ $# -eq 1 ] && [ -n "$1" ]; then
    echo "$1" | grep -Eq "$semver" || die "not a version (X.Y.Z): $1"
    if echo "$tags" | grep -qx "v$1"; then die "the tag v$1 exists"; fi
    echo "$1"
    exit 0
fi

latest="$(echo "$tags" | sed -n 1p)"
if [ -z "$latest" ]; then
    first="$(sed -n 1p "$repo/VERSION" 2>/dev/null)" || die "cannot read $repo/VERSION"
    echo "$first" | grep -Eq "$semver" || die "no version in VERSION: '$first'"
    echo "$first"
    exit 0
fi
v="${latest#v}"
major="${v%%.*}"
rest="${v#*.}"
minor="${rest%%.*}"
patch="${rest#*.}"
echo "$major.$minor.$((patch + 1))"
