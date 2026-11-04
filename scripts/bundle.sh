#!/bin/bash
# Builds contr.app with `go tool mygo build` (D115): the release binary, the app icon, Info.plist,
# libghostty-vt in Contents/Resources (D93) and an ad-hoc signature.
#
#   scripts/bundle.sh [--out DIR] [--zip]
#
#   --out DIR   output dir (default: <repo>/dist/bundle); contr.app inside is replaced.
#               Refuses /, /Applications and $HOME.
#   --zip       also write contr-<version>-macos-arm64.zip next to the .app
#
# The version is the first line of the VERSION file in the repository root. scripts/mygo-config.sh
# writes mygo.json from it. With a public key in scripts/update.pub the build has updates; with
# MYGO_UPDATER_PRIVATE_KEY in the environment, `mygo build` also signs the update files, which
# stay in dist/mygo. The MyGo build downloads libghostty-vt into the user cache when needed.
# On success prints the absolute path of contr.app, and the zip path with --zip.
# Errors go to stderr as "bundle.sh: <message>" with exit 1.

set -euo pipefail
unset CDPATH # with CDPATH set, cd prints the directory and breaks $(cd ... && pwd)

usage() {
    echo "usage: $0 [--out DIR] [--zip]" >&2
}

die() {
    echo "bundle.sh: $*" >&2
    exit 1
}

repo="$(cd "$(dirname "$0")/.." && pwd)"
out=""
zip=0
app_tmp=""
build_dir=""

cleanup() {
    if [ -n "$app_tmp" ]; then rm -rf "$app_tmp"; fi
    if [ -n "$build_dir" ]; then rm -rf "$build_dir"; fi
}
trap cleanup EXIT
trap 'echo "bundle.sh: a command failed at line $LINENO" >&2; exit 1' ERR

while [ $# -gt 0 ]; do
    case "$1" in
        --out)
            [ $# -ge 2 ] || die "--out needs a dir"
            case "$2" in -*) die "--out needs a dir, not a flag: $2" ;; esac
            [ -n "$2" ] || die "--out needs a dir, not an empty value"
            out="$2"
            shift 2
            ;;
        --zip)
            zip=1
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            usage
            die "unknown argument: $1"
            ;;
    esac
done

# Refuses an --out dir that the rm -rf below must not reach: it removes <out>/contr.app, so
# /, /Applications and $HOME are refused. The physical path is compared (symlinks resolved), in
# lower case, because APFS is case-insensitive.
check_out() {
    local phys home lower_phys lower_home name
    phys="$(cd -- "$1" && pwd -P)" || die "cannot resolve --out: $1"
    home="$(cd -- "${HOME:-}" 2>/dev/null && pwd -P)" || home=""
    lower_phys="$(printf '%s' "$phys" | tr '[:upper:]' '[:lower:]')"
    lower_home="$(printf '%s' "$home" | tr '[:upper:]' '[:lower:]')"
    for name in / /applications "$lower_home"; do
        if [ -n "$name" ] && [ "$lower_phys" = "$name" ]; then
            die "refusing --out $1: it is a protected dir (/, /Applications or \$HOME)"
        fi
    done
}

version="$(sed -n 1p "$repo/VERSION" 2>/dev/null)" || die "cannot read $repo/VERSION"
case "$version" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) die "no version in VERSION: '$version'" ;;
esac

if [ -z "$out" ]; then
    out="$repo/dist/bundle"
fi
mkdir -p "$out"
out="$(cd "$out" && pwd)"
check_out "$out"
app="$out/contr.app"
app_tmp="$out/contr.app.tmp"
zip_path="$out/contr-$version-macos-arm64.zip"

# mygo.json is generated, so the version and the update key have one source each (D115).
"$repo/scripts/mygo-config.sh" >"$repo/mygo.json" || die "cannot write mygo.json"

build_dir="$(mktemp -d)"
(cd "$repo" && go tool mygo build -platform darwin/arm64 -skip-dmg -o "$build_dir") 1>&2 ||
    die "mygo build failed"
built="$build_dir/darwin-arm64/contr.app"
[ -d "$built" ] || die "mygo build made no app at $built"

# The terminal library is in the app: a packaged app never downloads it (D93).
lib="$built/Contents/Resources/libghostty-vt.dylib"
[ -f "$lib" ] || die "the app has no libghostty-vt.dylib in Resources"

rm -rf "$app_tmp"
ditto "$built" "$app_tmp" || die "cannot copy the app"
codesign --verify --strict "$app_tmp" ||
    die "the signature does not verify"

case "$app" in
    *.app) ;;
    *) die "refusing to remove $app: not a .app bundle" ;;
esac
rm -rf "$app"
mv "$app_tmp" "$app"

echo "$app"
if [ "$zip" = 1 ]; then
    rm -f "$zip_path"
    ditto -c -k --keepParent "$app" "$zip_path" || die "ditto failed"
    echo "$zip_path"
fi
