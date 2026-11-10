# OrchestratePill

A small pill in the title bar, for the selected Claude session. It says that
the session orchestrates and how many live workers it has, or it is a button
that starts orchestration (D234, D253).

Design system page: none yet; the look copies the [UpdatePill](UpdatePill.md)
and the title bar's icon buttons: no fill (D253).
Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Place | in the [TitleBar](TitleBar.md), after the flexible space and the UpdatePill, `space-8` left of the DiffSummary (or of Search when no diff shows); centered on the bar's height (D253) |
| Box | height `icon-button` (28px), padding `0 10px` (`0 6px` compact), gap `space-6`, radius 14px (fully round) |
| Look | no fill, no line and no shadow at rest (D258), in both states. Button hover, and while the popover is open: fill `hover` |
| Glyph | StatusGlyph, Claude mascot, 14px, still: `claude` while it orchestrates, `ink-45` on the button (D191) |
| Label | `label` (12/18/500), `ink-85`: "Orchestrating", "Orchestrating · 1 worker", "Orchestrating · 3 workers", or "Orchestrate" |
| Compact | in a window under `FilesHideBelow` (1100px, the title bar's narrow rule): the mascot and the worker count (`count`, `ink-85`; none while it is 0), or the mascot alone on the button. The full text is the tooltip. |

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
  pill is a button "Orchestrate". Hover, and while the popover is open: fill `hover`, label `ink-100`.
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

## Placement in the title bar

- The pill is a button or a text, not a drag area: the bar's flexible space
  still drags the window and a double-click on it zooms (D253).
- In a narrow window the pill is compact, so the tabs keep their room.
- The popover is a popover of the window: it draws over the terminal and the
  notch.

## Data

- `agentstate.Snapshot` (Orchestrators, Workers, `Orchestrating`,
  `LiveWorkersOf`), read only; the selected terminal from the selection.
- No timer; the pill changes on an agent state event or a click.
- The title bar gives `compact` from the window width; the pill reads no layout.

## Keys and focus

- ⇥ reaches the button; Space and ⏎ open the popover. Focus ring: 1px
  `focus-ring`, 2px outside the box.
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

- Orchestrating: a text element labeled with the pill text, also when compact.
- Button: label "Orchestrate", role button, has popup.
- Popover: the field label is "Goal (optional)".

## Tokens

`icon-button`, `float-line`, `hover`, `float-solid`, `shadow-float`,
`radius-12`, `radius-8`, `input-line`, `focus-ring`, `ink-100`, `ink-95`,
`ink-90`, `ink-85`, `ink-45`, `ink-42`, `ink-30`, `claude`, `label`, `count`,
`meta`.

## Build in MyGo

- Package `internal/app/orchestrate`; `TitleBar.Orchestrate` builds it with
  `compact` from `TitleBar.Narrow`.
- `ButtonBase` for the button, `PopoverBase` for the panel, `TextInputBase`
  for the field, `kit.CardButton` for Cancel and Orchestrate,
  `kit.StatusGlyph` for the mascot, `.Tooltip(...)` for the compact text.

## Do and do not

**Do**

- Count in words: "1 worker", "3 workers".

**Don't**

- Do not show the pill on worker or plain shell terminals.
- Do not give the pill a fill, a line or a shadow at rest: the title bar stays plain (D258).
- Do not send to any terminal but the selected session's.

## Tests

- View: Orchestrating with 0, 1 and 3 live workers; an ended worker does not count.
- View: the button for a Claude session that does not orchestrate.
- View: no pill on a worker terminal, a plain terminal, or with no selection.
- View: ⏎ sends `/contr:orchestrate <goal>` to the selected address; an empty
  goal sends `/contr:orchestrate`; Cancel and Esc close and send nothing; a
  send error shows a toast.
- View: the pill sits `space-8` left of the next item of the title bar, 28px
  high, centered; the frame puts it left of Search, in the bar.
- View: compact shows the mascot and the count, with the full text as the
  tooltip; the button shows the mascot alone; a narrow window makes it compact.
- View: the popover's right edge is on the pill's right edge, 6px under it,
  full and compact.
- Sheet: `scripts/sheet.sh OrchestratePill`. See: fixtures `orchestrate` and
  `orchestrating`.

## Replaces

- The pill over the terminal's top right (D234, D237), with its 240px notch
  reserve: replaced by the title bar place (D253).
- Before that, nothing: before, only the tab color showed orchestration (D191), and
  nothing in the window started it.
