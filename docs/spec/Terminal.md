# Terminal

The terminal panel shows the hc terminal of the selected tab; it fills the space between the sidebar and the files panel.

Design system page: component Terminal. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Panel | Panel with padding 0 |
| Text | mono `terminal` (13/20), `terminal-text`; dim lines `ink-42`; padding `18px 26px 10px`; text sits at the bottom |
| Overlays | QuestionsNotch at the top center, Outline at the right edge (orchestrator only) |

## States

- **Focused:** the cursor blinks; keys go to the terminal.
- **Not focused:** the cursor is a hollow box.
- **Process ended:** a last line in `ink-38`; the tab closes on stop.

## Behavior

- Shows the hc terminal of the selected tab (D80). contr watches until the
  owner types; the first key sizes the terminal (D91).
- The background is clear over `panel`; the text color is
  `terminal-text`; the ANSI palette is the theme's (README Themes). The
  Light palette is a known light palette tuned for contrast on the light
  panel (D129).
- Overlays: QuestionsNotch in every terminal of a project with an open
  question, to-do or blocked worker menu (D124); Outline in the orchestrator terminal only.

## Data

- `internal/app/ghostty` `Conn` over hc.

## Keys and focus

- Keys go to the terminal while it has the focus. Esc is never taken by contr.
- `⌘F` opens MyGo's `FindBar` over the terminal text (D130). Esc in the
  `FindBar` closes it and returns the focus to the terminal. There is no split.

## Edge cases

- No selection: "select an agent to see its terminal" in `ink-42`.
- The process ends: a last line in `ink-38`.
- Theme change: the palette changes without a reconnect.

## Accessibility

- Label: "Terminal, <agent>".

## Tokens

`terminal`, `terminal-text`, `ink-42`, `space-18`, `space-26`, `space-10`, `panel`.

## Build in MyGo

- MyGo's terminal plugin (`docs/plugins/terminal.md`) with `internal/app/ghostty`'s `Conn` as the connection to the hc terminal.
- Set the terminal colors from the tokens: background transparent over `panel`, foreground `terminal-text`.
- Put the notch and the outline in an overlay layer above the terminal; they must not take keys from it unless focused.

## Do and do not

**Do**

- Keep the terminal background transparent so the glass shows.

**Don't**

- Do not draw a border around the prompt in the app: the shell draws it.

## Tests

- View: palette per theme; overlays do not take keys.
- View: `⌘F` shows the `FindBar` over the terminal; Esc closes it.
- e2e: type in a scratch core terminal and read the text back.

## Replaces

- The v0.2 terminal card and its 34 header (not built).
