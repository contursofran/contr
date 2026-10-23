#!/bin/bash
# Test of scripts/install.sh with a dummy bundle. It runs no cargo build, so it takes seconds.
# Every run passes --dest and --bin-dir under mktemp -d, so the real /Applications and
# ~/.local/bin are never written. The guard cases only check refusals, which stop before any change.
set -euo pipefail
unset CDPATH # with CDPATH set, cd prints the directory and breaks $(cd ... && pwd)

repo="$(cd "$(dirname "$0")/.." && pwd)"
install="$repo/scripts/install.sh"
bundle="$repo/scripts/bundle.sh"
tmp="$(cd "$(mktemp -d)" && pwd)" # normalised, so the paths printed by install.sh compare equal
trap 'rm -rf "$tmp"' EXIT

fail() {
    echo "test-install.sh: FAIL: $*" >&2
    exit 1
}

[ -n "${HOME:-}" ] || fail "HOME is not set"

# Runs install.sh with the given arguments. Sets $rc. Output goes to $tmp/run.out and $tmp/run.err.
run_install() {
    rc=0
    "$install" "$@" >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
}

# Runs install.sh, which must refuse (exit 2) with stderr "install.sh: <pattern>".
expect_refused() {
    local pattern="$1"
    shift
    run_install "$@"
    [ "$rc" = 2 ] || fail "install.sh $* should refuse with exit 2, got $rc: $(cat "$tmp/run.err")"
    grep -q "^install.sh: .*$pattern" "$tmp/run.err" ||
        fail "install.sh $* should say '$pattern' on stderr, got: $(cat "$tmp/run.err")"
}

# Runs install.sh, which must fail (exit 1) with stderr "install.sh: <pattern>".
expect_failed() {
    local pattern="$1"
    shift
    run_install "$@"
    [ "$rc" = 1 ] || fail "install.sh $* should fail with exit 1, got $rc: $(cat "$tmp/run.err")"
    grep -q "^install.sh: .*$pattern" "$tmp/run.err" ||
        fail "install.sh $* should say '$pattern' on stderr, got: $(cat "$tmp/run.err")"
}

bundle_id() {
    /usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist" 2>/dev/null || true
}

# Writes a bundle with the given id and a marker file, so an old bundle can be told from a new one.
make_bundle() { # dir id marker
    mkdir -p "$1/Contents"
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' '<plist version="1.0"><dict>' \
        "<key>CFBundleIdentifier</key><string>$2</string>" '</dict></plist>' \
        >"$1/Contents/Info.plist"
    printf '%s\n' "$3" >"$1/Contents/marker"
}

printf 'int main(void){return 0;}' | cc -x c - -o "$tmp/contr" ||
    fail "cc could not build the dummy binary"
"$bundle" --bin "$tmp/contr" --out "$tmp/b" >"$tmp/build.out" 2>"$tmp/build.err" ||
    {
        cat "$tmp/build.err" >&2
        fail "bundle.sh could not build the dummy bundle"
    }
app="$tmp/b/contr.app"
[ "$(bundle_id "$app")" = dev.contr ] || fail "the dummy bundle is not dev.contr"

# Fresh install: the app and the link are made, the link runs, stdout is the two paths.
run_install --app "$app" --dest "$tmp/apps" --bin-dir "$tmp/bin"
[ "$rc" = 0 ] || fail "fresh install should succeed, got $rc: $(cat "$tmp/run.err")"
[ "$(bundle_id "$tmp/apps/contr.app")" = dev.contr ] || fail "no dev.contr bundle at $tmp/apps/contr.app"
[ -L "$tmp/bin/contr" ] || fail "$tmp/bin/contr is not a symlink"
[ "$(readlink "$tmp/bin/contr")" = "$tmp/apps/contr.app/Contents/MacOS/contr" ] ||
    fail "link target is $(readlink "$tmp/bin/contr")"
"$tmp/bin/contr" || fail "the linked binary does not run"
[ "$(sed -n 1p "$tmp/run.out")" = "$tmp/apps/contr.app" ] ||
    fail "stdout line 1 should be the app path, got: $(sed -n 1p "$tmp/run.out")"
[ "$(sed -n 2p "$tmp/run.out")" = "$tmp/bin/contr" ] ||
    fail "stdout line 2 should be the link path, got: $(sed -n 2p "$tmp/run.out")"

# Second install over our own bundle: succeeds, one entry in dest, no staging copy left.
run_install --app "$app" --dest "$tmp/apps" --bin-dir "$tmp/bin"
[ "$rc" = 0 ] || fail "second install should succeed, got $rc: $(cat "$tmp/run.err")"
[ "$(ls -A "$tmp/apps")" = contr.app ] || fail "second install left: $(ls -A "$tmp/apps")"

