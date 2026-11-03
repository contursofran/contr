#!/usr/bin/env python3
"""Agent metrics for contr: what coordinator and worker sessions spend, and how CI and PRs flow.

Usage: agent-metrics.py [--days N] [--project NAME] [--repo OWNER/NAME] [--no-github] [--json]   (default 7 days)

Sessions come from Claude Code transcripts: $CLAUDE_CONFIG_DIR/projects/*/*.jsonl (default
~/.claude/projects). A session is
  - a worker when its folder is a worktree of the project: <...>/<project>-wt/<branch>
  - a coordinator when it runs `contr agents spawn`, in the project folder or another one
Other sessions are left out. The project defaults to the name of the repository in the current folder.
Turns and tokens count from the window start, by record time. Cost is an estimate at the list prices below.

From GitHub (gh, unless --no-github), for --repo (default: the current folder's repository):
  CI per workflow: runs, failures, median queue and run time, total run time
  PR flow over merged PRs: hours from open to merge, fix commits after open, failed CI runs per PR
"""

import argparse
import collections
import json
import os
import re
import statistics
import subprocess
import sys
import time
from datetime import datetime, timezone

# List prices in USD per million tokens. Not checked against the provider's price page. A model's `tier`
# applies to a request whose input + cache read + cache write is above its threshold.
PRICES = {
    "claude-opus-5-5": {"inp": 4, "out": 20, "cr": 0.2, "cw5": 5, "cw1h": 8},
    "claude-sonnet-5-5": {"inp": 2, "out": 10, "cr": 0.1, "cw5": 2.5, "cw1h": 4},
    "claude-haiku-5-5": {
        "inp": 0.1, "out": 0.5, "cr": 0.01, "cw5": 0.125, "cw1h": 0.2,
        "tier": (100_000, {"inp": 0.5, "out": 2.5, "cr": 0.05, "cw5": 0.625, "cw1h": 1}),
    },
    "claude-fable-5-1": {"inp": 10, "out": 50, "cr": 0.25, "cw5": 12.5, "cw1h": 20},
}
RATE_KEYS = ("inp", "out", "cr", "cw5", "cw1h")
SPAWN = re.compile(r"\bcontr\s+agents\s+spawn\b")
PR_LIMIT = 200


def epoch(stamp):
    return datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp()


def usage(msg):
    u = msg.get("usage") or {}
    cc = u.get("cache_creation") or {}
    cw1h = cc.get("ephemeral_1h_input_tokens") or 0
    cw_all = u.get("cache_creation_input_tokens") or 0
    return {
        "inp": u.get("input_tokens") or 0,
        "out": u.get("output_tokens") or 0,
        "cr": u.get("cache_read_input_tokens") or 0,
        "cw5": cc.get("ephemeral_5m_input_tokens", cw_all - cw1h) or 0,
        "cw1h": cw1h,
    }


def price(model, u):
    """USD for one request, or None when PRICES has no entry for the model."""
    p = PRICES.get(model)
    if p is None:
        return None
    rates = p
    if "tier" in p and u["inp"] + u["cr"] + u["cw5"] + u["cw1h"] > p["tier"][0]:
        rates = p["tier"][1]
    return sum(u[k] * rates[k] for k in RATE_KEYS) / 1e6


def records(path):
    """The user and assistant records of one transcript, in file order. Bad lines are skipped."""
    with open(path, "rb") as fh:
        for line in fh:
            if b'"user"' not in line and b'"assistant"' not in line:
                continue
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if d.get("type") in ("user", "assistant") and d.get("timestamp"):
                yield d


def spawns(msg, seen):
    """How many `contr agents spawn` Bash calls one assistant record makes. A tool call that seen (a set of tool
    call ids) already holds counts once."""
    n = 0
    for block in msg.get("content") or []:
        if isinstance(block, dict) and block.get("type") == "tool_use" and block.get("name") == "Bash":
            if block.get("id") in seen:
                continue
            seen.add(block.get("id"))
            n += len(SPAWN.findall(str((block.get("input") or {}).get("command", ""))))
    return n


def role(cwd, spawned, project):
    if f"{os.sep}{project}-wt{os.sep}" in cwd + os.sep:
        return "worker"
    if spawned:
        return "coordinator"
    return None


