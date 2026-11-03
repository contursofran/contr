# contr kit

A Claude Code plugin for agent work with `contr agents`:

- `agents/`: the builder, planner and reviewer agent definitions.
- `commands/orchestrate.md`: the coordinator playbook, as `/contr:orchestrate <goal>`.
- `.mcp.json`: starts `contr mcp`, which gives the session the agent tools (spawn, message,
  interrupt, stop, read, list). `contr` must be on the PATH; `scripts/install.sh` links it.

## Use

1. Point `contr agents` at this folder: set `CONTR_KIT` to its path, or put
   `"kit": "<path>"` in the profiles file (`contr agents help` names it).
   A claude profile with an agent file here then starts as that agent:
   `claude --plugin-dir <kit> --agent contr:<profile> …`.
2. Start the coordinator in an hc terminal with the plugin:
   `claude --plugin-dir <kit> --model opus --effort medium`, then
   `/contr:orchestrate <goal>`.
3. Workers that the coordinator starts get its address in
   `CONTR_COORDINATOR` and report with `hc send`.

## Metrics

`scripts/agent-metrics.py --days 7` reports the tokens and cost of coordinator
and worker sessions per model, and CI and PR flow from GitHub. Run it from the
repository folder; `--help` lists the options.