# Foreign bundle at dest: refused without --replace, and nothing changes (no app change, no link).
make_bundle "$tmp/apps2/Contr.app" com.example.other old
expect_refused "Use --replace" --app "$app" --dest "$tmp/apps2" --bin-dir "$tmp/bin2"
[ "$(cat "$tmp/apps2/Contr.app/Contents/marker")" = old ] || fail "a refused install changed the foreign bundle"
[ ! -e "$tmp/bin2/contr" ] && [ ! -L "$tmp/bin2/contr" ] || fail "a refused install created the link"
# With --replace: the foreign bundle is replaced by ours.
run_install --app "$app" --dest "$tmp/apps2" --bin-dir "$tmp/bin2" --replace
[ "$rc" = 0 ] || fail "--replace should replace the foreign bundle, got $rc: $(cat "$tmp/run.err")"
[ "$(bundle_id "$tmp/apps2/contr.app")" = dev.contr ] || fail "--replace did not install dev.contr"
[ ! -e "$tmp/apps2/contr.app/Contents/marker" ] || fail "--replace kept the foreign bundle"
[ -L "$tmp/bin2/contr" ] || fail "--replace did not create the link"

# Foreign regular file at the link path: refused, file kept, app not installed.
mkdir -p "$tmp/bin3"
printf 'keep\n' >"$tmp/bin3/contr"
expect_refused "is not a link to a contr bundle" --app "$app" --dest "$tmp/apps3" --bin-dir "$tmp/bin3"
[ "$(cat "$tmp/bin3/contr")" = keep ] || fail "a refused install changed the foreign file"
[ ! -e "$tmp/apps3/contr.app" ] || fail "a refused install still installed the app"
run_install --app "$app" --dest "$tmp/apps3" --bin-dir "$tmp/bin3" --replace
[ "$rc" = 0 ] || fail "--replace should replace the foreign file, got $rc: $(cat "$tmp/run.err")"
[ -L "$tmp/bin3/contr" ] || fail "--replace did not make a link over the foreign file"

# A directory at the link path is refused even with --replace (it is never removed).
mkdir -p "$tmp/bin4/contr"
expect_refused "is a directory" --app "$app" --dest "$tmp/apps4" --bin-dir "$tmp/bin4" --replace
[ -d "$tmp/bin4/contr" ] || fail "install removed the directory at the link path"

# Symlinks that do not point at a contr bundle are refused: one into a foreign bundle, one elsewhere.
make_bundle "$tmp/foreign5/contr.app" com.example.other other
mkdir -p "$tmp/bin5"
ln -s "$tmp/foreign5/contr.app/Contents/MacOS/contr" "$tmp/bin5/contr"
expect_refused "is not a link to a contr bundle" --app "$app" --dest "$tmp/apps5" --bin-dir "$tmp/bin5"
rm "$tmp/bin5/contr"
ln -s "$tmp/old/Contr.app/Contents/Helpers/bin/contr" "$tmp/bin5/contr"
expect_refused "is not a link to a contr bundle" --app "$app" --dest "$tmp/apps5" --bin-dir "$tmp/bin5"
[ ! -e "$tmp/apps5/contr.app" ] || fail "a refused install still installed the app"

# --no-link: the app is installed, no link and no bin dir are made.
run_install --app "$app" --dest "$tmp/apps6" --bin-dir "$tmp/bin6" --no-link
[ "$rc" = 0 ] || fail "--no-link install should succeed, got $rc: $(cat "$tmp/run.err")"
[ "$(bundle_id "$tmp/apps6/contr.app")" = dev.contr ] || fail "--no-link did not install the app"
[ ! -e "$tmp/bin6" ] && [ ! -L "$tmp/bin6" ] || fail "--no-link created the bin dir"
[ "$(wc -l <"$tmp/run.out" | tr -d ' ')" = 1 ] || fail "--no-link stdout should be 1 line, got: $(cat "$tmp/run.out")"

# PATH hint: printed when the bin dir is not in PATH, not printed when it is.
PATH="/usr/bin:/bin:/usr/sbin:/sbin" "$install" --app "$app" --dest "$tmp/apps7" --bin-dir "$tmp/bin7" \
    >"$tmp/run.out" 2>"$tmp/run.err" || fail "install should succeed without the bin dir in PATH: $(cat "$tmp/run.err")"
grep -q "is not in PATH" "$tmp/run.out" || fail "no PATH hint when the bin dir is not in PATH: $(cat "$tmp/run.out")"
PATH="$tmp/bin7:/usr/bin:/bin" "$install" --app "$app" --dest "$tmp/apps7" --bin-dir "$tmp/bin7" \
    >"$tmp/run.out" 2>"$tmp/run.err" || fail "install should succeed with the bin dir in PATH: $(cat "$tmp/run.err")"
grep -q "is not in PATH" "$tmp/run.out" && fail "PATH hint printed although the bin dir is in PATH"

