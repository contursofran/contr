# Outline

A column of short ticks on the right edge of the orchestrator terminal, centered on the height; each tick is one message from the orchestrator, and new ones are `ink-100` with their count on top. In the Chat mode of a session, the ticks are the important things of the chat (D270, [ChatView.md](ChatView.md#outline)).

Design system page: component Outline. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![Outline, dark](images/Outline-dark.png) | ![Outline, light](images/Outline-light.png) |

## Anatomy

| Part | Value |
|---|---|
| Column | right edge of the terminal, inset 10px, centered on the height; ticks right-aligned, gap 9px |
| Tick | height `tick-height` (2px), width 8–14px by message length, `tick`, radius `radius-1` |
| New tick | `ink-100` |
| Blocked tick | `blocked`, new or not: only a tick that waits for the owner (Chat mode: an open question, a decision remark with no prompt of the owner after it; D270) |
| Count | mono `count` (11/14/500), `ink-100`, 3px above the ticks; only when there are new ticks |

## States

- **Nothing new:** all ticks `tick`; no count.
- **New messages:** the ticks since the owner last looked at this terminal are `ink-100`; the count shows how many.
- **Hover:** the tick grows to 20px and the OutlineCard shows at its left.
- **Click:** the terminal scrolls to the message; that tick and those before it become read.

## Behavior

- Terminal mode: one tick per orchestrator message pinned for the owner (`pin_for_owner`).
- Chat mode (D270): one tick per important row of the chat (`chatview.Marks`), in the room of the transcript list, in place of the pins. The seen state is the terminal's, so "new" follows the same rule.
- "New" means after the last time this terminal had the focus. The terminal
  has the focus while it is selected and the window has the focus. A message
  that comes while the owner looks is not new. A message that came while away
  stays new until the owner leaves again or clicks it. A terminal never seen
  starts with what is pinned when contr starts as old.
- Click jumps to the message. hc has no scroll-to (DESIGN.md data table):
  in Terminal mode, click opens the OutlineCard and marks the tick read. In
  Chat mode, click also scrolls the chat to the row.

## Data

- Pins from the owner store; focus times kept in memory per terminal.
- Chat mode: the chat model's marks; a pin or a question takes its text from the owner store.

## Keys and focus

- ⇥ reaches the ticks; ↑ ↓ move between them; ⏎ opens.

## Edge cases

- More ticks than fit (D270): the column keeps inside its height (the terminal's, or the chat's transcript list). The pitch gets smaller, from 11px down to 4px (a 2px tick and 2px); the hit area is as high as the pitch when that is less than 10px. When even 4px does not fit, the newest that fit show. The count's line takes 17px of the height while it shows. Terminal mode also keeps at most the newest 40 pins.
- No pins: no column.

## Accessibility

- Each tick is a button labeled with the headline and "new" when new; in Chat mode the kind comes first: "Decision: Ship the notch now?, new".

## Tokens

`tick`, `tick-height`, `radius-1`, `ink-100`, `blocked`, `count`.

## Build in MyGo

- A small custom widget: a column of `ButtonBase` ticks (each label: the headline). Hit area 20×10px for each tick, even if the tick is 2px high.
- Hover card: `TooltipBase` with placement left (see OutlineCard).
- Data: the orchestrator's pinned messages (`pin_for_owner`); "since you left" = after the last time the terminal had focus.
- `Outline.Top` gives the top of the column from the last drawn layout, for the Chat | Terminal switch (D237, D253). A change of the top asks for one more frame.
- Terminal mode: only the orchestrator terminal shows the outline. Chat mode: each session with a chat (`Outline.Chat`, D270).
- The column keeps its height from the last drawn layout for the fit; a change asks for one more frame. Nothing else redraws: no frame at rest (D55).

## Do and do not

**Do**

- Keep the ticks short and quiet: the count says that something is new.

**Don't**

- Do not let ticks go off the column: show the newest that fit (D270).
- Do not use red for new ticks.

## Tests

- View: ticks render; new count; focus marks them read.
- View: only the orchestrator terminal has the column in Terminal mode.
- View, Chat mode: the chat's ticks in place of the pins, in the list's room; blocked and new inks; click scrolls and marks read; many ticks fit; no frame at rest.

## Replaces

- The removed pin bar.
