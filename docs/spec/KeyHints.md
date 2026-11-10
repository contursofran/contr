# KeyHints

While ⌘ is held alone, each thing on screen that has a shortcut shows its key in a small key chip (#640, D245).

Design system page: none yet. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Chip | height 18px, least width 18px, padding 0×`space-4`, radius `radius-4` |
| Fill | `glass-float` with its blur (`kit.GlassFloat`), border 1px `float-line` |
| Text | type style `kbd`, `ink-100`: the key without ⌘ ("1", "T", "⇧E", "⌥↓") |

## Where the chips show

| Element | Chip | Place |
|---|---|---|
| Title bar tab | `1` to `8` for the first eight tabs; `9` on the last tab past them | over the left end of the tab, centered in height |
| New tab, Search, Hide files | `T`, `F`, `⇧E` | centered on the bottom right corner |
| Sidebar Settings | `,` | centered on the bottom right corner |
| Icon column: Open projects, New orchestrator | `O`, `N` | centered on the bottom right corner |
| Agent rows above and below the selected row | `⌥↑`, `⌥↓` | at the right end of the row, over the age |
| Questions notch | `J` | centered on the bottom right corner |

A disabled button shows no chip.

## Behavior

- The chips show when ⌘ is held alone for 400 ms (`shortcut.HintDelay`), in the active window.
- They hide when ⌘ is released, when another modifier or key is pressed, when a menu item runs
  its key, and when the window loses the keyboard. After another key they stay hidden until ⌘
  is released, so a short ⌘C or ⌘V shows nothing.
- The chips are drawn over the element: the layout does not move. They take no pointer.

## Data

- `c.Modifiers()` and `c.KeysDown()` of MyGo; each change draws a frame.

## Keys and focus

- The chips do not take the focus. The keys are in [README.md](README.md#keys).

## Edge cases

- More than nine tabs: tabs 9 and on show no chip, except the last one, which shows `9`.
- ⌘ held while the window is not active: no chips.

## Accessibility

- Screen readers do not read the chips (`AccessHidden`). Each element's label and tooltip name its key.

## Tokens

`glass-float`, `float-line`, `ink-100`, `kbd`, `radius-4`, `space-4`.

## Build in MyGo

- One part: `kit.KeyHint(c, key, at, self)`, built as the last child of the element. It is out of
  the flow (`Attach`), `IgnoreInput` and `AccessHidden`.
- The frame reads ⌘ once a frame with `shortcut.Hints`. While ⌘ waits it asks for one frame at
  the end of the delay (`c.After`); after that no frame comes while nothing changes (D55).
- The keys come from the one list in `internal/app/shortcut`: the handlers, the menus, the tooltips
  and the chips read it.

## Tests

- Unit: the chips show after the delay and not before; they hide on release, another key, blur and a menu key.
- View: the chips of the tabs, the title bar buttons, the agent rows, the icon column and the notch; the chip does not move the button.

## Replaces

- Nothing.
