#!/bin/sh
# Tests agent-metrics.py on made-up transcripts. Writes only under tmp/.
set -eu
cd "$(dirname "$0")/.."
dir=tmp/test-agent-metrics
rm -rf "$dir"
mkdir -p "$dir/projects/p1" "$dir/projects/p2" "$dir/projects/p3"
now=$(python3 -c 'import datetime;print(datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))')
old="2020-01-01T00:00:00Z"
msg() { # cwd time id model input output command; the tool call id is the message id
	printf '{"type":"assistant","cwd":"%s","timestamp":"%s","message":{"id":"%s","model":"%s","usage":{"input_tokens":%s,"output_tokens":%s,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0},"content":[{"type":"tool_use","id":"tu-%s","name":"Bash","input":{"command":"%s"}}]}}\n' "$1" "$2" "$3" "$4" "$5" "$6" "$3" "$7"
}
# A coordinator in the project folder: two spawns, one record written twice, one turn before the window.
# A second coordinator spawned only before the window and keeps working in it.
{
	msg /w/demo "$old" n0 claude-opus-5-5 1 1 "contr agents spawn --name y --profile builder"
	msg /w/demo "$now" n1 claude-opus-5-5 1 1 "gh pr merge 1"
} >"$dir/projects/p3/coord2.jsonl"
{
	msg /w/demo "$now" m1 claude-opus-5-5 100 50 "contr agents spawn --name b-1 --profile builder"
	msg /w/demo "$now" m1 claude-opus-5-5 100 50 "contr agents spawn --name b-1 --profile builder"
	msg /w/demo "$now" m2 claude-opus-5-5 200 100 "contr agents spawn --name r-1 --profile reviewer && ls"
	msg /w/demo "$old" m0 claude-opus-5-5 9999 9999 "contr agents spawn --name x --profile builder"
	echo 'not json'
} >"$dir/projects/p1/coord.jsonl"
# A worker in a worktree of the project.
{
	msg /w/demo-wt/b-1 "$now" w1 claude-sonnet-5-5 1000 500 "go test ./..."
	msg /w/demo-wt/b-1 "$now" w2 some-new-model 5 5 "ls"
} >"$dir/projects/p2/worker.jsonl"
# Another session: neither a worker nor a coordinator.
msg /w/other "$now" o1 claude-sonnet-5-5 7 7 "ls" >"$dir/projects/p3/other.jsonl"

CLAUDE_CONFIG_DIR="$dir" python3 scripts/agent-metrics.py --project demo --no-github --json >"$dir/out.json"
python3 - "$dir/out.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
rows = {(r["role"], r["model"]): r for r in d["sessions"]}
assert set(rows) == {("coordinator", "claude-opus-5-5"), ("worker", "claude-sonnet-5-5"), ("worker", "some-new-model")}, rows
c, w = rows[("coordinator", "claude-opus-5-5")], rows[("worker", "claude-sonnet-5-5")]
assert c["turns"] == 3 and c["inp"] == 301 and c["out"] == 151 and c["sessions"] == 2, c
assert d["spawned"] == 2, d["spawned"]  # the record written twice counts once
assert w["turns"] == 1 and w["inp"] == 1000, w
want = (1000 * 2 + 500 * 10 + 1000 * 0.1) / 1e6
assert abs(w["cost"] - want) < 1e-9, (w["cost"], want)
assert d["github"] is None
new = rows.get(("worker", "some-new-model"))
assert new and new["unpriced"] == 1 and new["cost"] == 0, new
assert w["unpriced"] == 0, w
PY
CLAUDE_CONFIG_DIR="$dir" python3 scripts/agent-metrics.py --project demo --no-github >"$dir/out.txt"
grep -q "coordinators started 2 workers" "$dir/out.txt"
grep -q '\$0.00\*' "$dir/out.txt"
grep -q "some turns use a model with no price here" "$dir/out.txt"
python3 scripts/test_agent_metrics.py
echo "test-agent-metrics.sh: ok"