def scan(root, start, project):
    """One row per (role, model): sessions, turns, tokens, cost, and how many workers coordinators spawned."""
    rows = collections.defaultdict(lambda: {"sessions": set(), "turns": 0, "inp": 0, "out": 0, "cr": 0, "cw": 0,
                                            "cw5": 0, "cw1h": 0, "cost": 0.0, "unpriced": 0})
    spawned_total = 0
    for folder in sorted(os.listdir(root)) if os.path.isdir(root) else []:
        d = os.path.join(root, folder)
        if not os.path.isdir(d):
            continue
        for name in sorted(os.listdir(d)):
            if not name.endswith(".jsonl"):
                continue
            # The role uses spawns over the whole file, so a coordinator that started its workers
            # before the window still counts; "workers started" counts only spawns in the window.
            cwd, ever, n_spawn, turns, tools = "", 0, 0, {}, set()
            for r in records(os.path.join(d, name)):
                cwd = cwd or r.get("cwd") or ""
                if r["type"] != "assistant":
                    continue
                msg = r.get("message") or {}
                n = spawns(msg, tools)
                ever += n
                if epoch(r["timestamp"]) < start:
                    continue
                n_spawn += n
                # A message split over several records counts once, with the usage of its last record.
                turns[msg.get("id") or id(r)] = (msg.get("model") or "?", usage(msg))
            kind = role(cwd, ever > 0, project)
            if kind is None or not turns:
                continue
            spawned_total += n_spawn
            for model, u in turns.values():
                row = rows[(kind, model)]
                row["sessions"].add(name)
                row["turns"] += 1
                for k in ("inp", "out", "cr", "cw5", "cw1h"):
                    row[k] += u[k]
                row["cw"] += u["cw5"] + u["cw1h"]
                cost = price(model, u)
                if cost is None:
                    row["unpriced"] += 1
                else:
                    row["cost"] += cost
    out = [{"role": k[0], "model": k[1], **{**v, "sessions": len(v["sessions"])}} for k, v in rows.items()]
    out.sort(key=lambda r: (r["role"], -r["cost"]))
    return out, spawned_total


