# StatusGlyph

A one-character mark that tells the state of an agent: a braille spinner for workers and terminals, and the Claude glyph for the orchestrator.

Design system page: component StatusGlyph. Shared rules: [README.md](README.md).

## Anatomy

- Braille glyph: mono `glyph` style (17/18), 16px wide; 14px inside tabs and the notch.
- Claude glyph: 16px stroke icon in `claude`.

## States

| State | Braille | Claude glyph | Means |
|---|---|---|---|
| working | ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ every 90ms (cycle .9s), `ink-100` | turns once in 2.4s | the agent runs |
| working, slow | same, cycle 1.4s | — | no output for a while |
| waiting | ⠿ fades .25↔1 in 1.6s | — | waits for CI or another agent |
| idle | ⠶ still, `ink-30` | still, opacity .45 | nothing to do |
| needs you | spinner in `blocked` | stroke `blocked` | asks the owner a question or is stuck at a prompt |

With reduce motion on, every glyph is still: working shows ⠋, waiting shows ⠿.

## Behavior

- One glyph per agent. The state comes from the agent state (D35): working,
  working slow (no output for a while), waiting, idle, needs you.
- Needs you wins over every other state.

## Data

- `internal/agents` watch events; the view only maps state to glyph.

## Keys and focus

- None.

## Edge cases

- Off screen: the timer stops.
- Reduce motion: still glyphs (⠋ working, ⠿ waiting).

## Accessibility

- Label: "working", "waiting", "idle", "needs you".

## Tokens

`glyph` type style, `ink-100`, `ink-30`, `blocked`, `claude`.

## Build in MyGo

- A small custom widget (see `ui/custom-widgets.md`) that draws one `Text` glyph. A timer changes the frame only while the state is working or waiting and the glyph is on screen; idle and needs-you states use no timer, so the app uses no CPU while idle.
- Read the reduce-motion setting from MyGo and stop the timer when it is on.
- Claude glyph: `ui.Icon` with the official Claude mark (open point O8); turn it with `.Transition(...)` or `AnimatePose`.
- Give the glyph a screen-reader label: "working", "waiting", "idle", "needs you".

## Do and do not

**Do**

- Use red only for "needs you".
- Keep one glyph per row, tab or chip.

**Don't**

- Do not use a spinner from MyGo here: the braille glyph is part of contr's look.
- Do not use amber or green for states in the chrome.

## Tests

- Unit: state to glyph and frame list.
- View: no timer runs for idle and needs you; reduce motion stops it.

## Replaces

- The status icons in `assets/icons` used by rows and tabs (v0.2 Status icons).
