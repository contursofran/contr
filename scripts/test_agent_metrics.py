"""Unit tests for agent-metrics.py: prices, GitHub parsing, CI and PR flow, error paths. Run by test-agent-metrics.sh."""

import importlib.util
import json
import os
import sys

spec = importlib.util.spec_from_file_location("metrics", os.path.join(os.path.dirname(__file__), "agent-metrics.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def u(inp=0, out=0, cr=0, cw5=0, cw1h=0):
    return {"inp": inp, "out": out, "cr": cr, "cw5": cw5, "cw1h": cw1h}


def test_prices():
    assert m.price("unknown-model", u(inp=1)) is None
    # A 1-hour cache write costs more than a 5-minute one.
    assert m.price("claude-opus-5-5", u(cw1h=1_000_000)) == 8
    assert m.price("claude-opus-5-5", u(cw5=1_000_000)) == 5
    # Haiku's higher tier applies above 100k context tokens.
    assert abs(m.price("claude-haiku-5-5", u(inp=1000)) - 1000 * 0.1 / 1e6) < 1e-12
    assert abs(m.price("claude-haiku-5-5", u(inp=150_000)) - 150_000 * 0.5 / 1e6) < 1e-12
    # The 5m and 1h parts come from cache_creation; with none, all cache writes are 5m.
    assert m.usage({"usage": {"cache_creation_input_tokens": 10}})["cw5"] == 10
    split = m.usage({"usage": {"cache_creation_input_tokens": 10, "cache_creation": {"ephemeral_1h_input_tokens": 4,
                                                                                    "ephemeral_5m_input_tokens": 6}}})
    assert (split["cw5"], split["cw1h"]) == (6, 4)


RUNS = "\n".join([
    "CI\tsuccess\t2026-11-01T10:00:00Z\t2026-11-01T10:00:10Z\t2026-11-01T10:02:10Z\tagent/1",
    "CI\tfailure\t2026-11-01T11:00:00Z\t2026-11-01T11:00:30Z\t2026-11-01T11:01:30Z\tagent/1",
    "CI\tfailure\t2026-11-01T12:00:00Z\t2026-11-01T12:00:00Z\t2026-11-01T12:01:00Z\tmain",
    "CI\tskipped\t2026-11-01T13:00:00Z\t2026-11-01T13:00:00Z\t2026-11-01T13:00:01Z\tagent/1",
    "Docs\t\t2026-11-01T14:00:00Z\t\t\tagent/2",
]) + "\n"
PRS = [
    {"number": 1, "title": "One", "createdAt": "2026-11-01T09:00:00Z", "mergedAt": "2026-11-01T13:00:00Z",
     "headRefName": "agent/1", "baseRefName": "main",
     "commits": [{"committedDate": "2026-11-01T08:00:00Z"}, {"committedDate": "2026-11-01T10:30:00Z"}]},
    {"number": 2, "title": "Two", "createdAt": "2026-11-01T09:00:00Z", "mergedAt": "2026-11-01T10:00:00Z",
     "headRefName": "agent/2", "baseRefName": "main", "commits": []},
]


def fake_gh(replies):
    def gh(*args, timeout=120):
        for key, reply in replies.items():
            if key in args:
                return reply
        raise AssertionError(f"unexpected gh call {args}")
    return gh


def test_github_flow():
    m.gh = fake_gh({"api": (RUNS, None), "pr": (json.dumps(PRS), None)})
    flow = m.github("o/r", 0)
    wf = {w["name"]: w for w in flow["workflows"]}
    # The skipped run and the run that never started are left out.
    assert set(wf) == {"CI"}, wf
    ci = wf["CI"]
    assert (ci["runs"], ci["failures"]) == (3, 2), ci
    assert ci["queue_median"] == 10 and ci["run_median"] == 60 and ci["run_total"] == 240, ci
    prs = flow["prs"]
    assert prs["merged"] == 2
    one = next(p for p in prs["slowest"] if p["number"] == 1)
    # One commit after the PR opened; one failed run on its branch (the failure on the base branch is not counted).
    assert (one["hours"], one["fix_commits"], one["failed_ci"]) == (4, 1, 1), one
    assert prs["slowest"][0]["number"] == 1  # slowest first


def test_github_errors():
    m.gh = fake_gh({"api": (None, "HTTP 404")})
    assert m.github("o/r", 0) == {"repo": "o/r", "error": "HTTP 404"}
    m.gh = fake_gh({"api": ("", None), "pr": (None, "timed out")})
    flow = m.github("o/r", 0)
    assert flow["workflows"] == [] and flow["prs_error"] == "timed out" and "prs" not in flow
    assert m.pr_flow([], []) == {"merged": 0}


for name, fn in list(globals().items()):
    if name.startswith("test_") and callable(fn):
        fn()
print("test_agent_metrics.py: ok")
