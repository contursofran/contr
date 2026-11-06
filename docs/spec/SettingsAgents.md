# SettingsAgents

The tab bar of the Settings modal and its Agents tab: the Roles section (D186). The Flow
section of the same tab has its own spec: [FlowSection.md](FlowSection.md) (D187).

Shared rules: [README.md](README.md). Design: board 10 of the from-zero canvas.

## Anatomy

- **Tab bar** (under the head, `ModalPadX` side padding): `General`, `Agents`, `Updates` on a
  `hover` track, `radius` of a card, 2px padding. The selected tab has the `selected` fill and
  `ink-100` text; the others `ink-62`. While an update waits, `Updates` has a 6px `ink-100`
  dot (D146).
- **General:** Appearance and the transparency sliders, then `Done`.
- **Updates:** the versions, the update status, the notes, and the buttons `Check for updates`,
  `Install and restart` or `Restart now` (D176), `Done`.
- **Agents**, from top to bottom:
  1. The Flow section ([FlowSection.md](FlowSection.md)). Items 1 to 4 scroll when the window
     is short; the foot stays.
  2. Head: `Roles` (bold, `ink-95`) and the hint `Saved to agents.json · new workers use it`
     (`ink-50`).
  3. Column heads `Role`, `CLI`, `Model`, `Effort` (`ink-50`).
  4. One row per role, a `hairline` above each: the name (`ink-95`) over what it does
     (`ink-50`), then three dropdowns (`SelectBase`: `input-line` border, card radius, the value left-aligned and a
     `chevron-down` icon (12px, `ink-50`) at the right; the
     popup on `ground` with `float-line`; the chosen and highlighted items on `selected`).
     Under the row, the command line in mono (`value` style, `ink-62`), one line with an
     ellipsis.
  5. Foot, `hairline` above: `Applies to agents started from now on` (`ink-50`),
     `Reset to defaults` (outlined button), `Done` (plain button).

| Role | Profile | Line |
|---|---|---|
| Orchestrator | `orchestrator` | plans and lands work |
| Planner | `planner` | writes PLAN.md |
| Builder | `builder` | builds, opens the PR |
| Reviewer | `reviewer` | reviews the PR head |
| Debugger | `debugger` | after failed fixes |

## Values

- CLI: `Claude Code` (`claude`). Codex and OpenCode come later.
- Model: `Opus 5.5` (`opus`), `Sonnet 5.5` (`sonnet`), `Haiku 5.5` (`haiku`).
- Effort: `low`, `medium`, `high`, `xhigh`, `max`.

## States

- **Custom:** a profile of another program, or with a model or effort that is not in the
  lists. The dropdowns that cannot show its value say `Custom` (`ink-50`); the command line
  shows the argv as it is. Nothing writes it until the owner picks a value.
- **Not set:** no `orchestrator` profile. The dropdowns say `not set`, and the line says
  `not set · suggested: claude --model opus --effort medium --permission-mode auto`.
- **Error:** a failed read or save shows under the head in `blocked`, and goes to the log.

## Behavior

- Each open of Settings reads agents.json again, off the UI thread.
- A pick sets `--model` or `--effort` in the profile's argv and keeps the other arguments. A
  pick on a profile of another program, or on a missing one, starts from the role's default.
  Then the config is saved off the UI thread.
- `Reset to defaults` puts each role back to `DefaultConfig`, and an orchestrator that exists
  to the suggested default. A missing orchestrator stays missing. The flow goes back to
  `DefaultFlow`.
- A change in the Flow section is saved the same way as a role pick.
- Saving writes only profiles that the file had or that differ from `DefaultConfig`, and keeps
  keys that contr does not know.

## Data

`agents.Config` from `agents.LoadConfig` and `Config.Save`, at `Frame.AgentsConfig`
(`CONTR_AGENTS_CONFIG`, else `contr/agents.json` in the user config folder).

## Keys and focus

The tab bar takes the arrows, Home and End (`TabsBase`). A dropdown opens with the arrows,
Enter or Space chooses, Escape closes (`SelectBase`).

## Edge cases

- No path: the tab shows `no agents.json path`.
- An unreadable file: `read failed: …`, and no rows.

## Accessibility

Each tab is named by its text. Dropdowns are named `<Role> CLI`, `<Role> model` and
`<Role> effort`; their items `<dropdown> <value>`.

## Build in MyGo

`ui.TabsBase`, `ui.SelectBase`, `Text.SingleLine().Ellipsis`. Code:
`internal/app/settings.go` (tab bar), `internal/app/settings_agents.go` (Agents tab).

## Tests

`internal/app/settings_agents_test.go` and the Save tests in `internal/agents/flow_test.go`.

## Replaces

The one-page Settings modal of D115 and D162; editing agents.json by hand.
