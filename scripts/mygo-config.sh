#!/bin/bash
# Writes the MyGo project configuration (mygo.json) for `mygo build` to stdout.
#
#   scripts/mygo-config.sh
#
# The version is the first line of VERSION (D17). The update settings (D115) are added only when
# scripts/update.pub holds the public update key: MyGo refuses an empty key, so a build without
# the key has no updates, and the app says so in Settings. The key file is empty until the owner
# adds the key from `mygo keygen`.
# Errors go to stderr as "mygo-config.sh: <message>" with exit 1.

set -euo pipefail
unset CDPATH

die() {
    echo "mygo-config.sh: $*" >&2
    exit 1
}

repo="$(cd "$(dirname "$0")/.." && pwd)"

version="$(sed -n 1p "$repo/VERSION" 2>/dev/null)" || die "cannot read $repo/VERSION"
case "$version" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) die "no version in VERSION: '$version'" ;;
esac

key=""
if [ -f "$repo/scripts/update.pub" ]; then
    key="$(tr -d '\r\n ' <"$repo/scripts/update.pub")"
fi
case "$key" in
    *[!A-Za-z0-9+/=_-]*) die "scripts/update.pub is not a public key" ;;
esac

commit="$(git -C "$repo" rev-parse --short HEAD 2>/dev/null)" || commit=unknown

updates=""
if [ -n "$key" ]; then
    updates=$(printf '  "updates": {\n    "github": "contursofran/contr",\n    "publicKey": "%s"\n  },\n' "$key")
fi

cat <<JSON
{
  "name": "contr",
  "slug": "contr",
  "identifier": "dev.contr",
  "version": "$version",
  "icon": "assets/app/icon.png",
  "main": "./cmd/contr",
  "out": "dist/mygo",
$updates
  "macos": {
    "minimumSystemVersion": "12.0",
    "infoPlist": {
      "ContrCommit": "$commit",
      "LSApplicationCategoryType": "public.app-category.developer-tools"
    }
  }
}
JSON
