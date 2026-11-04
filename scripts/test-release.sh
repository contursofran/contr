#!/bin/bash
# Test of scripts/next-version.sh and scripts/release-notes.sh on a small temporary repository.
set -euo pipefail
unset CDPATH

repo="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$repo/tmp"
tmp="$(cd "$(mktemp -d "$repo/tmp/test-release.XXXXXX")" && pwd)"
trap 'rm -rf "$tmp"' EXIT

fail() {
    echo "test-release.sh: FAIL: $*" >&2
    exit 1
}

mkdir -p "$tmp/scripts"
cp "$repo/scripts/next-version.sh" "$repo/scripts/release-notes.sh" "$tmp/scripts/"
printf '0.1.0\n' >"$tmp/VERSION"
cd "$tmp"
git init -q .
git config user.email t@example.com
git config user.name t
git config commit.gpgsign false
commit() {
    echo "$1" >>file
    git add -A
    git commit -q -m "$1"
}
next() { scripts/next-version.sh "$@" 2>"$tmp/err"; }

commit "first (#1)"
[ "$(next)" = 0.1.0 ] || fail "with no tag, the version is the VERSION file's"
git tag v0.1.0
[ "$(next)" = 0.1.1 ] || fail "after v0.1.0 the next is 0.1.1"
git tag v0.1.9
git tag v0.1.10
[ "$(next)" = 0.1.11 ] || fail "tags sort as versions: after v0.1.10 the next is 0.1.11, got $(next)"
git tag v1.2.3-beta.1
[ "$(next)" = 0.1.11 ] || fail "a prerelease tag must not count"
[ "$(next 2.0.0)" = 2.0.0 ] || fail "an input version is used"
if next 0.1.9 >/dev/null; then fail "an existing tag must fail"; fi
grep -q '^next-version.sh: the tag v0.1.9 exists' "$tmp/err" || fail "existing tag message: $(cat "$tmp/err")"
if next v2.0.0 >/dev/null; then fail "a version with v must fail"; fi
grep -q '^next-version.sh: not a version' "$tmp/err" || fail "bad version message: $(cat "$tmp/err")"

# Notes: the pull requests since the newest tag.
git tag v0.2.0
commit "Add settings (#10)"
commit "Fix the sidebar (#11)"
notes="$(scripts/release-notes.sh)"
[ "$notes" = "$(printf -- '- Fix the sidebar (#11)\n- Add settings (#10)')" ] || fail "notes: $notes"
[ "$(scripts/release-notes.sh v0.1.0 | wc -l | tr -d ' ')" = 2 ] || fail "notes since v0.1.0 should list 2 changes"
git tag v0.3.0
[ "$(scripts/release-notes.sh)" = "No changes listed." ] || fail "no change should say so"
if scripts/release-notes.sh v9.9.9 2>"$tmp/err"; then fail "a missing tag must fail"; fi
grep -q '^release-notes.sh: no tag v9.9.9' "$tmp/err" || fail "missing tag message"

# The workflow keeps its rules (D115): it runs on pushes to main with Markdown-only pushes ignored,
# on dispatch, and fails when the signing secret is missing.
wf="$repo/.github/workflows/release.yml"
grep -q "branches: \[main\]" "$wf" || fail "release.yml must run on pushes to main"
grep -qF '"**/*.md"' "$wf" || fail "release.yml must ignore Markdown-only pushes"
grep -q "workflow_dispatch" "$wf" || fail "release.yml must keep workflow_dispatch"
grep -q "MYGO_UPDATER_PRIVATE_KEY is not set" "$wf" || fail "release.yml must fail clearly without the secret"
if grep -q "git push origin HEAD\|git commit" "$wf"; then fail "release.yml must not commit (it would start a second release)"; fi
echo "test-release.sh: ok"
