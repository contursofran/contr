#!/bin/bash
# Installs contr.app and a `contr` link to the app's binary.
#
#   scripts/install.sh [--app PATH] [--dest DIR] [--bin-dir DIR] [--no-link] [--replace]
#
#   --app PATH     bundle to install (default: run scripts/bundle.sh and use its output)
#   --dest DIR     where contr.app goes (default: /Applications)
#   --bin-dir DIR  where the `contr` symlink goes (default: $HOME/.local/bin)
#   --no-link      do not create the symlink
#   --replace      allow replacing a bundle or a `contr` entry that is not ours
#
# "Ours" means a bundle whose CFBundleIdentifier is dev.contr. Anything else is refused
# (exit 2) unless --replace is given. All refusal checks run before the first change.
# The new bundle is copied to a staging name first; the old one is removed only after the copy.
# On success prints the installed app path, the link path and, if needed, a PATH hint.
# Errors go to stderr as "install.sh: <message>": exit 1 for bad input or a failed step,
# exit 2 for a refusal that protects an existing file or dir.

set -Eeuo pipefail # -E: the ERR trap also covers the functions below
unset CDPATH # with CDPATH set, cd prints the directory and breaks $(cd ... && pwd)

repo="$(cd "$(dirname "$0")/.." && pwd)"
app_src=""
dest="/Applications"
bin_dir=""
no_link=0
replace=0
link=""
stage=""

usage() {
    echo "usage: $0 [--app PATH] [--dest DIR] [--bin-dir DIR] [--no-link] [--replace]"
}

die() {
    echo "install.sh: $*" >&2
    exit 1
}

refuse() {
    echo "install.sh: $*" >&2
    exit 2
}

# Removes one bundle path. Only a path that ends in .app is removed, so the guard
# holds for every remove in this script (the install target and the staging copy).
remove_bundle() {
    case "$1" in
        *.app) rm -rf -- "$1" ;;
        *) die "refusing to remove $1: not a .app bundle" ;;
    esac
}

cleanup() {
    if [ -n "$stage" ] && [ -e "$stage" ]; then remove_bundle "$stage"; fi
}
trap cleanup EXIT
trap 'echo "install.sh: a command failed at line $LINENO" >&2; exit 1' ERR

# Prints the CFBundleIdentifier of the bundle at $1. Prints nothing if there is none.
bundle_id() {
    /usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist" 2>/dev/null || true
}

# True when the bundle at $1 is ours.
is_ours() {
    [ "$(bundle_id "$1")" = "dev.contr" ]
}

# True when $1 is a symlink to <bundle>/Contents/MacOS/contr and that bundle is ours.
link_is_ours() {
    local target
    [ -L "$1" ] || return 1
    target="$(readlink "$1")" || return 1
    case "$target" in
        */contr.app/Contents/MacOS/contr) is_ours "${target%/Contents/MacOS/contr}" ;;
        *) return 1 ;;
    esac
}

