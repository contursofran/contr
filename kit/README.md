# contr kit

A Claude Code plugin for agent work with `contr agents`:

- `agents/`: the builder, planner and reviewer agent definitions.
- `commands/orchestrate.md`: the coordinator playbook, as `/contr:orchestrate <goal>`.
- `hooks/hooks.json`: the Stop hook (a worker's turn that sent no report; a coordinator's state
  before a compaction) and the SessionStart hook after a compaction (the playbook comes back).
- `.mcp.json`: starts `contr mcp`, which gives the session the agent tools (spawn, message,
  interrupt, stop, read, list, clear_ended). `stop` removes the
  worker's terminal unless `keep` is set. `contr` must be on the PATH; `scripts/install.sh` links it.

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
   To prepare new worktrees of a project, add its main checkout and a
   shell command: `"setup": {"~/code/app": "cp -R \"$CONTR_MAIN/.husky/_\"
   .husky/ && pnpm install --frozen-lockfile --offline"}`. Spawn runs it
   in the worktree before the agent starts.
2. Start the coordinator in an hc terminal with the plugin:
   `claude --plugin-dir <kit> --model opus --effort medium`, then
   `/contr:orchestrate <goal>`.
3. Workers that the coordinator starts get its address in
   `CONTR_COORDINATOR` and report with `hc send`. When a worker's turn ends
   without one, the kit's Stop hook (`hooks/hooks.json`, `contr hook stop`)
   sends the coordinator "ended its turn without a report. Last words: …".
   The hook does nothing in sessions without `CONTR_COORDINATOR`.
4. Compaction: in a coordinator (a session that ran `/contr:orchestrate`, or an
   hc terminal with role orchestrator), the Stop hook keeps the turn going
   once when the context passes `CONTR_SAVE_AT` tokens (default 220000, under
   a 250000 `autoCompactWindow`) and asks for `COORDINATOR-STATE.md`. After
   Claude Code compacts, the SessionStart hook (matcher `compact`) adds the
   playbook, the state file to read, the coordinator's live workers and its
   open owner items. Claude Code's own auto-compaction stays on.

## Todos and artifacts

In an hc terminal (`HC_TERMINAL_ID` set), two hooks keep the agent's todo
list and artifacts in `<state dir>/session/<terminal>.json` (D195):

- PostToolUse (`contr hook post-tool`): `TodoWrite` replaces the todos;
  `TaskCreate` and `TaskUpdate` add or change one by id; an
  artifacts call (publish, edit, read or file tools; not list, guide or db
  tools) that names a slug moves that artifact to the top (at most 20).
- UserPromptSubmit (`contr hook prompt`): asks Claude to add each request of
  the owner to its todos, starting with `[you] `. Prompts that start with
  `<hc from=` are skipped, and workers (`CONTR_COORDINATOR` set) get nothing.

Both hooks log errors to stderr and always exit 0, so they never block
Claude Code.

## Metrics

`scripts/agent-metrics.py --days 7` reports the tokens and cost of coordinator
and worker sessions per model, and CI and PR flow from GitHub. Run it from the
repository folder; `--help` lists the options.
