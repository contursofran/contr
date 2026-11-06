# AgentRow

A two-line sidebar row for one agent: the orchestrator sits at the top of its project, and its workers list under it on a thin rail.

Design system page: component AgentRow. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![AgentRow, dark](images/AgentRow-dark.png) | ![AgentRow, light](images/AgentRow-light.png) |

## Anatomy

| Part | Value |
|---|---|
| Row | orchestrator padding `7px 10px`, worker padding `5px 10px`, gap `space-10`, radius `radius-8` |
| Leading | Claude glyph (orchestrator; grey `ink-45` for a Claude session that does not orchestrate, D191) or braille StatusGlyph (worker) |
| Line 1 | title in `body`, `ink-60`; one line, ellipsis. A worker shows its issue title, else its task (the terminal title); an orchestrator its name |
| Line 2 | mono `meta` (11px), `ink-42`, items `space-10` apart: worker name, model and effort, then the age at the right ("b-241  sonnet medium  6m"). An orchestrator shows its role in place of the name: "2 workers", or "orchestrator" with no workers yet; a Claude session that does not orchestrate shows none (D191) |
| Indent | the orchestrator row sits 16px in from its project row (D169) |
| Kids rail | workers indent a further 17px with 9px padding; a 1px `rail` line on the left |

## States

- **Default.**
- **Hover:** fill `hover`.
- **Selected:** no fill. Title turns `ink-100`, line 2 `ink-62`. The tabs and the right panel follow the selected row.
- **Needs you:** the glyph turns `blocked`. The row is not otherwise colored.
- **Waiting on CI:** idle glyph; line 2 adds "CI" after the model (D145). The data is the checks of the worker's pull request: a live worker that is not working, whose PR has unfinished checks.

## Behavior

- The orchestrator row is first under its project. Its workers list under
  it on the rail.
- Click selects the agent: the title bar shows its tabs, the terminal
  shows its main terminal, the files panel shows its worktree. The terminal
  takes the keyboard focus, also on a click on the row that is already
  selected, so the owner can type at once (D188).
- Line 1 of a worker is the title of its issue (D174); with none known, its
  task: the terminal title without its status glyph, when it is usable (not
  a path or a program name); else the worker name (D169).
- Line 2: worker name, model, age. The name is left out when line 1 shows
  it, and for an orchestrator. The model is the `--model` of the agent's
  profile, else the `--model` of the terminal's own command line (D169), else
  the model that Claude Code's status line reported for the terminal (D174).
  The kit's Stop hook labels each Claude Code session's terminal with its
  model and effort (`contr.model`, `contr.effort`), and these labels come
  after the command line and before the status line, so a session shows
  them after its first turn with no status line set up (D189). The effort
  follows the model, from the same places in the same order ("opus high").
  The issue id is in the accessibility label only. The age is the time since the last hc event for the terminal,
  as "now" (under a minute), "6m" or "2h".
- Ages (D137, O10): one shared 1-minute timer moves them. It runs only
  while the window is visible and an age is on screen (an agent row of an
  open project has one). It stops when the window hides or no age shows.
  Ages also move on every state change, because each build reads the clock.
- Context menu: Interrupt, Stop, Open PR. They call the agent source
  (`internal/agents`) off the UI thread; Open PR opens the pull request in the
  browser (`gh pr view --web`), never in the PR pane (D127), and is off while
  the agent has no pull request. A failure shows in the status message. Stop asks for no second click: the menu choice is the
  confirmation.
- A selected agent that is gone leaves the selection.
- The project row shows the selection for the project itself, and while the
  project is folded; an open project shows it on the agent row.

## Data

- `internal/agents` (list, watch) through `Window.Update`.

## Keys and focus

- ↑ ↓ move the selection over project and agent rows; ⏎ focuses the terminal (D145). They work in the icon column too.

## Edge cases

- A worker with no usable title: line 1 is its name; line 2 starts with the model.
- Model unknown: the age still sits at the right of line 2, under the title.
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
- Ages: `AgeClock`, one timer (D137). It starts and stops from `SpacesList`
  (an age shows) and the window visibility.
- Keys: ↑ ↓ move the selection, ⏎ focuses the terminal.
- `.ContextMenu(...)`: Interrupt, Stop, Open PR.

## Do and do not

**Do**

- Put the orchestrator first, whatever its state.
- Keep line 2 short: name, model, age.

**Don't**

- Do not show ended workers: stop removes them.
- Do not show the full task text: the title is enough.

## Tests

- View: orchestrator and worker rows, selected, needs you, waiting on CI.
- View: click selects the agent and the tabs change.

## Replaces

- The Agents section of `agents.go` (D54 line 2, state groups).

