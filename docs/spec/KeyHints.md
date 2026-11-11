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

One rule places every chip (D248): its center is on the center of the element's icon or glyph,
and it lies fully inside the element and the window. A chip wider than the icon ("⇧E", "⌥↓")
stays centered on it, and moves in along an edge of the element or the window when it would
leave it. No chip is clipped. The icon column (D128) follows the same rule.

| Element | Chip | On |
|---|---|---|
| Title bar tab | `1` to `8` for the first eight tabs; `9` on the last tab past them | the tab's glyph |
| New tab, Search, Hide files | `T`, `F`, `⇧E` | the icon |
| Sidebar Settings | `,` | the icon |
| Icon column: Open projects, New orchestrator | `O`, `N` | the icon |
| Agent rows above and below the selected row | `⌥↑`, `⌥↓` | the status glyph (also of a worker row and in the icon column) |
| Project rows before and after the open project | `⌥←`, `⌥→`; with two projects the other row shows `⌥→` | the folder icon |
| Other project rows | `⇧1` to `⇧8` for the first eight projects; `⇧9` on the last project past them (D289) | the folder icon |
| Questions notch | `J` | the dot |
| Chat \| Terminal switch | `E` | the switch (it has no icon) |

A disabled button shows no chip. With one project no project row shows a chip.

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
- An element narrower than its chip: the chip is centered on the element.
- ⌘ held while the window is not active: no chips.

## Accessibility

- Screen readers do not read the chips (`AccessHidden`). Each element's label and tooltip name its key.

## Tokens

`glass-float`, `float-line`, `ink-100`, `kbd`, `radius-4`, `space-4`.

## Build in MyGo

- One part: `kit.KeyHint(c, key, box, icon)`, built as the last child of the element `box`, with
  `icon` its icon or glyph element. It is out of the flow (`Attach` on the center of `box`, then
  `Translate` to the icon's center, held inside `box` and the window), `IgnoreInput` and
  `AccessHidden`. The place comes from the previous frame's boxes (`LayoutBounds`), which the
  element has before ⌘ is held for the delay; the chip takes its width from its measured text, so
  its place needs no layout of its own.
- The frame reads ⌘ once a frame with `shortcut.Hints`. While ⌘ waits it asks for one frame at
  the end of the delay (`c.After`); after that no frame comes while nothing changes (D55).
- The keys come from the one list in `internal/app/shortcut`: the handlers, the menus, the tooltips
  and the chips read it.

## Tests

- Unit: the chips show after the delay and not before; they hide on release, another key, blur and a menu key.
- View: the chips of the tabs, the title bar buttons, the agent rows, the project rows, the icon column and the notch; each chip lies inside its element and the window and is centered on its icon; the chip does not move the button.
- Unit: the move of a chip near each edge of its element and of the window.

## Replaces

- Nothing.
