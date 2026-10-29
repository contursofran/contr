#!/bin/bash
# Test of scripts/bundle.sh with dummy binaries. It runs no go build, so it takes seconds.
# Every path it writes is under a dir in tmp/ of the repository. HOME points into that dir.
set -euo pipefail
unset CDPATH # with CDPATH set, cd prints the directory and breaks $(cd ... && pwd)

repo="$(cd "$(dirname "$0")/.." && pwd)"
bundle="$repo/scripts/bundle.sh"
mkdir -p "$repo/tmp"
tmp="$(cd "$(mktemp -d "$repo/tmp/test-bundle.XXXXXX")" && pwd)"
export HOME="$tmp/home"
mkdir -p "$HOME"
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

printf 'int main(void){return 0;}' | cc -x c - -o "$tmp/contr" ||
    fail "cc could not build the arm64 dummy binary"
printf 'int main(void){return 0;}' | cc -arch x86_64 -x c - -o "$tmp/contr-x86" ||
    fail "cc could not build the x86_64 dummy binary"
printf 'not a binary\n' >"$tmp/text"

# First run: --zip. Stdout is the app path, then the zip path.
if ! run_bundle --bin "$tmp/contr" --out "$tmp/out" --zip; then
    cat "$tmp/run.err" >&2
    fail "bundle.sh failed on the arm64 dummy binary"
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
expect_plist CFBundleIconFile contr
expect_plist CFBundleShortVersionString "$version"
expect_plist CFBundleVersion "$version"
expect_plist CFBundlePackageType APPL
min="$(plist_value LSMinimumSystemVersion "$plist")"
case "$min" in
    [0-9]*.[0-9]*) ;;
    *) fail "LSMinimumSystemVersion is '$min', expected a version" ;;
esac
# ContrCommit is the short HEAD hash, or "unknown" outside git (same as bundle.sh).
expected_commit="$(git -C "$repo" rev-parse --short HEAD 2>/dev/null || echo unknown)"
expect_plist ContrCommit "$expected_commit"
if grep -q '@' "$plist"; then
    fail "Info.plist still has a placeholder: $(grep '@' "$plist")"
fi

[ -x "$app/Contents/MacOS/contr" ] || fail "Contents/MacOS/contr is not executable"
"$app/Contents/MacOS/contr" || fail "the bundled binary does not run"

icns="$app/Contents/Resources/contr.icns"
[ -f "$icns" ] || fail "missing $icns"
iconutil -c iconset "$icns" -o "$tmp/icons.iconset" >/dev/null 2>&1 ||
    fail "iconutil cannot read $icns"
pngs="$(find "$tmp/icons.iconset" -name '*.png' | wc -l | tr -d ' ')"
[ "$pngs" = 10 ] || fail "contr.icns holds $pngs PNGs, expected 10"
px="$(sips -g pixelWidth "$tmp/icons.iconset/icon_512x512@2x.png" | awk '/pixelWidth/ { print $2 }')"
[ "$px" = 1024 ] || fail "the largest icon is ${px}px, expected 1024"

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
if ! run_bundle --bin "$tmp/contr" --out "$tmp/out"; then
    cat "$tmp/run.err" >&2
    fail "the second run failed"
fi
[ "$(cat "$tmp/run.out")" = "$app" ] || fail "second run: stdout should be only $app"
[ ! -e "$app/stale-file" ] || fail "second run kept the old bundle"
[ ! -e "$tmp/out/contr.app.tmp" ] || fail "second run left contr.app.tmp behind"
codesign --verify --strict "$app" >/dev/null 2>&1 || fail "second run: signature does not verify"

# CDPATH set and a relative --out: stdout must still be the one app path. The run starts in
# $tmp/cdpath, so the out dir exists under a CDPATH entry and a bare cd would print it.
mkdir -p "$tmp/cdpath"
(cd "$tmp/cdpath" && CDPATH="$tmp/cdpath" "$bundle" --bin "$tmp/contr" --out cdp-out) \
    >"$tmp/run.out" 2>"$tmp/run.err" ||
    {
        cat "$tmp/run.err" >&2
        fail "bundle.sh failed with CDPATH set"
    }
[ "$(cat "$tmp/run.out")" = "$tmp/cdpath/cdp-out/contr.app" ] ||
    fail "with CDPATH set, stdout should be only the app path, got: $(cat "$tmp/run.out")"

# Error paths. Each one fails with "bundle.sh: <message>" and writes no bundle.
expect_failure "binary not found" --bin "$tmp/missing" --out "$tmp/err-out"
expect_failure "not a Mach-O binary" --bin "$tmp/text" --out "$tmp/err-out"
expect_failure "expected an arm64 binary" --bin "$tmp/contr-x86" --out "$tmp/err-out"
expect_failure "unknown argument" --nope
expect_failure "needs a dir" --out
expect_failure "needs a path" --bin
expect_failure "needs a dir, not a flag" --out --zip
expect_failure "needs a path, not a flag" --bin --zip

# --out must not reach a protected dir: the rm -rf removes <out>/contr.app. The physical path is
# checked, so a symlink to one is refused too. Each run fails before anything is removed.
ln -s /Applications "$tmp/apps-link"
ln -s "$HOME" "$tmp/home-link"
expect_failure "needs a dir, not an empty value" --bin "$tmp/contr" --out ""
expect_failure "a protected dir" --bin "$tmp/contr" --out /
expect_failure "a protected dir" --bin "$tmp/contr" --out /Applications
expect_failure "a protected dir" --bin "$tmp/contr" --out /applications
expect_failure "a protected dir" --bin "$tmp/contr" --out "$HOME"
expect_failure "a protected dir" --bin "$tmp/contr" --out "$HOME/"
expect_failure "a protected dir" --bin "$tmp/contr" --out "$tmp/apps-link"
expect_failure "a protected dir" --bin "$tmp/contr" --out "$tmp/home-link"

# A command that is not guarded and fails (here a cp that exits 2) must end bundle.sh with
# exit 1, print the ERR trap message, and leave no staging dir.
mkdir -p "$tmp/fakebin"
printf '#!/bin/sh\nexit 2\n' >"$tmp/fakebin/cp"
chmod +x "$tmp/fakebin/cp"
rc=0
(PATH="$tmp/fakebin:$PATH" "$bundle" --bin "$tmp/contr" --out "$tmp/cp-out") \
    >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
[ "$rc" = 1 ] || fail "a failed cp should make bundle.sh exit 1, got $rc"
grep -q "^bundle.sh: a command failed at line" "$tmp/run.err" ||
    fail "a failed cp should print 'bundle.sh: a command failed at line', got: $(cat "$tmp/run.err")"
[ ! -e "$tmp/cp-out/contr.app.tmp" ] || fail "a failed cp left contr.app.tmp behind"

if ! "$bundle" --help >/dev/null 2>&1; then
    fail "--help should exit 0"
fi

echo "test-bundle.sh: ok"
