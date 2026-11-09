# UsageMeter

The info block at the bottom of the files panel shows the context of the selected agent. The plan meters (how much of each plan is used) are in the sidebar foot, left of the Settings button (D196).

Design system page: component UsageMeter. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![UsageMeter, dark](images/UsageMeter-dark.png) | ![UsageMeter, light](images/UsageMeter-light.png) |

## Anatomy

| Part | Value |
|---|---|
| Block | line `hairline` on top, padding `12px 8px 2px`, gap `space-7` |
| Row | label `small`, `ink-42`; value mono `value`, `ink-62`, at the right |
| Meter | height `meter-height` (4px), track `meter-track`, fill `meter-fill`, radius `radius-2` |

Rows: "Context  90k of 200k  45%" only.

Plan meters, in the sidebar foot row (D196):

| Part | Value |
|---|---|
| Group | one row, inline, gap `space-7`; `space-6` more between two plans |
| Label | `small`, `ink-42` |
| Meter | width 44px, the meter above |
| Value | mono `value`, `ink-62` |

"Claude ▬ 38%  Codex ▬ 12%", then free space, then the Settings button.

## States

- **Default.**
- **Above 80%:** not in the design. Keep `meter-fill`; the number says it.
- **No data:** the value shows "—"; the meter is empty.

## Behavior

- Under the file tree: Context of the selected agent.
- Sidebar foot: one meter per plan (Claude, Codex), as percent used. Plan
  usage is global, so it does not follow the selection (D196). The icon
  column (D128) has no room: it shows only the Settings button.

## Data

- Context (D144): `contr statusline` saves the tokens and window size of an
  agent in `usage/context/<HC_TERMINAL_ID>.json`. The sampler reads the folder
  with the plans. The line shows the file of the selected orchestrator, worker
  or plain terminal as "90k of 200k  45%", and the meter fills to that percent.
  No file, no selection or a worktree row: "—" and an empty meter. The
  context is up to one sampler period (60 s) old.
- Plans: `internal/usage` on a goroutine; read again on a status line or log
  change. The view reads the sampler's last result and touches no file.
- Claude plan (D203): Claude Code gives the plan limits (`rate_limits`) only
  to its status line command. Each Claude session that contr starts (workers
  and "New orchestrator") gets `--settings` with a `statusLine` that runs
  `<contr> statusline`. That command saves the windows to `usage/claude.json`
  and the context, and prints a short line ("opus · 90k of 200k"). A Claude
  session that contr did not start gives plan data only when the owner's own
  status line runs `contr statusline`.
- Codex plan: the Codex session logs.

## Keys and focus

- None.

## Edge cases

- No data: "—" and an empty meter.

## Accessibility

- Meter role with value 0–100 and a label.

## Tokens

`hairline`, `meter-track`, `meter-fill`, `meter-height`, `radius-2`, `space-7`, `ink-42`, `ink-62`, `small`, `value`.

## Build in MyGo

- `ui.Meter` for each bar (role meter, value 0–100). Style it with the tokens.
- Data: `internal/usage` (Claude status line, Codex logs).

## Do and do not

**Do**

- Show whole percents.

**Don't**

- Do not color the meters red or amber.

## Tests

- View: values, no data, 100%.
- The files panel has only Context; the sidebar foot has Claude, then Codex, then the gear, in one row; the icon column has no plan meters.

## Replaces

- The plan bars of the v0.2 footer (`statusbar.go`).
