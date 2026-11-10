# ChatView

The Chat mode of the terminal panel for a Claude Code session (D246). It
shows the session from its transcript (D242), read only, in the place of
the terminal grid. The overlays (notch, pill, outline) stay above it.

Design system page: none yet; the mockups C1 (base), C2a (lines of an open
group) and C2b (the folded group) are the design intent.
Shared rules: [README.md](README.md).

## Switch

| Part | Value |
|---|---|
| Place | alone at the top right of the terminal panel, a layer of the terminal overlay under the notch: top `space-8`, right 10px (the outline inset); right 40px while the outline column's top is above the switch's bottom (34px), clear of the column (`Outline.Top`, the D237 rule, D253). The [OrchestratePill](OrchestratePill.md) is in the title bar. |
| Control | `kit.Switch` (`SegmentedBase`): "Chat", "Terminal"; label "View of this session" |
| Shows for | a Claude Code session: an orchestrator (with a plain terminal that runs Claude Code, D190), or a terminal whose transcript path a kit hook saved (a worker, D243). A plain terminal has no switch. |
| Mode | kept for each terminal for the run of the app; Terminal is the default |
| Key | `⌘E` toggles the selected session; key chip `E` centered on the switch while ⌘ is held ([KeyHints.md](KeyHints.md)). Back in Terminal, the grid takes the focus. |

## Regions

| Region | Value |
|---|---|
| Transcript | `ui.List` with `FollowEnd`: it follows the end while the owner is at the end. Rows `space-18` apart, padding 44px above (the switch row), `space-20` at the sides and below; rows that do not fill it sit at the bottom. |
| Column | the rows' content, at most 760px wide, centered |
| Dock | one line, 44px high, centered: "Type in the terminal" (`body`, `ink-42`) and the outlined card button "Switch to Terminal". The composer comes in a later slice. |
| Empty | "No transcript yet" (`body`, `ink-42`), centered, while the session has no path, no file or no row yet. The switch still works. |

## Rows

| Row | Value |
|---|---|
| Owner prompt | right-aligned, at most 560px, padding `8px 13px`, radius `radius-12`, fill `hover`, text `prose` in `ink-100` |
| Claude's text | `markdown.View`: `prose` size and line, sans; text `ink-85`, strong and links `ink-95`, muted `ink-45`; code mono `ink-90` on `hover`; code blocks with Copy on `hover` with a `hairline` line; tables with `hairline` lines and a `hover` header |
| Work group, folded | the tool calls and kit notes between two texts, as one row 20px high, mono `value`, `ink-42` (`ink-62` on hover): "3 worker notes · 6 tool calls" (a part with no item is left out, "1 tool call"), a 6px dot for each worker that sent a note, then `›`. While a call has no result, a working StatusGlyph (14px) comes first. |
| Work group, open | `⌄`; under it a card, at most 584px, `space-6` below the row, padding `8px 12px`, radius `radius-9`, line `hairline`: the note lines, a `hairline` rule, the call lines |
| Note line | 20px, gap `space-8`, mono `meta`: the sender's dot, the name (500, `ink-85`), the text on one line (`ink-42`). An owner answer or done mark has the name "owner". |
| Call line | 20px, gap `space-8`, mono `meta`: a working StatusGlyph while it runs, the name (`ink-62`, at least 48px; an MCP tool shows its server, "contr"), the summary on one line (`ink-42`), "error" in `blocked` when the result is an error, the duration (`ink-38`): "0.4s", "12s", "2m 5s", from the timestamps of the call and its result, none when one is missing |
| Note for the owner | outside the fold, as a note line with a `blocked` dot, the name in `ink-100` and the text in `ink-85`: an hc message of a worker that "ended its turn without a report", or that says it is blocked ("NAME: blocked, …", "blocked …") |
| Dot | 6px, round: `blocked` for a blocked worker, `ink-85` for a working one, else `ink-40` |

Hidden entries do not show: meta lines, system reminders, hook output (an
attachment, or a user line in a `<…-hook>` tag), the kit's playbook, local
command output, compact summaries, interrupt marks, system lines, thinking
marks and the lines of subagents.

## Behavior

- The view reads the transcript only while it shows: a read runs off the
  UI thread and its entries are applied on it. A change of the file
  (chatsource, D243) reads what was added; a moved path or a shorter file
  reads again from 0. Nothing polls, and no timer runs (D55). While the
  chat does not show, a change of the file asks for no frame (D257).
- A click on a group row opens or closes it; the list holds its place for
  that frame.
