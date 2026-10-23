#!/bin/bash
# Builds contr.app: the release binary, the app icon, Info.plist and an ad-hoc signature.
#
#   scripts/bundle.sh [--bin PATH] [--out DIR] [--zip]
#
#   --bin PATH  use this binary instead of building (tests use a dummy); default:
#               cargo build --release --locked -p contr, then <target>/release/contr
#   --out DIR   output dir (default: <target>/bundle); contr.app inside is replaced
#   --zip       also write contr-<version>-macos-arm64.zip next to the .app
#
# <target> is cargo's target directory (CARGO_TARGET_DIR or the cargo config).
# On success prints the absolute path of contr.app, and the zip path with --zip.
# Errors go to stderr as "bundle.sh: <message>" with exit 1.

set -euo pipefail

usage() {
    echo "usage: $0 [--bin PATH] [--out DIR] [--zip]" >&2
}

die() {
    echo "bundle.sh: $*" >&2
    exit 1
}

repo="$(cd "$(dirname "$0")/.." && pwd)"
bin=""
out=""
zip=0
td=""
app_tmp=""
icon_dir=""

cleanup() {
    if [ -n "$app_tmp" ]; then rm -rf "$app_tmp"; fi
    if [ -n "$icon_dir" ]; then rm -rf "$icon_dir"; fi
}
trap cleanup EXIT
trap 'echo "bundle.sh: a command failed at line $LINENO" >&2' ERR

while [ $# -gt 0 ]; do
    case "$1" in
        --bin)
            [ $# -ge 2 ] || die "--bin needs a path"
            bin="$2"
            shift 2
            ;;
        --out)
            [ $# -ge 2 ] || die "--out needs a dir"
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

# cargo's target directory for this workspace. cargo honours CARGO_TARGET_DIR,
# so this is not always ./target. Read once, only when a path needs it.
need_target() {
    if [ -z "$td" ]; then
        td="$(cd "$repo" && cargo metadata --format-version 1 --no-deps 2>/dev/null |
            sed -n 's/.*"target_directory":"\([^"]*\)".*/\1/p')" ||
            die "cannot read cargo's target directory"
        [ -n "$td" ] || die "cannot read cargo's target directory"
    fi
}

if [ -z "$bin" ]; then
    (cd "$repo" && cargo build --release --locked -p contr) 1>&2 ||
        die "cargo build failed"
    need_target
    bin="$td/release/contr"
fi
[ -f "$bin" ] || die "binary not found: $bin"

arch="$(lipo -archs "$bin" 2>/dev/null)" || die "not a Mach-O binary: $bin"
[ "$arch" = arm64 ] || die "expected an arm64 binary, got: $arch"

min_macos="$(vtool -show-build "$bin" 2>/dev/null | awk '/minos/ && !v { v = $2 } END { print v }')" ||
    die "cannot read the minimum macOS version of $bin"
[ -n "$min_macos" ] || die "no minimum macOS version in $bin"

version="$(awk '
    /^\[workspace\.package\]/ { in_pkg = 1; next }
    /^\[/ { in_pkg = 0 }
    in_pkg && /^version[[:space:]]*=/ {
        sub(/^[^"]*"/, "")
        sub(/".*$/, "")
        print
        exit
    }
' "$repo/Cargo.toml")" || die "cannot read the version in Cargo.toml"
case "$version" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) die "no workspace version in Cargo.toml: '$version'" ;;
esac

commit="$(git -C "$repo" rev-parse --short HEAD 2>/dev/null)" || commit=unknown

icon_src="$repo/crates/contr/assets/app/icon.svg"
[ -f "$icon_src" ] || die "icon not found: $icon_src"

if [ -z "$out" ]; then
    need_target
    out="$td/bundle"
fi
mkdir -p "$out"
out="$(cd "$out" && pwd)"
app="$out/contr.app"
app_tmp="$out/contr.app.tmp"
zip_path="$out/contr-$version-macos-$arch.zip"

rm -rf "$app_tmp"
mkdir -p "$app_tmp/Contents/MacOS" "$app_tmp/Contents/Resources"
cp "$bin" "$app_tmp/Contents/MacOS/contr"
chmod 755 "$app_tmp/Contents/MacOS/contr"

# Icon: render the SVG once, resize it to the ten iconset sizes, pack them as .icns.
icon_dir="$(mktemp -d)"
sips -s format png "$icon_src" --out "$icon_dir/icon-1024.png" >/dev/null ||
    die "cannot render $icon_src"
iconset="$icon_dir/contr.iconset"
mkdir "$iconset"
for pair in 16:icon_16x16.png 32:icon_16x16@2x.png 32:icon_32x32.png \
    64:icon_32x32@2x.png 128:icon_128x128.png 256:icon_128x128@2x.png \
    256:icon_256x256.png 512:icon_256x256@2x.png 512:icon_512x512.png \
    1024:icon_512x512@2x.png; do
    size="${pair%%:*}"
    name="${pair#*:}"
    sips -z "$size" "$size" "$icon_dir/icon-1024.png" --out "$iconset/$name" >/dev/null ||
        die "cannot resize the icon to $size"
done
iconutil -c icns "$iconset" -o "$app_tmp/Contents/Resources/contr.icns" ||
    die "iconutil failed"

sed -e "s|@VERSION@|$version|g" \
    -e "s|@COMMIT@|$commit|g" \
    -e "s|@MIN_MACOS@|$min_macos|g" \
    "$repo/scripts/Info.plist.in" >"$app_tmp/Contents/Info.plist"
plutil -lint "$app_tmp/Contents/Info.plist" >/dev/null ||
    die "Info.plist is not valid"

codesign --force --sign - --timestamp=none "$app_tmp" ||
    die "codesign failed"
codesign --verify --strict "$app_tmp" ||
    die "the signature does not verify"

rm -rf "$app"
mv "$app_tmp" "$app"

echo "$app"
if [ "$zip" = 1 ]; then
    rm -f "$zip_path"
    ditto -c -k --keepParent "$app" "$zip_path" || die "ditto failed"
    echo "$zip_path"
fi
