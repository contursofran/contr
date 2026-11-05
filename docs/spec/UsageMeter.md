# UsageMeter

The info block at the bottom of the files panel shows the context of the selected agent and how much of each plan is used.

Design system page: component UsageMeter. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Block | line `hairline` on top, padding `12px 8px 2px`, gap `space-7` |
| Row | label `small`, `ink-42`; value mono `value`, `ink-62`, at the right |
| Meter | height `meter-height` (4px), track `meter-track`, fill `meter-fill`, radius `radius-2` |

Rows: "Context  90k of 200k  45%", then one meter per plan (Claude, Codex).

## States

- **Default.**
- **Above 80%:** not in the design. Keep `meter-fill`; the number says it.
- **No data:** the value shows "—"; the meter is empty.

## Behavior

- Under the file tree: Context of the selected agent, then one meter per
  plan (Claude, Codex), as percent used.

## Data

- Context: no source yet (D139). The line shows "—" and an empty meter until
  #372 adds one (`contr statusline` writes it per terminal).
- Plans: `internal/usage` on a goroutine; read again on a status line or log
  change. The view reads the sampler's last result and touches no file.

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

## Replaces

- The plan bars of the v0.2 footer (`statusbar.go`).
