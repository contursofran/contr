#!/bin/sh
# Starts a scratch contr that an agent can drive with agent-desktop (#522).
# It builds the release app under the name contr-explore (so --app never
# matches the owner's contr), starts a scratch hc core with scratch home and
# folders, loads a fixture of cmd/contr/e2e_see_test.go, keeps the app open
# and prints one JSON line: app, pid, window_id, scratch, hc_session.
# It never touches the owner's hc core. One scratch app runs at a time.
#
#   scripts/explore.sh                          list the fixtures
#   scripts/explore.sh <fixture> [dark|light]   start (default theme: dark)
#   scripts/explore.sh stop                     stop the app and the core,
#                                               remove the scratch folder
#
# Drive it: agent-desktop snapshot --app contr-explore --window-id <id> -i --compact
# Read a terminal: HC_SESSION=<hc_session> hc read ... (it needs the XDG
# folders of <scratch> too; see the harness appEnv).
#
# Env for tests: EXPLORE_DIR (state folder, default tmp/explore),
# EXPLORE_STOP_WAIT (seconds to wait for a clean stop, default 30).
set -eu
cd "$(dirname "$0")/.."

dir=${EXPLORE_DIR:-tmp/explore}
app=contr-explore
statefile=$dir/state
infofile=$dir/info
stopfile=$dir/stop
log=$dir/log.txt
wait_stop=${EXPLORE_STOP_WAIT:-30}

fixtures() {
	sed -n 's/^	"\([a-z]*\)": {$/\1/p' cmd/contr/e2e_see_test.go
}

alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

# stop_all ends what the state file lists: the test (which stops the core and
# removes the scratch folder), then, if that fails, the app, the holders, the
# core and the folder by hand. Only a folder /tmp/contr-h* is ever removed.
stop_all() {
	gopid= scratch= apppid= session=
	[ -f "$statefile" ] && gopid=$(sed -n 's/^gopid=//p' "$statefile")
	if [ -f "$infofile" ]; then
		scratch=$(sed -n 's/^scratch=//p' "$infofile")
		apppid=$(sed -n 's/^pid=//p' "$infofile")
		session=$(sed -n 's/^hc_session=//p' "$infofile")
	fi
	mkdir -p "$dir"
	: >"$stopfile"
	i=0
	while alive "$gopid" && [ "$i" -lt "$((wait_stop * 5))" ]; do
		sleep 0.2
		i=$((i + 1))
	done
	if alive "$gopid"; then
		kill -TERM "$gopid" 2>/dev/null || true
		sleep 1
		kill -KILL "$gopid" 2>/dev/null || true
	fi
	# The app: only a process that has the name of the scratch app.
	if alive "$apppid" && [ "$(basename "$(ps -p "$apppid" -o comm= 2>/dev/null)")" = "$app" ]; then
		kill -TERM "$apppid" 2>/dev/null || true
		sleep 0.5
		kill -KILL "$apppid" 2>/dev/null || true
	fi
	case "$scratch" in
	/tmp/contr-h?*)
		if [ -d "$scratch" ]; then
			pkill -TERM -f "hc hold --socket $scratch/" 2>/dev/null || true
			if [ -n "$session" ] && command -v hc >/dev/null 2>&1; then
				HC_SESSION=$session HOME=$scratch/home SHELL=/bin/sh \
					XDG_CONFIG_HOME=$scratch/co XDG_DATA_HOME=$scratch/da XDG_STATE_HOME=$scratch/st \
					XDG_RUNTIME_DIR=$scratch/ru XDG_CACHE_HOME=$scratch/ca \
					hc server stop >/dev/null 2>&1 || true
			fi
			rm -rf "$scratch"
		fi
		;;
	esac
	rm -f "$statefile" "$infofile" "$stopfile"
}

fixture=${1:-}
if [ -z "$fixture" ]; then
	scripts/see.sh
	exit 0
fi

if [ "$fixture" = stop ]; then
	if [ ! -f "$statefile" ]; then
		echo "explore: nothing to stop"
		exit 0
	fi
	stop_all
	echo "explore: stopped"
	exit 0
fi

theme=${2:-dark}
case "$theme" in
dark | light) ;;
*)
	echo "explore: theme '$theme': want dark or light" >&2
	exit 2
	;;
esac
if ! fixtures | grep -qx -- "$fixture"; then
	echo "explore: no fixture '$fixture'; known: $(fixtures | tr '\n' ' ')" >&2
	exit 2
fi
if [ $# -gt 2 ]; then
	echo "explore: too many arguments" >&2
	exit 2
fi

mkdir -p "$dir"
if [ -f "$statefile" ]; then
	echo "explore: a scratch app is already running; run 'scripts/explore.sh stop' first" >&2
	exit 1
fi
if ! command -v agent-desktop >/dev/null 2>&1; then
	echo "explore: agent-desktop is not installed" >&2
	exit 1
fi

abs=$(cd "$dir" && pwd)
rm -f "$infofile" "$stopfile"
CGO_ENABLED=0 go build -o "$abs/$app" ./cmd/contr

# From here on, Ctrl-C or an error cleans up. A start that works clears the trap.
started=0
cleanup() {
	[ "$started" -eq 1 ] || stop_all
}
trap cleanup EXIT
trap 'exit 130' INT TERM

CONTR_EXPLORE=$fixture CONTR_EXPLORE_THEME=$theme CONTR_EXPLORE_BIN=$abs/$app \
	CONTR_EXPLORE_INFO=$abs/info CONTR_EXPLORE_STOP=$abs/stop \
	nohup go test -tags e2e -count=1 -timeout 0 -run '^TestExplore$' -v ./cmd/contr >"$log" 2>&1 &
gopid=$!
echo "gopid=$gopid" >"$statefile"

# Wait for the info file (the app runs), then for its window.
i=0
while [ ! -f "$infofile" ]; do
	if ! alive "$gopid"; then
		echo "explore: the scratch run failed:" >&2
		tail -30 "$log" >&2
		exit 1
	fi
	i=$((i + 1))
	if [ "$i" -gt 600 ]; then
		echo "explore: the scratch run did not start in 120 s" >&2
		exit 1
	fi
	sleep 0.2
done
if grep -q -- '--- SKIP' "$log"; then
	echo "explore: skipped (is hc installed?)" >&2
	exit 1
fi

pid=$(sed -n 's/^pid=//p' "$infofile")
scratch=$(sed -n 's/^scratch=//p' "$infofile")
session=$(sed -n 's/^hc_session=//p' "$infofile")
win=
i=0
while [ -z "$win" ]; do
	win=$(agent-desktop list-windows --app "$app" 2>/dev/null |
		sed -n 's/.*"id":"\(w-[0-9]*\)".*/\1/p' | head -1) || true
	[ -n "$win" ] && break
	if ! alive "$pid"; then
		echo "explore: the app ended before it opened a window" >&2
		exit 1
	fi
	i=$((i + 1))
	if [ "$i" -gt 150 ]; then
		echo "explore: no window of '$app' after 30 s" >&2
		exit 1
	fi
	sleep 0.2
done
# Let the first paint and the fixture's workers settle.
sleep 1

started=1
printf '{"app":"%s","pid":%s,"window_id":"%s","scratch":"%s","hc_session":"%s"}\n' \
	"$app" "$pid" "$win" "$scratch" "$session"
