# OrchestratePill

A small glass pill at the top right of the terminal of a Claude session. It
says that the session orchestrates and how many live workers it has, or it
is a button that starts orchestration (D234).

Design system page: none yet; the look copies [QuestionsNotch.md](QuestionsNotch.md).
Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Place | in the terminal overlay, top `space-8`, right 10px (the outline inset); right 40px only while the outline column reaches the pill's rows (below, D237) |
| Box | height 26px, padding `0 12px 0 10px`, gap `space-8`, radius `radius-14` (fully round) |
| Glass | `glass-float`, blur `glass-blur`, line `float-line` (1px all round), shadow `shadow-notch` |
| Glyph | StatusGlyph, Claude mascot, 14px, still: `claude` while it orchestrates, `ink-45` on the button (D191) |
| Label | `label` (12/18/500), `ink-85`: "Orchestrating", "Orchestrating · 1 worker", "Orchestrating · 3 workers", or "Orchestrate" |

Popover (button state only), below the pill, its right edge on the pill's right edge, 6px apart:

| Part | Value |
|---|---|
| Panel | width 320px, padding `space-12`, gap `space-8`, radius `radius-12`, fill `float-solid` (D217), line `float-line`, shadow `shadow-float` |
| Field | `TextInputBase`, label and placeholder "Goal (optional)", height 30, padding `0 12px`, radius `radius-8`, line `input-line`, text `ink-90`, placeholder `ink-30` |
| Command | mono `meta`, `ink-42`: the text that goes, "/contr:orchestrate" and the goal |
| Buttons | right-aligned, gap `space-8`: "Cancel" (outlined card button), "Orchestrate" (filled card button) |

## States

- **Hidden:** no terminal is selected; the selected terminal is a worker
  (builder, reviewer, …) or a plain shell; the session's program ended (not
  `Live`); or the agent state is not there.
- **Orchestrating:** the selected terminal is a Claude session (a row of
  `Snapshot.Orchestrators`) and `Snapshot.Orchestrating` is true (D191): it
  ran `/contr:orchestrate` or has live workers. The label counts its live
  workers (`LiveWorkersOf`); with none yet it is "Orchestrating". No click.
- **Button:** the selected terminal is a Claude session that does not
  orchestrate: an orchestrator-role session before `/contr:orchestrate`, a
  Claude session in a plain terminal (D190) or one contr did not start. The
  pill is a button "Orchestrate". Hover, and while the popover is open: label `ink-100`.
- **Open:** the button with its popover.
- **Sending:** after Orchestrate, until the send returns: the buttons take
  no click. An error shows as a toast, and the popover stays open with the goal.

## Behavior

- A click on the button opens the popover, with the focus in the goal field.
- ⏎ in the field or a click on "Orchestrate" sends `/contr:orchestrate <goal>`
  and Enter to the selected session's terminal, through the agent source
  (`AgentSource.Send`, the same terminal input path that the questions card
  uses). The goal is trimmed. An empty goal sends `/contr:orchestrate` alone.
  The send runs off the UI thread. When it returns, the popover closes and
  the goal clears.
- "Cancel", Esc and a click outside close the popover. The goal stays for
  the next open.
- The text goes only to the session that was selected when the owner
  pressed Orchestrate; a selection change while the send runs does not move it.
- The pill changes to Orchestrating when the agent state says so: after the
  session's first turn (the kit's Stop hook label) or its first worker.
  The pill does not guess it from the send.

## Placement next to the notch

- The notch hangs at the top center. While it shows, the pill keeps
  a reserve of 240px at the center, plus 8px on each side, free.
- When the pill does not fit at the right of that reserve, it moves down,
  under the notch: top `notch-height` + `space-8` (38px). It keeps its right
  space (10px or 40px, below).

## Placement next to the outline

- The outline column ([Outline.md](Outline.md)) is inset 10px and 20px wide,
  centered on the terminal's height. The pill sits at right 10px.
- When the column's top (its count, or its first tick's hit area) is above
  the bottom of the pill's rows, the pill moves to right 40px: clear of the
  column by 10px. The rows end at 34px (top 8, height 26), or at 64px while
  the notch shows, because the pill can wrap under it.
- The top is the column's real top, from the outline's last drawn layout
  (`Outline.Top`), not a guess from the tick count (D237).
- The open questions card is a popover and draws over the pill.

## Data

- `agentstate.Snapshot` (Orchestrators, Workers, `Orchestrating`,
  `LiveWorkersOf`), read only; the selected terminal from the selection.
- No timer; the pill changes on an agent state event or a click.

## Keys and focus

- ⇥ reaches the button; Space and ⏎ open the popover.
- In the popover: ⏎ sends, Esc closes. Esc closes only the popover; it does
  not reach the terminal (D72).
- After the popover closes, the focus goes back to where it was.

## Edge cases

- Many workers: the count is the number ("Orchestrating · 12 workers").
- Ended workers do not count; only `Live` workers whose parent is the session.
- The session ends or the selection moves while the popover is open: the
  pill hides and the popover closes with it.
- The send fails (hc offline, terminal gone): a toast with the error.

## Accessibility

- Orchestrating: a text element labeled with the pill text.
- Button: label "Orchestrate", role button, has popup.
- Popover: the field label is "Goal (optional)".

## Tokens

`glass-float`, `float-line`, `float-solid`, `shadow-notch`, `shadow-float`,
`radius-14`, `radius-12`, `radius-8`, `input-line`, `ink-100`, `ink-95`,
`ink-90`, `ink-85`, `ink-45`, `ink-42`, `ink-30`, `claude`, `label`, `meta`,
`notch-height`.

## Build in MyGo

- Package `internal/app/orchestrate`, a layer of the terminal overlay, added
  before the notch so the notch draws over it.
- `ButtonBase` for the button, `PopoverBase` for the panel, `TextInputBase`
  for the field, `kit.CardButton` for Cancel and Orchestrate, `kit.GlassFloat`
  for the glass, `kit.StatusGlyph` for the mascot.
- The move under the notch is a wrapping row: a spacer that ends at the
  right of the notch reserve, then the pill, which wraps to a second line
  when it does not fit. No layout read of an earlier frame.
- The right space reads `Outline.Top` (D237): the outline keeps its column's
  top from a `Draw` callback and asks for one more frame when it changes.

## Do and do not

**Do**

- Count in words: "1 worker", "3 workers".

**Don't**

- Do not show the pill on worker or plain shell terminals.
- Do not cover the outline ticks or the notch.
- Do not send to any terminal but the selected session's.

## Tests

- View: Orchestrating with 0, 1 and 3 live workers; an ended worker does not count.
- View: the button for a Claude session that does not orchestrate.
- View: no pill on a worker terminal, a plain terminal, or with no selection.
- View: ⏎ sends `/contr:orchestrate <goal>` to the selected address; an empty
  goal sends `/contr:orchestrate`; Cancel and Esc close and send nothing; a
  send error shows a toast.
- View: the pill sits at right 10px with no column or a short one, and at
  right 40px when the column reaches its rows; it does not overlap the
  column or the notch, in a wide and a narrow terminal.
- View: the popover's right edge is on the pill's right edge, at 10px and 40px.
- View: `Outline.Top` is the column's top; the outline asks for a frame when it moves.
- Sheet: `scripts/sheet.sh OrchestratePill`. See: fixtures `orchestrate` and
  `orchestrating`.

## Replaces

- Nothing: before, only the tab color showed orchestration (D191), and
  nothing in the window started it.
