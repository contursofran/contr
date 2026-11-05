# Window

The contr window is one glass sheet over a dark ground: a title bar on top, then three panels in a row (sidebar, terminal, files).

Design system page: component Window. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Ground | `ground` with three soft glows (`glow-slate`, `glow-umber`, `glow-graphite`) behind the glass |
| Glass sheet | `glass-window`, blur `glass-blur`, saturate `glass-saturate`, radius `radius-14`, border `window-line`, inner top line `shadow-window-top` |
| Title bar | `titlebar-height` (46px), see TitleBar |
| Body | row of panels, gap `space-10`, padding `0 10px 10px` |
| Panels | sidebar `sidebar-width` (312px), terminal (fills), files `files-width` (280px) |

## States

- **Default:** three panels.
- **Files hidden:** the files toggle in the title bar removes the right panel; the terminal grows.
- **Inactive window:** macOS greys the traffic lights. Nothing else changes.

## Behavior

- The window opens at its last size. The sidebar and files widths are the
  ones the owner dragged (D111), else the defaults here.
- Limits: sidebar 200–480 (D111), files 220–720. The terminal keeps at least
  480.
- The files toggle (`⌘⇧E`, the title bar button) hides and shows the files
  panel. A hidden panel keeps its width.
- Narrow window (O6, settled by the owner): below 1100px window width the
  files panel hides, and its toggle is disabled. The toggle keeps its state,
  so the panel comes back when the window is wide again. The sidebar
  collapse to icons below about 800px comes with the sidebar issues.

## Data

- Widths: `internal/widths` (read at start, written on drag end).
- Theme: `mygo.Theme` (see README Themes).

## Keys and focus

- `⌘⇧E` files panel. Other keys: README Keys.
- Tab order: sidebar, terminal, files panel.

## Edge cases

- Window narrower than sidebar + 480 + files, but 1100px or more: the files
  panel gives way down to 220, then the sidebar down to 200. The stored
  widths do not change.
- Reduce transparency: solid fills (O13).

## Accessibility

- The three panels are groups with the names "Sidebar", "Terminal", "Files".

## Tokens

`ground`, `glow-*`, `glass-window`, `window-line`, `shadow-window-top`, `radius-14`, `space-10`, `titlebar-height`, `sidebar-width`, `files-width`.

## Build in MyGo

- Window: set `Vibrancy` to `VibrancyGlass` so macOS draws the real backdrop blur. The CSS glows only stand in for the desktop behind the window. Do not paint glows in the app.
- Hide the system title bar and draw the title bar in the view (see TitleBar). Keep the system traffic lights.
- Body: a row with `ui.Split` between the panels, so the owner can drag the sidebar and files widths. Store the widths with `internal/widths`.
- Use `Material(...)` (glass plugin, `docs/plugins/glass.md`) only where MyGo needs a material for a floating part, not for each panel.

## Do and do not

**Do**

- Keep 10px between all panels and the window edge.
- Keep the default widths 312 and 280; let the owner drag them.

**Don't**

- Do not add a status bar or a footer to the window.
- Do not use color on the ground or the glass.

## Tests

- View: widths load from a fake `internal/widths` store; a drag writes them.
- View: `⌘⇧E` hides and shows the files panel; the terminal width grows.
- e2e: open the window on a scratch core in each theme and capture it.

## Replaces

- `frame.go`: the v0.2 frame (top bar, content row, bottom row, footer).
  The bottom row and footer go only after O2 and O4 settle (see the
  cleanup issue).
