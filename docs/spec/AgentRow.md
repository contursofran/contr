# AgentRow

A two-line sidebar row for one agent: the orchestrator sits at the top of its project, and its workers list under it on a thin rail.

Design system page: component AgentRow. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Row | orchestrator padding `7px 10px`, worker padding `5px 10px`, gap `space-10`, radius `radius-8` |
| Leading | Claude glyph (orchestrator) or braille StatusGlyph (worker) |
| Line 1 | title in `body`, `ink-60`; one line, ellipsis |
| Line 2 | mono `meta` (11px), `ink-42`, items `space-10` apart: issue id, model, then the age at the right ("b-241  sonnet  6m") |
| Kids rail | workers indent 17px with 9px padding; a 1px `rail` line on the left |

## States

- **Default.**
- **Hover:** fill `hover`.
- **Selected:** no fill. Title turns `ink-100`, line 2 `ink-62`. The tabs and the right panel follow the selected row.
- **Needs you:** the glyph turns `blocked`. The row is not otherwise colored.
- **Waiting on CI:** idle glyph; line 2 adds "CI".

## Behavior

- The orchestrator row is first under its project. Its workers list under
  it on the rail.
- Click selects the agent: the title bar shows its tabs, the terminal
  shows its main terminal, the files panel shows its worktree.
- Line 2: issue id, model, age. Ages follow D35 (state change or repaint)
  until open point O10 settles.

## Data

- `internal/agents` (list, watch) through `Window.Update`.

## Keys and focus

- ↑ ↓ move; ⏎ focuses the terminal.

## Edge cases

- A worker with no issue: line 2 starts with the model.
- An ended worker: removed (D112).
- A worker of another orchestrator in the same project: under its own
  orchestrator.

## Accessibility

- Label: "<title>, <state>, <issue>, <model>".

## Tokens

`hover`, `rail`, `ink-100`, `ink-62`, `ink-60`, `ink-42`, `space-10`, `radius-8`, `body`, `meta`.

## Build in MyGo

- A `kit.Row` in the same `ui.List` as the projects. The two lines are `Text(...).NoWrap().Ellipsis()`.
- Data: `internal/agents` (list, watch). Watch events reach the view through `Window.Update`.
- Ages follow D35 until open point O10 settles.
- Keys: ↑ ↓ move the selection, ⏎ focuses the terminal.
- `.ContextMenu(...)`: Interrupt, Stop, Open PR.

## Do and do not

**Do**

- Put the orchestrator first, whatever its state.
- Keep line 2 short: id, model, age.

**Don't**

- Do not show ended workers: stop removes them.
- Do not show the full task text: the title is enough.

## Tests

- View: orchestrator and worker rows, selected, needs you, waiting on CI.
- View: click selects the agent and the tabs change.

## Replaces

- The Agents section of `agents.go` (D54 line 2, state groups).
