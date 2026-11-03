# contr design (v0.1)

Source of truth: the "herdr sidebar mockup" design canvas, artboards **G · contr window** and **G · contr status icons** (owner's claude.ai artifact). This file is the text version builders work from.

## Window (1440×900 reference)

```
┌ sidebar 280 ─┬ title bar 40: agent tabs ················ [Review] [panel] ┐
│ ● ● ●  [◧] contr                                                          │
│ Spaces  + new│ terminal (selected agent)            │ board panel 440       │
│ [Local card] │                                      │  Work  plan build …   │
│ client main  │                                      │  rows                 │
│  ◐ sort-bags │                                      │  Needs you N          │
│  ! bag-drag  │                                      │  rows                 │
│ server main  │                                      │                       │
│ linux-pc     │ [ needs-you row ]                    │                       │
│ Orchestrators│                                      │  [answer card 168]    │
│  ○ lead      │                                      │                       │
│ Agents N run │                                      │                       │
│  ◐ b162      │                                      │                       │
├──────────────┴ status bar 26: claude ▬ 38% · codex ▬ 12% · CPU MEM                     ┤
```

- **Title bar tabs (D50):** one tab for each hc terminal of the open project: its orchestrators first (`◆ name`, terminal background), then its workers (`<issue> · <worktree>`, or the name when it has no issue). Each tab has the status icon. The first orchestrator tab has a white badge with the number of open needs-you items (blocked workers, questions, to-dos); it is hidden at 0. The open project is the project of the selected orchestrator or worker, and stays as it was for any other selection. A click on a tab selects that agent; the selected tab has the terminal background and a white underline. There are no "+" or split buttons until hc has actions for them.
- The title bar has a "Review" button left of the panel toggle. It swaps the terminal area for the Review view (see Review below) and is white while the view shows (D37).
- Sidebar spans the full height, title-bar area included. Panel toggles from the title bar; hidden, the terminal takes the width.
- v0.1 status bar usage shows only claude and codex. There is no OpenCode Go segment.
- The agents area has an Orchestrators section on top, whatever the state of its agents, then Agents N running. An agent row has the status icon, the name, and a second line `<command> · <age>` (age since the last output: `5s`, `3m`, `2h`; it moves on a state change or a repaint, with no timer). The Orchestrators section is hidden when there is none.
- Selecting a worktree or agent in the sidebar highlights it as a rounded card and shows that agent's hc terminal in the terminal area (D45). The terminal watches at the size the terminal already has and takes the size when you type.

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
| diff.added | #A6D189 | added lines and + counts in code views (D34) |
| diff.deleted | #F07178 | deleted lines and − counts in code views (D34) |

No hue anywhere, except the two diff colors in code views and +/− counts: white means the owner acts, greys mean everything else.
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

- **Work rows:** `#640` label, title, second line saying who/what holds it (`b-1151 building`, `CI on linux-pc`, `your test` in the "you" style), then four 44px stage columns headed `plan build review you`. The stage comes from the role (D46): planner is plan, builder is build, reviewer is review; earlier stages show done and later ones not started. The you column lights when the worker is blocked or an open question or to-do names it. A click on a row selects the worker. Rows are the workers of the selected orchestrator's project (all projects when none is selected).
- **Needs-you row:** one line under the terminal. Tabs: blocked workers first, then questions (`D7`), then to-dos (`T12`; kind `know` shows the fyi icon). It shows the selected orchestrator's project, or all projects when none is selected. Hidden when empty. Resolved items strike through until "clear done". A click picks a tab and opens the answer card. There is no pin bar.
- **Needs you (panel list):** the same tabs as the row, in the same order, one line each: icon, id (`D7`, `T12`), title, kind (`decide`, `do`, `fyi`, `done`). Rows never move on their own; resolved ones strike through until "clear done".
- **Answer card:** floats over the bottom of the terminal area, above the needs-you row; fixed 168px. A question shows its options, the recommended one first as the filled button, and a field for a free answer. Enter sends the typed text, else the recommended option; `1`–`4` send an option in the card's order; Escape or Close closes it. The answer goes to the coordinator that asked as `Owner answered <ID>: <answer>`. If the send fails, the answer stays saved and the card shows the failure. A to-do shows Done. A blocked worker shows its menu: the card reads the last 40 lines of the worker's terminal when it opens and when the worker's turn or message changes, and shows the question with one button per choice (the first filled), then Focus worker and Close. A click or the digit `1`–`9` sends that choice as raw keys with no Enter, after a second read shows the same menu; if the menu changed, nothing is sent and the card says `The prompt changed; look again.` Enter does nothing. A choice that asks for typed text, an auth prompt, an unknown program or an unreadable screen shows only the worker's message and Focus worker. The card closes when the worker is no longer blocked. A resolved item shows no options.
- **Panel follows the selected tab:** an orchestrator (or nothing) selected shows the board; a worker selected shows that worker: name, role and issue, worktree, status, the last 40 lines of its terminal in a code view, and Interrupt and Stop. Stop needs a second click.
- **No Wrap up or Keep going button** (owner, 2026-11-03): the owner types "wrap up" in the orchestrator terminal. The Report tab is gone.

## Review

Shown in the terminal area while the title bar's "Review" button is on. The repo is the selected worktree's root, or the folder of the selected agent. With no selection it shows "Select a project".

- Three columns: branches (260), files or commits (260), code (grows). Branches are all local branches ahead of `main`.
- Labels: `branch <name>`, `file <path>`, `commit <short>`; the switch `review mode` has `Whole branch` and `By commit`.
- Rows and the code view come from `internal/app/kit`; the only colors are the two diff colors (D34).
- No git runs while the view or the window is hidden.
