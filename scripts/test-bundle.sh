#!/bin/bash
# Test of scripts/bundle.sh and scripts/mygo-config.sh. The bundle runs are real `mygo build` runs
# (D115), so the first one needs the terminal library in the user cache (it downloads it when missing).
# Every path it writes is under a dir in tmp/ of the repository. HOME points into that dir.
set -euo pipefail
unset CDPATH # with CDPATH set, cd prints the directory and breaks $(cd ... && pwd)

repo="$(cd "$(dirname "$0")/.." && pwd)"
bundle="$repo/scripts/bundle.sh"
mkdir -p "$repo/tmp"
tmp="$(cd "$(mktemp -d "$repo/tmp/test-bundle.XXXXXX")" && pwd)"
# The build must keep the real Go caches and the real user cache (the terminal library is in it),
# so those are fixed before HOME moves into tmp/.
export GOMODCACHE="$(go env GOMODCACHE)" GOCACHE="$(go env GOCACHE)"
real_cache="$HOME/Library/Caches"
export HOME="$tmp/home"
mkdir -p "$HOME/Library"
ln -s "$real_cache" "$HOME/Library/Caches"
trap 'rm -rf "$tmp"' EXIT

fail() {
    echo "test-bundle.sh: FAIL: $*" >&2
    exit 1
}

version="$(sed -n 1p "$repo/VERSION")"
case "$version" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) fail "no version in VERSION: '$version'" ;;
esac

# Runs bundle.sh with the given arguments. Output goes to $tmp/run.out and $tmp/run.err.
run_bundle() {
    "$bundle" "$@" >"$tmp/run.out" 2>"$tmp/run.err"
}

# Runs bundle.sh, which must fail. Stderr must say "bundle.sh: " then the pattern, and no bundle may appear.
expect_failure() {
    local pattern="$1"
    shift
    if run_bundle "$@"; then
        fail "bundle.sh $* should fail"
    fi
    grep -q "^bundle.sh: .*$pattern" "$tmp/run.err" ||
        fail "bundle.sh $* should say '$pattern' on stderr, got: $(cat "$tmp/run.err")"
    [ ! -e "$tmp/err-out/contr.app" ] || fail "bundle.sh $* left a contr.app behind"
    [ ! -e "$tmp/err-out/contr.app.tmp" ] || fail "bundle.sh $* left contr.app.tmp behind"
}

plist_value() {
    plutil -extract "$1" raw -o - "$2" 2>/dev/null || true
}

expect_plist() {
    local got
    got="$(plist_value "$1" "$plist")"
    [ "$got" = "$2" ] || fail "$1 is '$got', expected '$2'"
}

# mygo-config.sh, in a copy of the repository files it reads: the version, and the update settings
# only when scripts/update.pub holds a key (MyGo refuses an empty key).
fake="$tmp/fake"
mkdir -p "$fake/scripts"
cp "$repo/scripts/mygo-config.sh" "$fake/scripts/"
printf '1.2.3\n' >"$fake/VERSION"
: >"$fake/scripts/update.pub"
config() { "$fake/scripts/mygo-config.sh" 2>"$tmp/cfg.err"; }
json_get() { python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d.get(k) if isinstance(d,dict) else None
print("" if d is None else d)' "$1"; }
[ "$(config | json_get version)" = 1.2.3 ] || fail "mygo.json should carry the version of VERSION"
[ "$(config | json_get identifier)" = dev.contr ] || fail "mygo.json should carry the identifier dev.contr"
[ -z "$(config | json_get updates)" ] || fail "an empty update key should leave out updates"
printf 'RWQexamplekey+/=\n' >"$fake/scripts/update.pub"
[ "$(config | json_get updates.publicKey)" = 'RWQexamplekey+/=' ] || fail "the update key should reach mygo.json"
[ "$(config | json_get updates.github)" = contursofran/contr ] || fail "updates.github should be contursofran/contr"
printf 'not a key!\n' >"$fake/scripts/update.pub"
if config >/dev/null; then fail "a bad update key should fail"; fi
grep -q '^mygo-config.sh: scripts/update.pub is not a public key' "$tmp/cfg.err" ||
    fail "a bad update key should say so, got: $(cat "$tmp/cfg.err")"
: >"$fake/scripts/update.pub"
printf 'dev\n' >"$fake/VERSION"
if config >/dev/null; then fail "a bad VERSION should fail"; fi
grep -q '^mygo-config.sh: no version in VERSION' "$tmp/cfg.err" || fail "a bad VERSION should say so"

# The committed icon is the 1024 px PNG that MyGo needs.
px="$(sips -g pixelWidth "$repo/assets/app/icon.png" | awk '/pixelWidth/ { print $2 }')"
[ "$px" = 1024 ] || fail "assets/app/icon.png is ${px}px, expected 1024"

# First run: --zip. Stdout is the app path, then the zip path.
if ! run_bundle --out "$tmp/out" --zip; then
    cat "$tmp/run.err" >&2
    fail "bundle.sh failed"
fi
app="$tmp/out/contr.app"
zip="$tmp/out/contr-$version-macos-arm64.zip"
[ "$(wc -l <"$tmp/run.out" | tr -d ' ')" = 2 ] ||
    fail "stdout should be 2 lines, got: $(cat "$tmp/run.out")"
[ "$(sed -n 1p "$tmp/run.out")" = "$app" ] ||
    fail "stdout line 1 should be $app, got: $(sed -n 1p "$tmp/run.out")"
[ "$(sed -n 2p "$tmp/run.out")" = "$zip" ] ||
    fail "stdout line 2 should be $zip, got: $(sed -n 2p "$tmp/run.out")"
[ -f "$zip" ] || fail "zip not written: $zip"

# The zip keeps the executable bit and the signature (the CI artifact is this zip).
ditto -x -k "$zip" "$tmp/unzipped" || fail "ditto cannot unpack $zip"
[ -x "$tmp/unzipped/contr.app/Contents/MacOS/contr" ] || fail "the zip lost the executable bit"
codesign --verify --strict "$tmp/unzipped/contr.app" >/dev/null 2>&1 ||
    fail "the zip lost the signature"

plist="$app/Contents/Info.plist"
plutil -lint "$plist" >/dev/null || fail "Info.plist does not lint"
expect_plist CFBundleIdentifier dev.contr
expect_plist CFBundleExecutable contr
expect_plist CFBundleShortVersionString "$version"
expect_plist CFBundleVersion "$version"
expect_plist CFBundlePackageType APPL
min="$(plist_value LSMinimumSystemVersion "$plist")"
case "$min" in
    [0-9]*.[0-9]*) ;;
    *) fail "LSMinimumSystemVersion is '$min', expected a version" ;;