def gh(*args, timeout=120):
    """Runs gh; returns (stdout, None), or (None, why) when it failed."""
    try:
        p = subprocess.run(["gh", *args], capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return None, "timed out"
    except OSError as e:
        return None, str(e)
    if p.returncode != 0:
        lines = p.stderr.strip().splitlines()
        return None, lines[0][:200] if lines else f"gh exited with {p.returncode}"
    return p.stdout, None


def workflow_rows(runs):
    wf = collections.defaultdict(lambda: {"failures": 0, "queue": [], "run": []})
    for r in runs:
        w = wf[r["name"]]
        w["failures"] += r["concl"] in ("failure", "timed_out")
        w["queue"].append(epoch(r["started"]) - epoch(r["created"]))
        w["run"].append(epoch(r["updated"]) - epoch(r["started"]))
    rows = [{"name": n, "runs": len(w["run"]), "failures": w["failures"], "queue_median": statistics.median(w["queue"]),
             "run_median": statistics.median(w["run"]), "run_total": sum(w["run"])} for n, w in wf.items()]
    return sorted(rows, key=lambda w: -w["run_total"])


def pr_flow(prs, runs):
    bases = {p.get("baseRefName") for p in prs}
    failed = collections.Counter(r["branch"] for r in runs if r["concl"] == "failure" and r["branch"] not in bases)
    rows = []
    for p in prs:
        opened = epoch(p["createdAt"])
        rows.append({"number": int(p["number"]), "title": str(p["title"]),
                     "hours": (epoch(p["mergedAt"]) - opened) / 3600,
                     "fix_commits": sum(1 for c in p.get("commits", [])
                                        if c.get("committedDate") and epoch(c["committedDate"]) > opened),
                     "failed_ci": failed[p["headRefName"]]})
    rows.sort(key=lambda r: -r["hours"])
    if not rows:
        return {"merged": 0}
    return {"merged": len(rows), "hours_median": statistics.median(r["hours"] for r in rows),
            "fix_commits_median": statistics.median(r["fix_commits"] for r in rows),
            "failed_ci_median": statistics.median(r["failed_ci"] for r in rows), "slowest": rows[:5]}


def github(repo, start):
    """CI and PR flow of repo. A failure goes into `error`; it never stops the report."""
    day = datetime.fromtimestamp(start, timezone.utc).strftime("%Y-%m-%d")
    out, why = gh("api", "--paginate", f"repos/{repo}/actions/runs?per_page=100&created=>={day}", "--jq",
                  ".workflow_runs[]|[.name,.conclusion,.created_at,.run_started_at,.updated_at,.head_branch]|@tsv")
    if why:
        return {"repo": repo, "error": why}
    runs = []
    for line in out.splitlines():
        name, concl, created, started, updated, branch = (line.split("\t") + [""] * 6)[:6]
        if started and concl not in ("skipped", ""):
            runs.append({"name": name, "concl": concl, "created": created, "started": started,
                         "updated": updated, "branch": branch})
    flow = {"repo": repo, "workflows": workflow_rows(runs)}
    out, why = gh("pr", "list", "-R", repo, "--state", "merged", "--limit", str(PR_LIMIT), "--search",
                  f"merged:>={day}", "--json", "number,title,createdAt,mergedAt,headRefName,baseRefName,commits")
    if why:
        flow["prs_error"] = why
    else:
        flow["prs"] = pr_flow(json.loads(out), runs)
    return flow


def fmt_tok(n):
    return f"{n / 1e6:.1f}M" if n >= 1e6 else f"{n / 1e3:.0f}k"


def print_text(start, project, rows, spawned, flow):
    since = datetime.fromtimestamp(start, timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    print(f"Agent sessions of {project} since {since}; coordinators started {spawned} workers")
    print(f"{'role':<12}{'model':<20}{'sessions':>9}{'turns':>7}{'input':>8}{'output':>8}{'c.read':>8}{'c.write':>8}"
          f"{'cost':>10}")
    for r in rows:
        cost = f"${r['cost']:.2f}" + ("*" if r["unpriced"] else "")
        print(f"{r['role']:<12}{r['model']:<20}{r['sessions']:>9}{r['turns']:>7}{fmt_tok(r['inp']):>8}"
              f"{fmt_tok(r['out']):>8}{fmt_tok(r['cr']):>8}{fmt_tok(r['cw']):>8}{cost:>10}")
    if not rows:
        print("(no coordinator or worker sessions)")
    if any(r["unpriced"] for r in rows):
        print("* some turns use a model with no price here")
    if flow is None:
        return
    print(f"\nCI and PRs of {flow['repo']}")
    if "error" in flow:
        print(f"  CI: {flow['error']}")
        return
    for w in flow["workflows"]:
        print(f"  {w['name']}: {w['runs']} runs, {w['failures']} failed, queue {w['queue_median']:.0f}s, "
              f"run {w['run_median'] / 60:.1f}m median, {w['run_total'] / 60:.0f}m in all")
    prs = flow.get("prs")
    if prs is None:
        print(f"  PRs: {flow.get('prs_error')}")
    elif prs["merged"]:
        print(f"  {prs['merged']} PRs merged: {prs['hours_median']:.1f}h open to merge, "
              f"{prs['fix_commits_median']:g} fix commits and {prs['failed_ci_median']:g} failed CI runs (medians)")
        for p in prs["slowest"]:
            print(f"    #{p['number']} {p['hours']:.1f}h, {p['fix_commits']} fixes, {p['failed_ci']} failed CI: "
                  f"{p['title'][:60]}")
    else:
        print("  no PRs merged")


def current_repo():
    out, _ = gh("repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner", timeout=30)
    return (out or "").strip() or None


def main(argv):
    ap = argparse.ArgumentParser(description="Agent metrics for contr.")
    ap.add_argument("--days", type=float, default=7)
    ap.add_argument("--project", help="project folder name (default: the current repository's name)")
    ap.add_argument("--repo", help="OWNER/NAME for CI and PRs (default: the current folder's repository)")
    ap.add_argument("--no-github", action="store_true", help="skip CI and PRs")
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args(argv)
    start = time.time() - a.days * 86400
    repo = None if a.no_github else (a.repo or current_repo())
    project = a.project or (repo.split("/")[-1] if repo else os.path.basename(os.getcwd()))
    root = os.path.join(os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude"), "projects")
    rows, spawned = scan(root, start, project)
    flow = github(repo, start) if repo else None
    if a.json:
        json.dump({"since": start, "project": project, "sessions": rows, "spawned": spawned, "github": flow},
                  sys.stdout, indent=2)
        print()
    else:
        print_text(start, project, rows, spawned, flow)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