# Refuses a --dest that is / or $HOME (checked physically, in lower case, because APFS is
# case-insensitive). /Applications is allowed: it is the folder that holds contr.app.
# The script never removes or replaces the dest dir itself, only <dest>/contr.app.
# An existing dest is resolved here.
check_dest() {
    local phys home lower_phys lower_home name
    if [ -d "$dest" ]; then
        dest="$(cd -- "$dest" && pwd)" || die "cannot resolve --dest: $dest"
        phys="$(cd -- "$dest" && pwd -P)" || die "cannot resolve --dest: $dest"
        home=""
        if [ -n "${HOME:-}" ]; then
            home="$(cd -- "$HOME" 2>/dev/null && pwd -P)" || home=""
        fi
        lower_phys="$(printf '%s' "$phys" | tr '[:upper:]' '[:lower:]')"
        lower_home="$(printf '%s' "$home" | tr '[:upper:]' '[:lower:]')"
        for name in / "$lower_home"; do
            if [ -n "$name" ] && [ "$lower_phys" = "$name" ]; then
                refuse "refusing --dest $dest: it is a protected dir (/ or \$HOME)"
            fi
        done
    else
        case "$dest" in /*) ;; *) dest="$PWD/$dest" ;; esac
    fi
}

# Refuses to replace what is not ours, unless --replace is given. Nothing is changed here.
check_targets() {
    local target="$dest/contr.app" id
    if [ -e "$target" ] || [ -L "$target" ]; then
        if ! is_ours "$target" && [ "$replace" = 0 ]; then
            id="$(bundle_id "$target")"
            refuse "$target is not a contr bundle (bundle id: ${id:-none}). Use --replace to replace it."
        fi
    fi
    if [ "$no_link" = 0 ]; then
        if [ -d "$link" ] && [ ! -L "$link" ]; then
            refuse "$link is a directory. Remove it by hand."
        fi
        if { [ -e "$link" ] || [ -L "$link" ]; } && ! link_is_ours "$link" && [ "$replace" = 0 ]; then
            refuse "$link is not a link to a contr bundle. Use --replace to replace it."
        fi
    fi
}

# Sets app_src: the given --app, or the output of scripts/bundle.sh. Checks that it is ours.
resolve_source() {
    if [ -z "$app_src" ]; then
        app_src="$("$repo/scripts/bundle.sh")" || die "bundle.sh failed"
        app_src="$(printf '%s\n' "$app_src" | sed -n 1p)"
    fi
    [ -d "$app_src" ] || die "app not found: $app_src"
    app_src="$(cd -- "$app_src" && pwd)"
    is_ours "$app_src" || die "not a contr bundle (bundle id: $(bundle_id "$app_src")): $app_src"
}

# Copies the source to a staging name, then swaps it in. The old bundle is removed only after
# the copy succeeds.
install_app() {
    local target="$dest/contr.app"
    stage="$dest/.contr-new.app"
    mkdir -p -- "$dest" || die "cannot create --dest: $dest"
    if [ -e "$stage" ] || [ -L "$stage" ]; then remove_bundle "$stage"; fi
    ditto "$app_src" "$stage" || die "ditto failed"
    # The old bundle is removed before the new one is moved in. Between the two steps there is no
    # app at $target, and if the move fails the old app is already gone. A rename swap would avoid
    # that, but it needs a third name in $dest, and only $dest/contr.app may be removed or replaced.
    if [ -e "$target" ] || [ -L "$target" ]; then remove_bundle "$target"; fi
    mv -- "$stage" "$target"
    stage=""
}

link_cli() {
    mkdir -p -- "$bin_dir" || die "cannot create --bin-dir: $bin_dir"
    ln -sfn "$dest/contr.app/Contents/MacOS/contr" "$link"
}

print_hint() {
    case ":$PATH:" in
        *":$bin_dir:"*) ;;
        *) echo "install.sh: hint: $bin_dir is not in PATH. Add it to run contr by name." ;;
    esac
}

# When the file is sourced, only the functions above are defined.
# The test sources it to call check_dest.
if [ "${BASH_SOURCE[0]}" != "$0" ]; then return 0; fi

while [ $# -gt 0 ]; do
    case "$1" in
        --app)
            [ $# -ge 2 ] || die "--app needs a path"
            case "$2" in -*) die "--app needs a path, not a flag: $2" ;; esac
            [ -n "$2" ] || die "--app needs a path, not an empty value"
            app_src="$2"
            shift 2
            ;;
        --dest)
            [ $# -ge 2 ] || die "--dest needs a dir"
            case "$2" in -*) die "--dest needs a dir, not a flag: $2" ;; esac
            [ -n "$2" ] || die "--dest needs a dir, not an empty value"
            dest="$2"
            shift 2
            ;;
        --bin-dir)
            [ $# -ge 2 ] || die "--bin-dir needs a dir"
            case "$2" in -*) die "--bin-dir needs a dir, not a flag: $2" ;; esac
            [ -n "$2" ] || die "--bin-dir needs a dir, not an empty value"
            bin_dir="$2"
            shift 2
            ;;
        --no-link)
            no_link=1
            shift
            ;;
        --replace)
            replace=1
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            usage >&2
            die "unknown argument: $1"
            ;;
    esac
done

if [ -z "$bin_dir" ]; then
    [ -n "${HOME:-}" ] || die "HOME is not set: pass --bin-dir"
    bin_dir="$HOME/.local/bin"
fi
case "$bin_dir" in /*) ;; *) bin_dir="$PWD/$bin_dir" ;; esac
link="$bin_dir/contr"

check_dest
check_targets
resolve_source
install_app
echo "$dest/contr.app"
if [ "$no_link" = 0 ]; then
    link_cli
    echo "$link"
    print_hint
fi
