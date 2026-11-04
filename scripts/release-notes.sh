#!/bin/bash
# Prints the release notes in Markdown: one bullet for each pull request merged since a tag.
#
#   scripts/release-notes.sh [SINCE_TAG]
#
# Merges to main are squash merges, so the first-parent log has one line for each pull request,
# with its number. Without SINCE_TAG, the newest tag vX.Y.Z is used; with no tag, all commits.
# Errors go to stderr as "release-notes.sh: <message>" with exit 1.

set -euo pipefail
unset CDPATH

repo="$(cd "$(dirname "$0")/.." && pwd)"
since="${1:-}"
if [ -z "$since" ]; then
    since="$(git -C "$repo" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed -n 1p || true)"
fi
range=HEAD
if [ -n "$since" ]; then
    git -C "$repo" rev-parse --verify --quiet "refs/tags/$since" >/dev/null ||
        { echo "release-notes.sh: no tag $since" >&2; exit 1; }
    range="$since..HEAD"
fi

lines="$(git -C "$repo" log --first-parent --no-merges --format='%s' "$range" | sed 's/^/- /')"
if [ -z "$lines" ]; then
    echo "No changes listed."
else
    echo "$lines"
fi
