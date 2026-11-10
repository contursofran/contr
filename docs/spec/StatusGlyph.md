# StatusGlyph

A small mark that tells the state of an agent: a spinner of six drawn dots for workers and terminals, and the Claude glyph (the Claude Code mascot) for Claude sessions.

Design system page: component StatusGlyph. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![StatusGlyph, dark](images/StatusGlyph-dark.png) | ![StatusGlyph, light](images/StatusGlyph-light.png) |

## Anatomy

- Dot glyph (D251): six drawn dots in a 2×3 grid, in a 16px box; 14px inside tabs, the notch and chat rows. In the 16px box each dot is a 3px circle; the centres are at x 5.5 and 10.5 and y 3, 8 and 13, so the grid is centred. The 14px box scales the same drawing (dots of 2.625px). Each frame lights some dots: a lit dot takes the ink of the state, a dim dot `ink-30`. The frames are the braille patterns below: dots 1 to 3 are the left column from the top, dots 4 to 6 the right column.
- Claude glyph: the Claude Code mascot (D218), the pixel creature of Claude Code, in `claude`. A Claude session that does not orchestrate draws it in `ink-45`; needs you stays `blocked` (D191). It is one filled shape of 16×10 cells: the body, two eye holes, the arms and four legs (`assets/icons/claude-mascot.svg`, one fill; NOTICE credits the source). It is as wide as the glyph box and centred in it: a cell is 1px in the 16px box, so the edges are sharp, and .875px in the 14px box.

## States

| State | Dots (as braille) | Claude glyph | Means |
|---|---|---|---|
| working | ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ every 90ms (cycle .9s), `ink-100` | hops 1.5px up and back in .8s | the agent runs |
| working, slow | same, cycle 1.4s | still | no output for a while |
| waiting | ⠿ fades .25↔1 in 1.6s | still | waits for CI or another agent |
| idle | ⠶ still, `ink-30` (so all six dots are `ink-30`) | still, opacity .45 | nothing to do |
| needs you | lit dots in `blocked`, still on ⠋ | still, in `blocked` | asks the owner a question or is stuck at a prompt |

With reduce motion on, every glyph is still: working shows ⠋, waiting shows ⠿, and the mascot does not hop.

## Behavior

- One glyph per agent. The state comes from the agent state (D35): working,
  working slow (no output for a while), waiting, idle, needs you.
- Needs you wins over every other state.
- The Claude glyph has no motion for working slow and waiting: it is still at full opacity.
- Small (tabs and the notch) is 14px; rows are 16px.

## Data

- `internal/agents` watch events; the view only maps state to glyph.

## Keys and focus

- None.

## Edge cases

- Off screen: the timer stops.
- Reduce motion: still glyphs (⠋ working, ⠿ waiting, the mascot on the ground).

## Accessibility

- Label: "working", "waiting", "idle", "needs you".

## Tokens

`ink-100`, `ink-30` (idle, and the dim dots), `blocked`, `claude`.

## Build in MyGo

- A small custom widget (see `ui/custom-widgets.md`; `kit.StatusGlyph`) that draws six dots with `Painter.Fill` (a 3px rect with a 1.5px radius is a circle), not a font glyph, so the dots have the same size in each font stack (D251). A timer changes the frame only while the state is working or waiting and the glyph is on screen; idle and needs-you states use no timer, so the app uses no CPU while idle.
- Read the reduce-motion setting from MyGo and stop the timer when it is on.
- Claude glyph: the Claude Code mascot (D218, `assets/icons/claude-mascot.svg`), drawn with `Painter.Icon`. The hop moves the rect of the icon by whole device pixels, so its mask is rasterized once per size and a frame only places it again. `.Transition(...)` and `AnimatePose` go to a target and stop, and the hop repeats, so the mascot paints itself from `Draw`, as the working icon of `internal/app/icons` does. Like the dot glyph, it asks for repaints only while it works, so it stops off screen and with Reduce Motion.
- A test checks the SVG against the credited path, so the art does not drift.
- Dot frames repaint at the step boundary (`Painter.After`), a fade and the hop at 30 frames a second.
- Give the glyph a screen-reader label: "working", "waiting", "idle", "needs you".

## Do and do not

**Do**

- Use red only for "needs you".
- Keep one glyph per row, tab or chip.

**Don't**

- Do not use a spinner from MyGo here: the dot glyph is part of contr's look.
- Do not use amber or green for states in the chrome.

## Tests

- Unit: state to glyph and frame list; frame to lit dots; the dot grid at 16px and 14px; lit dots in the ink and dim dots in `ink-30`; the SVG is the credited path; hop offset over time.
- View: no timer runs for idle and needs you; reduce motion stops it.

## Replaces

- The status icons in `assets/icons` used by rows and tabs (v0.2 Status icons).
