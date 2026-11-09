#!/bin/bash
# Test of the argument and stop logic of scripts/explore.sh. It starts no app,
# no core and no build: a bad argument stops before the build, and stop is run
# on fake processes. State is under tmp/ of the repository.
set -euo pipefail
unset CDPATH

repo="$(cd "$(dirname "$0")/.." && pwd)"
explore="$repo/scripts/explore.sh"
mkdir -p "$repo/tmp"
tmp="$(cd "$(mktemp -d "$repo/tmp/test-explore.XXXXXX")" && pwd)"
scratch=""
pids=()
cleanup() {
	for p in "${pids[@]:-}"; do [ -n "$p" ] && kill -KILL "$p" 2>/dev/null || true; done
	[ -n "$scratch" ] && rm -rf "$scratch"
	rm -rf "$tmp"
}
trap cleanup EXIT

fail() {
	echo "test-explore.sh: FAIL: $*" >&2
	exit 1
}

export EXPLORE_DIR="$tmp/state" EXPLORE_STOP_WAIT=1
mkdir -p "$EXPLORE_DIR"

# run: sets $rc, $out (stdout) and $err (stderr).
run() {
	rc=0
	"$explore" "$@" >"$tmp/out" 2>"$tmp/err" || rc=$?
	out=$(cat "$tmp/out")
	err=$(cat "$tmp/err")
}

# A bad theme and an unknown fixture are refused with exit 2, before any build.
run workers purple
[ "$rc" = 2 ] || fail "bad theme: want exit 2, got $rc"
case "$err" in *"want dark or light"*) ;; *) fail "bad theme: message: $err" ;; esac
run nosuchfixture
[ "$rc" = 2 ] || fail "unknown fixture: want exit 2, got $rc"
case "$err" in *"no fixture 'nosuchfixture'"*"workers"*) ;; *) fail "unknown fixture: message: $err" ;; esac
run workers dark extra
[ "$rc" = 2 ] || fail "extra argument: want exit 2, got $rc"
[ ! -f "$EXPLORE_DIR/state" ] || fail "a refused start left a state file"

# stop with nothing running is not an error.
run stop
[ "$rc" = 0 ] || fail "stop without state: want exit 0, got $rc"
case "$out" in *"nothing to stop"*) ;; *) fail "stop without state: message: $out" ;; esac

# A start with a state file is refused (one scratch app at a time).
echo "gopid=" >"$EXPLORE_DIR/state"
run workers
[ "$rc" = 1 ] || fail "second start: want exit 1, got $rc"
case "$err" in *"already running"*) ;; *) fail "second start: message: $err" ;; esac
rm -f "$EXPLORE_DIR/state"

# stop ends a fake test process that ignores the stop file, a fake app that
# has the app name, and removes the scratch folder and the state files.
scratch="$(mktemp -d /tmp/contr-hexplore.XXXXXX)"
mkdir -p "$scratch/home"
cp /bin/sleep "$tmp/contr-explore"
sleep 300 &
gopid=$!
"$tmp/contr-explore" 300 &
apppid=$!
# A bystander with another name must survive a stop that names its pid by mistake.
sleep 300 &
other=$!
pids=("$gopid" "$apppid" "$other")
printf 'gopid=%s\n' "$gopid" >"$EXPLORE_DIR/state"
printf 'app=contr-explore\npid=%s\nscratch=%s\nhc_session=\n' "$apppid" "$scratch" >"$EXPLORE_DIR/info"
run stop
[ "$rc" = 0 ] || fail "stop: want exit 0, got $rc: $err"
sleep 0.5
kill -0 "$gopid" 2>/dev/null && fail "stop left the test process"
kill -0 "$apppid" 2>/dev/null && fail "stop left the app"
kill -0 "$other" 2>/dev/null || fail "stop ended a process that is not the app"
[ ! -e "$scratch" ] || fail "stop left the scratch folder"
for f in state info stop; do [ ! -e "$EXPLORE_DIR/$f" ] || fail "stop left $f"; done

# stop never removes a folder outside /tmp/contr-h*.
keep="$tmp/keep"
mkdir -p "$keep"
printf 'gopid=\n' >"$EXPLORE_DIR/state"
printf 'app=contr-explore\npid=\nscratch=%s\nhc_session=\n' "$keep" >"$EXPLORE_DIR/info"
run stop
[ -d "$keep" ] || fail "stop removed a folder outside /tmp/contr-h*"

echo "test-explore.sh: ok"