# A failed copy keeps the installed app, removes the partial copy, and fails with exit 1.
run_install --app "$app" --dest "$tmp/apps8" --bin-dir "$tmp/bin8"
[ "$rc" = 0 ] || fail "setup install should succeed, got $rc: $(cat "$tmp/run.err")"
printf 'keep\n' >"$tmp/apps8/contr.app/Contents/marker"
mkdir -p "$tmp/fakebin"
printf '#!/bin/sh\n# Fake ditto: leaves a partial copy at the destination, then fails.\nmkdir -p "$2"\nexit 2\n' \
    >"$tmp/fakebin/ditto"
chmod +x "$tmp/fakebin/ditto"
rc=0
PATH="$tmp/fakebin:$PATH" "$install" --app "$app" --dest "$tmp/apps8" --bin-dir "$tmp/bin8" \
    >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
[ "$rc" = 1 ] || fail "a failed copy should exit 1, got $rc"
grep -q "^install.sh: ditto failed" "$tmp/run.err" || fail "a failed copy should say 'ditto failed', got: $(cat "$tmp/run.err")"
[ "$(cat "$tmp/apps8/contr.app/Contents/marker")" = keep ] || fail "a failed copy changed the installed app"
[ "$(ls -A "$tmp/apps8")" = contr.app ] || fail "a failed copy left: $(ls -A "$tmp/apps8")"

# Bad --app: a missing path, and a bundle that is not ours. Both exit 1 and install nothing.
expect_failed "app not found" --app "$tmp/missing.app" --dest "$tmp/apps9" --bin-dir "$tmp/bin9"
make_bundle "$tmp/notours.app" com.example.other x
expect_failed "not a contr bundle" --app "$tmp/notours.app" --dest "$tmp/apps9" --bin-dir "$tmp/bin9"
[ ! -e "$tmp/apps9" ] || fail "a bad --app created the dest dir"

# Dest guards: empty, /, /Applications, $HOME and symlinks to them are refused (exit 2) before any change.
ln -s /Applications "$tmp/apps-link"
ln -s "$HOME" "$tmp/home-link"
expect_refused "dir is empty" --app "$app" --dest "" --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest / --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest /Applications --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest /applications --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest "$HOME" --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest "$HOME/" --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest "$tmp/apps-link" --bin-dir "$tmp/bin10"
expect_refused "protected dir" --app "$app" --dest "$tmp/home-link" --bin-dir "$tmp/bin10"
[ ! -e "$tmp/bin10" ] || fail "a refused --dest created the bin dir"

# A staging copy left by an earlier failed run is removed before the new copy is made.
mkdir -p "$tmp/apps12/.contr-new.app/Contents"
printf 'stale\n' >"$tmp/apps12/.contr-new.app/Contents/stale"
run_install --app "$app" --dest "$tmp/apps12" --bin-dir "$tmp/bin12"
[ "$rc" = 0 ] || fail "install over a stale staging copy should succeed, got $rc: $(cat "$tmp/run.err")"
[ "$(ls -A "$tmp/apps12")" = contr.app ] || fail "a stale staging copy was kept: $(ls -A "$tmp/apps12")"

# Relative --dest and --bin-dir are made absolute, so the link points at the installed app.
rc=0
(cd "$tmp" && "$install" --app "$app" --dest relapps --bin-dir relbin) >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
[ "$rc" = 0 ] || fail "relative paths should succeed, got $rc: $(cat "$tmp/run.err")"
[ "$(sed -n 1p "$tmp/run.out")" = "$tmp/relapps/contr.app" ] ||
    fail "relative --dest should print an absolute app path, got: $(sed -n 1p "$tmp/run.out")"
[ "$(sed -n 2p "$tmp/run.out")" = "$tmp/relbin/contr" ] ||
    fail "relative --bin-dir should print an absolute link path, got: $(sed -n 2p "$tmp/run.out")"
[ "$(readlink "$tmp/relbin/contr")" = "$tmp/relapps/contr.app/Contents/MacOS/contr" ] ||
    fail "the relative link target is $(readlink "$tmp/relbin/contr")"

# Without HOME and without --bin-dir, the default bin dir is unknown: exit 1, nothing installed.
rc=0
env -u HOME "$install" --app "$app" --dest "$tmp/apps11" >"$tmp/run.out" 2>"$tmp/run.err" || rc=$?
[ "$rc" = 1 ] || fail "an unset HOME should exit 1, got $rc"
grep -q "^install.sh: HOME is not set" "$tmp/run.err" || fail "an unset HOME should say so, got: $(cat "$tmp/run.err")"
[ ! -e "$tmp/apps11" ] || fail "an unset HOME still created the dest dir"

# Argument errors exit 1.
expect_failed "unknown argument" --nope
expect_failed "needs a dir" --dest
expect_failed "needs a dir, not a flag" --dest --replace
expect_failed "needs a path" --app
expect_failed "needs a path, not an empty value" --app ""
expect_failed "needs a dir, not an empty value" --bin-dir ""

if ! "$install" --help >/dev/null 2>&1; then
    fail "--help should exit 0"
fi

echo "test-install.sh: ok"
