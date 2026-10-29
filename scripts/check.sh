#!/bin/sh
# The checks that every pull request must pass. CI runs this script too.
set -eu
cd "$(dirname "$0")/.."

# gofmt of the toolchain that go.mod selects, so every machine formats alike.
unformatted=$("$(go env GOROOT)/bin/gofmt" -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: these files need formatting:"
	echo "$unformatted"
	exit 1
fi

go vet ./...
go test -race ./...

# MyGo needs no cgo, and contr must not add any.
CGO_ENABLED=0 go build -o /dev/null ./cmd/contr
# The bundle and install scripts, with dummy binaries. They write only under tmp/.
scripts/test-bundle.sh
scripts/test-install.sh
echo "check: ok"
