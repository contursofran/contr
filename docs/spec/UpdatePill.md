# UpdatePill

The pill in the title bar that shows a downloaded update, with its panel and its one toast (D176).

Shared rules: [README.md](README.md). Design: boards 8 and 9 of the from-zero canvas.

## Anatomy

- **Pill:** `download` icon (14px) and `Restart to update` (`body`), `ink-100`. Height
  `icon-button`, fully round, `float-line` border, `space-10` side padding, `space-6` gap,
  `space-8` right of it to the diff summary. Hover and open: `hover` fill.
- **Panel** (below the pill, its right edge on the pill's right edge, `space-8` apart), 340px,
  `glass-float`, `float-line`, `shadow-float`, `radius-12`:
  1. Head: `contr 0.4.0` (bold, `ink-95`) and `from 0.3.2` (`meta`, mono, `ink-50`).
  2. `What's new` (`ink-50`) and the release notes as a short list (`ink-85`).
  3. Note on a `hover` fill, `radius-8`: `server` icon and `Agents keep running. Their terminals
     live in hc; contr opens them again after the restart, in about 2 s.` (`ink-62`).
  4. Foot, hairline above: `Downloaded 3 min ago` (`ink-50`), `Later` (plain button) and
     `Restart now ⏎` (filled button).
- **Toast** (below the pill, same card): `contr 0.4.0 is ready` (bold) with a close button;
  `It downloaded in the background. Restart when you like: agents keep running.` (`ink-62`);
  `Later` and `Restart now`.

## States

- No update, a check that runs, a download that runs or failed: no pill.
- Ready: the pill shows until the restart.
- The toast shows once per version, the first time it is ready.

## Behavior

- The scheduled check installs a newer version in the background, without a relaunch.
- A click on the pill opens or closes the panel. A press outside or Escape closes it.
- `Restart now` (panel, toast, Settings) calls `Settings.Relaunch`. `Later` closes the panel or
  the toast.
- A failed download is logged and shows in Settings, not as a toast.

## Data

- `Settings.Installed()`: version, notes, and when the install ended.
- The release notes: list items of the Markdown, at most 6; plain lines when it has no list.

## Keys and focus

- Return in the open panel presses `Restart now`. Escape closes the panel.

## Tokens

`glass-float`, `float-line`, `shadow-float`, `hover`, `hairline`, `ink-100`, `ink-95`, `ink-85`,
`ink-62`, `ink-50`, `radius-8`, `radius-12`, `space-4` to `space-16`, `icon-button`.

## Build in MyGo

- Pill: `ButtonBase`. Panel: `PopoverBase`, placed with `AttachTo`. Toast: `ToastBase` in the
  frame's `ToastViewportBase`, attached to the pill. Return: `OverlayShortcut`.

## Tests

- View: no pill without an update; pill and one toast when ready; the toast does not come back
  after Later; the panel opens from the pill; Restart now relaunches; Return restarts; a failed
  download shows no pill.
