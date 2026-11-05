# Panel

A panel is a rounded area inside the window glass: the sidebar, the terminal and the files panel are panels.

Design system page: component Panel. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Box | fill `panel`, line `panel-line`, radius `radius-12`, padding `12px 10px 10px` |
| Head | row: name in `label` style (12px/500), `ink-45`, fills the row; icon buttons at the right; padding `2px 4px 4px 10px` |
| Footer | line `hairline` on top; 24px icon buttons at the right |

## States

- **Default.**
- **Focused panel:** no change. Focus shows on the focused row or terminal, not on the panel.

## Behavior

- A panel only draws its box, head and footer. Its content scrolls inside
  it (`ui.Scroll`); the head and footer stay.

## Data

- None.

## Keys and focus

- None of its own.

## Edge cases

- A head name too long for the width: ellipsis.

## Accessibility

- The panel is a group named by its head text.

## Tokens

`panel`, `panel-line`, `hairline`, `radius-12`, `space-12`, `space-10`, `label`, `ink-45`.

## Build in MyGo

- A plain container view with `Background`, `Border` and `CornerRadius`; there is no MyGo widget to wrap.
- The panel head: a row with `Text(...).SingleLine()` (it cuts with an ellipsis; `NoWrap` keeps the line whole) and IconButtons. The footer line is a 1px `hairline` box above the footer row.
- Put the panels in `ui.Split` (see Window).

## Do and do not

**Do**

- Keep head names short and lower-case where they are names (project name "contr").

**Don't**

- Do not put a panel inside a panel.
- Do not add shadows to panels.

## Tests

- View: head text and icon buttons render; a long name clips.

## Replaces

- The v0.2 card style of the terminal and the panel (`bg.card`, `line`).
