#!/bin/bash
# Test of scripts/bundle.sh with dummy binaries. It runs no cargo build, so it takes seconds.
# Every path it writes is under a mktemp -d dir.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
bundle="$repo/scripts/bundle.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() {
    echo "test-bundle.sh: FAIL: $*" >&2
    exit 1
}

version="$(awk '
    /^\[workspace\.package\]/ { in_pkg = 1; next }
    /^\[/ { in_pkg = 0 }
    in_pkg && /^version[[:space:]]*=/ {
        sub(/^[^"]*"/, "")
        sub(/".*$/, "")
        print
        exit
    }
' "$repo/Cargo.toml")"
case "$version" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) fail "no workspace version in Cargo.toml: '$version'" ;;
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
[ -n "$(plist_value ContrCommit "$plist")" ] || fail "ContrCommit is empty"
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

# Without --out, the bundle goes under cargo's target directory (CARGO_TARGET_DIR here).
if ! CARGO_TARGET_DIR="$tmp/target" "$bundle" --bin "$tmp/contr" >"$tmp/run.out" 2>"$tmp/run.err"; then
    cat "$tmp/run.err" >&2
    fail "bundle.sh failed with CARGO_TARGET_DIR set"
fi
case "$(cat "$tmp/run.out")" in
    */target/bundle/contr.app) ;;
    *) fail "default output should be <target>/bundle/contr.app, got: $(cat "$tmp/run.out")" ;;
esac
[ -d "$tmp/target/bundle/contr.app" ] || fail "no bundle under CARGO_TARGET_DIR"

# Error paths. Each one fails with "bundle.sh: <message>" and writes no bundle.
expect_failure "binary not found" --bin "$tmp/missing" --out "$tmp/err-out"
expect_failure "not a Mach-O binary" --bin "$tmp/text" --out "$tmp/err-out"
expect_failure "expected an arm64 binary" --bin "$tmp/contr-x86" --out "$tmp/err-out"
expect_failure "unknown argument" --nope
expect_failure "needs a dir" --out
expect_failure "needs a path" --bin

if ! "$bundle" --help >/dev/null 2>&1; then
    fail "--help should exit 0"
fi

echo "test-bundle.sh: ok"
