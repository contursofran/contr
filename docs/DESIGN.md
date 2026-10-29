# contr design (v0.1)

Source of truth: the "herdr sidebar mockup" design canvas, artboards **G · contr window** and **G · contr status icons** (owner's claude.ai artifact). This file is the text version builders work from.

## Window (1440×900 reference)

```
┌ sidebar 280 ─┬ title bar 40: agent tabs ··················· [split] [panel] ┐
│ ● ● ●  [◧] contr                                                          │
│ Spaces  + new│ terminal (selected agent)            │ board panel 440       │
│ [Local card] │                                      │  Board | Report  wrap │
│ client main  │                                      │  summary line         │
│  ◐ sort-bags │                                      │  Work  plan build …   │
│  ! bag-drag  │                                      │  rows                 │
│ server main  │                                      │  Needs you N          │
│ linux-pc     │ [ prompt ]                           │  rows                 │
│ Agents N run │                                      │  [answer card 168]    │
│  ○ principal │                                      │                       │
├──────────────┴ status bar 26: claude ▬ 38% · codex ▬ 12% · CPU MEM                     ┤
```

- Sidebar spans the full height, title-bar area included. Panel toggles from the title bar; hidden, the terminal takes the width.
- v0.1 status bar usage shows only claude and codex. There is no OpenCode Go segment.
- Selecting a worktree or agent in the sidebar highlights it as a rounded card and focuses that agent's tab; the terminal shows only that agent.

## Tokens

| Token | Value | Use |
|---|---|---|
| font | JetBrains Mono 12/18 (terminal 13/20) | everything |
| bg.window | #141414 | panel, window |
| bg.sidebar / bars | #101010 | sidebar, title bar, status bar |
| bg.terminal | #121212 | terminal |
| bg.card | #1C1C1C + 1px #2C2C2C inset, radius 7 | selected row |
| bg.answer | #1A1A1A, border #262626, radius 10 | answer card |
| line | #222222 | dividers |
| text | #EDEDED | primary |
| text.2 | #BDBDBD | secondary |
| text.dim | #7A7A7A / #6A6A6A | labels, meta |
| you | #EDEDED bg + #111111 bold text | anything waiting on the owner (badges, "your test") |

No hue anywhere: white means the owner acts, greys mean everything else.
The terminal shows program colours as they are; greyscale applies to contr's own UI only.

## Status icons (16px viewBox, drawn at 12–14px)

| State | Icon | Where |
|---|---|---|
| needs you | white filled circle, dark `!` | worktree waits on the owner |
| decide | white filled circle, dark `?` | decision (D…) |
| do | rounded square outline with → | to-do (T…) |
| blocked | white filled octagon, dark bar | stuck at a prompt |
| working | grey ring with a white quarter arc, spins 1.4 s | running |
| waiting | grey clock | CI or another agent |
| idle | hollow grey circle | nothing to do |
| done | grey check | stage passed |
| fyi | grey circled `i` | news |
| not started | faint dot | stage ahead |

One icon set for sidebar, tabs, board stages and needs-you rows. Each has an accessible label.

## Board panel

- **Work rows:** `#640` label, title, second line saying who/what holds it (`b-1151 building`, `CI on linux-pc`, `your test` in the "you" style), then five 44px stage columns headed `plan build review CI you`.
- **Needs you:** icon, id (`D7`, `T12`), title, kind (`decide`, `do`, `fyi`, `done`). Rows never move on their own; resolved ones strike through until "clear done".
- **Answer card:** fixed 168px. A decision shows its options, recommended first as the filled button; a to-do shows Done plus its link; resolved shows the answer, undo and "Next: D8". A selected work row shows its state with Focus tab, Report, Open PR.
- **Report tab:** the selected row's RESULT.md.
- **Wrap up / keep going** in the panel header.
