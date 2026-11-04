#!/bin/sh
# The checks that every pull request must pass. CI runs this script too.
set -eu
cd "$(dirname "$0")/.."

# gofmt of the toolchain that go.mod selects, so every machine formats alike.
# Only tracked files: job folders in the worktree hold untracked Go files.
unformatted=$(git ls-files -z '*.go' | xargs -0 "$(go env GOROOT)/bin/gofmt" -l)
if [ -n "$unformatted" ]; then
	echo "gofmt: these files need formatting:"
	echo "$unformatted"
	exit 1
fi

go vet ./...
# Compile the files behind build tags (no hc is needed to vet).
go vet -tags 'e2e bench hccore' ./...
# Merge-conflict markers and duplicate decision ids.
scripts/check-conflicts.sh
scripts/check-decisions.sh
go test -race ./...

# MyGo needs no cgo, and contr must not add any.
CGO_ENABLED=0 go build -o /dev/null ./cmd/contr
# The bundle and install scripts, with dummy binaries. They write only under tmp/.
scripts/test-bundle.sh
scripts/test-install.sh
# The agent metrics script, on made-up transcripts.
scripts/test-agent-metrics.sh
# The two checks above, on small temporary repos.
scripts/test-checks.sh
echo "check: ok"