esac
# ContrCommit is the short HEAD hash, or "unknown" outside git (same as mygo-config.sh).
expected_commit="$(git -C "$repo" rev-parse --short HEAD 2>/dev/null || echo unknown)"
expect_plist ContrCommit "$expected_commit"

[ -x "$app/Contents/MacOS/contr" ] || fail "Contents/MacOS/contr is not executable"
icon="$(plist_value CFBundleIconFile "$plist")"
[ -n "$icon" ] && [ -f "$app/Contents/Resources/$icon" ] || fail "the icon file '$icon' is not in Resources"

# The terminal library is in Resources (D93) and is the one the app loads.
[ -f "$app/Contents/Resources/libghostty-vt.dylib" ] || fail "the app has no libghostty-vt.dylib in Resources"
[ "$("$app/Contents/MacOS/contr" terminal-check)" = "$app/Contents/Resources/libghostty-vt.dylib" ] ||
    fail "terminal-check does not print the library in Resources"

codesign --verify --strict "$app" >/dev/null 2>&1 || fail "codesign --verify --strict fails"
signing="$(codesign -dv "$app" 2>&1 || true)"
case "$signing" in
    *Signature=adhoc*) ;;
    *) fail "the bundle is not ad-hoc signed: $signing" ;;
esac
case "$signing" in
    *Identifier=dev.contr*) ;;
    *) fail "the signature identifier is not dev.contr: $signing" ;;
esac

# Second run over the same --out: replaces the bundle, leaves no staging dir.
touch "$app/stale-file"
if ! run_bundle --out "$tmp/out"; then
    cat "$tmp/run.err" >&2
    fail "the second run failed"
fi
[ "$(cat "$tmp/run.out")" = "$app" ] || fail "second run: stdout should be only $app"
[ ! -e "$app/stale-file" ] || fail "second run kept the old bundle"
[ ! -e "$tmp/out/contr.app.tmp" ] || fail "second run left contr.app.tmp behind"
codesign --verify --strict "$app" >/dev/null 2>&1 || fail "second run: signature does not verify"

# Error paths. Each one fails with "bundle.sh: <message>" and writes no bundle.
expect_failure "unknown argument" --nope
expect_failure "needs a dir" --out
expect_failure "needs a dir, not a flag" --out --zip

# --out must not reach a protected dir: the rm -rf removes <out>/contr.app. The physical path is
# checked, so a symlink to one is refused too. Each run fails before anything is removed.
ln -s /Applications "$tmp/apps-link"
ln -s "$HOME" "$tmp/home-link"
expect_failure "needs a dir, not an empty value" --out ""
expect_failure "a protected dir" --out /
expect_failure "a protected dir" --out /Applications
expect_failure "a protected dir" --out /applications
expect_failure "a protected dir" --out "$HOME"
expect_failure "a protected dir" --out "$HOME/"
expect_failure "a protected dir" --out "$tmp/apps-link"
expect_failure "a protected dir" --out "$tmp/home-link"

# A command that is not guarded and fails (here a ditto that exits 2) must end bundle.sh with
# exit 1, print the ERR trap message, and leave no staging dir.
mkdir -p "$tmp/fakebin"
printf '#!/bin/sh\nexit 2\n' >"$tmp/fakebin/ditto"
chmod +x "$tmp/fakebin/ditto"
rc=0
(PATH="$tmp/fakebin:$PATH" "$bundle" --out "$tmp/cp-out") >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
[ "$rc" = 1 ] || fail "a failed ditto should make bundle.sh exit 1, got $rc"
grep -q "^bundle.sh: " "$tmp/run.err" || fail "a failed ditto should print a 'bundle.sh: ' message, got: $(cat "$tmp/run.err")"
[ ! -e "$tmp/cp-out/contr.app.tmp" ] || fail "a failed ditto left contr.app.tmp behind"
[ ! -e "$tmp/cp-out/contr.app" ] || fail "a failed ditto left a contr.app behind"

echo "test-bundle.sh: ok"
