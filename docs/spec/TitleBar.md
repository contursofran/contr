# TitleBar

The title bar holds the traffic lights, the tabs of the selected agent, the diff summary and three icon buttons; it is also the drag area of the window.

Design system page: component TitleBar. Shared rules: [README.md](README.md).

## Anatomy

From left to right:

1. Traffic-light area, as wide as the sidebar plus its gap (346px in the 1710px window), so the tabs start above the terminal.
2. Tabs (see Tab) and a "New tab" icon button.
3. Flexible space (drags the window).
4. DiffSummary of the selected project.
5. Icon buttons: Split terminal, Search, Hide files (pressed while files show).

Height `titlebar-height` (46px), gap `space-8`, right padding `space-14`. Items center on the vertical axis.

## States

- The tabs follow the selected agent row in the sidebar.
- DiffSummary hides when the project has no changes.
- Hide files is pressed (`aria-pressed`) while the files panel shows.

## Behavior

- The tabs are the hc terminals of the agent selected in the sidebar
  (orchestrator or worker), plus its plain terminals.
- `+` (New tab) opens a plain terminal in the folder of the selected agent
  (D82).
- DiffSummary counts the selected project.
- Split and Search are open point O12: show the buttons disabled until it
  settles, or leave them out.

## Data

- Tabs: `internal/agents` and the hc terminal list, through `Window.Update`.
- Diff counts: see DiffSummary.

## Keys and focus

- `⌘T` new tab, `⌘W` close tab (plain terminals only, D85), `⌘D` split,
  `⌘F` search, `⌘⇧E` files.
- The tooltips show each key.

## Edge cases

- No agent selected: no tabs; `+` still shows.
- Many tabs: tabs shrink to their min width, then the row scrolls.

## Accessibility

- The tab row is a tab list; each button has a label and a tooltip.

## Tokens

`titlebar-height`, `space-8`, `space-14`, `ink-*` through its children.

## Build in MyGo

- Draw the bar in the view; mark the empty parts as the window drag area so the window moves and a double-click zooms.
- Tabs: `TabsBase` (keeps arrow keys, focus and the automation hooks).
- Buttons: IconButton (`ButtonBase`).
- Shortcuts: `c.Shortcut(...)` for new tab (⌘T), close tab (⌘W), split (⌘D), search (⌘F), files (⌘⇧E). Show them in the tooltips.

## Do and do not

**Do**

- Keep the tabs aligned with the left edge of the terminal panel.

**Don't**

- Do not put a window title or a project name in the bar: the sidebar shows them.

## Tests

- View: selecting another agent changes the tabs.
- View: `+` opens a plain terminal (fake hc); `⌘W` closes a plain terminal
  tab and ignores an agent tab.
- View: Hide files is pressed while the files panel shows.

## Replaces

- `titlebar.go` and the top bar of `frame.go` (D50 tabs of the open project).
