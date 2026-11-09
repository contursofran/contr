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

# ident of a process, as explore.sh records it: "<command name>|<start time>".
ident() { printf '%s|%s\n' "$(basename "$(ps -p "$1" -o comm=)")" "$(ps -p "$1" -o lstart=)"; }

# stop ends a fake test binary that ignores the stop file, and a fake app that have the recorded ident (a fake go, with no ident, stays), and removes the scratch folder and
# the state files.
scratch="$(mktemp -d /tmp/contr-hexplore.XXXXXX)"
mkdir -p "$scratch/home"
ln -s /bin/sleep "$tmp/contr-explore"
ln -s /bin/sleep "$tmp/contr.test"
"$tmp/contr.test" 300 &
testpid=$!
sleep 300 &
gopid=$!
"$tmp/contr-explore" 300 &
apppid=$!
# Bystanders: processes that are not the recorded ones.
sleep 300 &
other=$!
pids=("$testpid" "$gopid" "$apppid" "$other")
sleep 0.3
for p in "${pids[@]}"; do kill -0 "$p" 2>/dev/null || fail "a fake process did not start"; done
printf 'gopid=%s\ntestpid=%s\ntestid=%s\napppid=%s\nappid=%s\n' \
	"$gopid" "$testpid" "$(ident "$testpid")" "$apppid" "$(ident "$apppid")" >"$EXPLORE_DIR/state"
printf 'app=contr-explore\npid=%s\ntestpid=%s\nscratch=%s\nhc_session=\n' "$apppid" "$testpid" "$scratch" >"$EXPLORE_DIR/info"
run stop
[ "$rc" = 0 ] || fail "stop: want exit 0, got $rc: $err"
sleep 0.5
kill -0 "$testpid" 2>/dev/null && fail "stop left the test binary"
kill -0 "$gopid" 2>/dev/null || fail "stop ended go, which is not recorded"
kill -0 "$apppid" 2>/dev/null && fail "stop left the app"
kill -0 "$other" 2>/dev/null || fail "stop ended a process that is not recorded"
[ ! -e "$scratch" ] || fail "stop left the scratch folder"
for f in state info stop; do [ ! -e "$EXPLORE_DIR/$f" ] || fail "stop left $f"; done

# A stale state file: the pids now belong to other processes. A bystander as
# gopid, testpid and apppid, with no ident or with another ident, survives.
scratch="$(mktemp -d /tmp/contr-hexplore.XXXXXX)"
"$tmp/contr-explore" 300 &
by1=$!
sleep 300 &
by2=$!
pids+=("$by1" "$by2")
sleep 0.3
kill -0 "$by1" 2>/dev/null && kill -0 "$by2" 2>/dev/null || fail "a fake process did not start"
printf 'gopid=%s\ntestpid=%s\ntestid=contr.test|Mon Jan  1 00:00:00 2001\napppid=%s\nappid=contr-explore|Mon Jan  1 00:00:00 2001\n' \
	"$by2" "$by2" "$by1" >"$EXPLORE_DIR/state"
printf 'app=contr-explore\npid=%s\ntestpid=%s\nscratch=%s\nhc_session=\n' "$by1" "$by2" "$scratch" >"$EXPLORE_DIR/info"
run stop
[ "$rc" = 0 ] || fail "stale stop: want exit 0, got $rc: $err"
kill -0 "$by1" 2>/dev/null || fail "stale stop ended a process with another start time (comm matches)"
kill -0 "$by2" 2>/dev/null || fail "stale stop ended a bystander"
[ ! -e "$scratch" ] || fail "stale stop left the scratch folder"
[ ! -e "$EXPLORE_DIR/state" ] || fail "stale stop left the state file"

# stop never removes a folder outside /tmp/contr-h*.
keep="$tmp/keep"
mkdir -p "$keep"
printf 'gopid=\n' >"$EXPLORE_DIR/state"
printf 'app=contr-explore\npid=\nscratch=%s\nhc_session=\n' "$keep" >"$EXPLORE_DIR/info"
run stop
[ -d "$keep" ] || fail "stop removed a folder outside /tmp/contr-h*"

echo "test-explore.sh: ok"
