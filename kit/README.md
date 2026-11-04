# contr kit

A Claude Code plugin for agent work with `contr agents`:

- `agents/`: the builder, planner and reviewer agent definitions.
- `commands/orchestrate.md`: the coordinator playbook, as `/contr:orchestrate <goal>`.
- `hooks/hooks.json`: the Stop hook that reports a worker's turn that sent no report.
- `.mcp.json`: starts `contr mcp`, which gives the session the agent tools (spawn, message,
  interrupt, stop, read, list). `contr` must be on the PATH; `scripts/install.sh` links it.

## Install

The repository root is a plugin marketplace (`.claude-plugin/marketplace.json`).
Install the kit once, from the repository folder:
`claude plugin marketplace add .`, then `claude plugin install contr@contr`.
Every `claude` session then has
`/contr:orchestrate`. Turn the plugin off or on in `/plugin`. The plugin
reads this folder in place, so a change to the kit needs no update step.
The `--plugin-dir` sessions below still work with the plugin installed.

## Use

1. Point `contr agents` at this folder: set `CONTR_KIT` to its path, or put
   `"kit": "<path>"` in the profiles file (`contr agents help` names it).
   A claude profile with an agent file here then starts as that agent:
   `claude --plugin-dir <kit> --agent contr:<profile> …`. A variant names
   its agent: `"planner-high": {"argv": ["claude", "--model", "opus",
   "--effort", "high"], "agent": "planner"}`. The `spawn` tool reads the
   profiles file on each spawn, so a new profile works at once.
2. Start the coordinator in an hc terminal with the plugin:
   `claude --plugin-dir <kit> --model opus --effort medium`, then
   `/contr:orchestrate <goal>`.
3. Workers that the coordinator starts get its address in
   `CONTR_COORDINATOR` and report with `hc send`. When a worker's turn ends
   without one, the kit's Stop hook (`hooks/hooks.json`, `contr hook stop`)
   sends the coordinator "ended its turn without a report. Last words: …".
   The hook does nothing in sessions without `CONTR_COORDINATOR`.

## Metrics

`scripts/agent-metrics.py --days 7` reports the tokens and cost of coordinator
and worker sessions per model, and CI and PR flow from GitHub. Run it from the
repository folder; `--help` lists the options.
