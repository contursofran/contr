#!/bin/sh
# Isolated herdr server for developing contr.
#
# The server runs with a cleared environment, its own HOME and XDG dirs, an
# empty config and its own sockets, all under ${TMPDIR:-/tmp}/contr-dev-herdr.
# Never point it at the real herdr config dir or the real sockets. The owner's
# herdr is not touched.
#
#   scripts/dev-herdr.sh         start the server and print the export line
#   scripts/dev-herdr.sh stop    stop that server
#
# Use it with: eval "$(scripts/dev-herdr.sh)"
#
# Environment:
#   CONTR_HERDR_BIN  the herdr binary (default: herdr on PATH)
#   CONTR_DEV_PATH   PATH inside the server, so real tools can run in its panes
#                    (default: the system directories only)

set -eu

base="${TMPDIR:-/tmp}"
base="${base%/}/contr-dev-herdr"
api_sock="$base/a.sock"
client_sock="$base/a-client.sock"
bin="${CONTR_HERDR_BIN:-herdr}"

usage() {
    echo "usage: $0 [start|stop]" >&2
}

# Runs herdr with the isolated environment. The arguments are herdr's.
run_herdr() {
    bin_path=$(command -v "$bin") || {
        echo "herdr not found: $bin" >&2
        exit 1
    }
    exec env -i \
        HOME="$base/home" \
        XDG_CONFIG_HOME="$base/xdg-config" \
        XDG_STATE_HOME="$base/xdg-state" \
        XDG_DATA_HOME="$base/xdg-data" \
        XDG_CACHE_HOME="$base/xdg-cache" \
        XDG_RUNTIME_DIR="$base/xdg-run" \
        TMPDIR="$base/tmp" \
        HERDR_CONFIG_PATH="$base/config.toml" \
        HERDR_SOCKET_PATH="$api_sock" \
        SHELL=/bin/sh \
        TERM=xterm-256color \
        LANG=en_US.UTF-8 \
        PATH="${CONTR_DEV_PATH:-/usr/bin:/bin:/usr/sbin:/sbin}" \
        "$bin_path" "$@"
}

start() {
    mkdir -p "$base/home" "$base/xdg-config" "$base/xdg-state" "$base/xdg-data" \
        "$base/xdg-cache" "$base/xdg-run" "$base/tmp"
    [ -f "$base/config.toml" ] || : >"$base/config.toml"
    # macOS refuses a unix socket path of 104 bytes or more.
    if [ "${#client_sock}" -ge 104 ]; then
        echo "socket path too long: $client_sock (set a shorter TMPDIR)" >&2
        exit 1
    fi
    if [ -S "$api_sock" ]; then
        echo "already running (stop it with: $0 stop)" >&2
    else
        nohup "$0" _herdr server >"$base/server.log" 2>&1 &
        tries=0
        until [ -S "$api_sock" ] && [ -S "$client_sock" ]; do
            tries=$((tries + 1))
            if [ "$tries" -gt 100 ]; then
                echo "herdr did not start; see $base/server.log" >&2
                exit 1
            fi
            sleep 0.1
        done
    fi
    printf 'export HERDR_SOCKET_PATH=%s CONTR_SURFACE=1\n' "$api_sock"
}

stop() {
    if [ ! -S "$api_sock" ]; then
        echo "no dev herdr running at $api_sock" >&2
        exit 0
    fi
    "$0" _herdr server stop
}

case "${1:-start}" in
    start) start ;;
    stop) stop ;;
    # Internal: run herdr with the isolated environment (start and stop call it).
    _herdr)
        shift
        run_herdr "$@"
        ;;
    *)
        usage
        exit 2
        ;;
esac
