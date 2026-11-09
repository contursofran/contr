# UpdatePill

The pill in the title bar that shows a downloaded update, with its panel and its one toast (D176).
After an update, the same pill and panel show What's new for the version that runs (D209).

Shared rules: [README.md](README.md). Design: boards 8 and 9 of the from-zero canvas.

## Anatomy

- **Pill:** `download` icon (14px) and `Restart to update` (`body`), `ink-100`. Height
  `icon-button`, fully round, `float-line` border, `space-10` side padding, `space-6` gap,
  `space-8` right of it to the diff summary. Hover and open: `hover` fill.
- **Panel** (below the pill, its right edge on the pill's right edge, `space-8` apart), 340px,
  `float-solid` (opaque, so the rows behind it do not show through, D212), `float-line`,
  `shadow-float`, `radius-12`:
  1. Head: `contr 0.4.0` (bold, `ink-95`) and `from 0.3.2` (`meta`, mono, `ink-50`).
  2. `What's new` (`ink-50`) and the release notes as a short list (`ink-85`).
  3. Note on a `hover` fill, `radius-8`: `server` icon and `Agents keep running. Their terminals
     live in hc; contr opens them again after the restart, in about 2 s.` (`ink-62`).
  4. Foot, hairline above: `Downloaded 3 min ago` (`ink-50`), `Later` (plain button) and
     `Restart now ⏎` (filled button).
- **After an update** (D209): the pill says `Updated to 0.4.0` (`download` icon). The panel has
  the same head (`contr 0.4.0`, `from 0.3.2`) and What's new, no agents note, and a foot with
  `Updated 3 min ago` (`ink-50`) and one filled button `Got it ⏎`. No `Later`, no `Restart now`.
- **Toast** (below the pill, same card): `contr 0.4.0 is ready` (bold) with a close button;
  `It downloaded in the background. Restart when you like: agents keep running.` (`ink-62`);
  `Later` and `Restart now`.

## States

- No update, a check that runs, a download that runs or failed: no pill.
- Ready: the pill shows until the restart.
- The toast shows once per version, the first time it is ready.
- Updated: on the first start of a version that contr installed, the `Updated to` pill shows
  with its panel open. When the panel closes, the pill goes. It shows once per version, also
  across later restarts. A ready update shows its own pill in its place: the `Updated to` panel
  closes and its note is marked shown, as with `Got it` (#533).
- The two pills have their own open state. The ready panel opens only from a click on its pill,
  never by itself; Return restarts only in that panel.

## Behavior

- The scheduled check (every 15 minutes on the wall clock, and when the app becomes active after 10 minutes; D198) installs a newer version in the background, without a relaunch.
- While an update is ready, the checks go on. A version newer than the ready one installs in the
  background over it; the pill, the panel, the toast and Settings then show the newer version, and
  its toast takes the place of the older one. A failed install of it keeps the ready one (D226).
- A click on the pill opens or closes the panel. A press outside or Escape closes it.
- `Restart now` (panel, toast, Settings) first checks once, with a 5 s limit. A version newer
  than the ready one installs, then `Settings.Relaunch` runs once. A failed check or install
  relaunches into the ready one (D226). `Later` closes the panel or the toast.
- A failed download is logged and shows in Settings, not as a toast.
- After an update, `Got it`, Return, Escape, a press outside or a click on the pill closes the
  panel, removes the pill and marks the note shown (saved off the UI thread).

## Data

- `Settings.Installed()`: version, notes, and when the install ended.
- The release notes: list items of the Markdown, at most 6; plain lines when it has no list.
- `updated.json` in contr's state folder (package `internal/updated`): `version`, `from`,
  `notes`, `installed_at` and `shown` of the last install. contr writes it (temp file and
  rename) off the UI thread when an install ends well, before any relaunch. At start it reads
  it off the UI thread: the panel opens when `version` is the running version and `shown` is
  false. A missing file, another version or `shown` shows nothing; a corrupt file is logged.

## Keys and focus

- Return in the open panel presses `Restart now`, or `Got it` after an update. Escape closes
  the panel.

## Tokens

`float-solid`, `float-line`, `shadow-float`, `hover`, `hairline`, `ink-100`, `ink-95`, `ink-85`,
`ink-62`, `ink-50`, `radius-8`, `radius-12`, `space-4` to `space-16`, `icon-button`.

## Build in MyGo

- Pill: `ButtonBase`. Panel: `PopoverBase`, placed with `AttachTo`. Toast: `ToastBase` in the
  frame's `ToastViewportBase`, attached to the pill. Return: `OverlayShortcut`.

## Tests

- View: no pill without an update; pill and one toast when ready; the toast does not come back
  after Later; the panel opens from the pill; Restart now relaunches; Return restarts; a failed
  download shows no pill.
- After an update: the panel opens at start with the notes; Got it, Return, Escape and a press
  outside or a click on the pill close it and mark it shown; it does not show again; a missing or corrupt file or
  another version shows nothing. Settings saves the note before the relaunch.
- A ready update while What's new shows (panel open or closed): the ready pill and its toast show,
  the ready panel stays closed, the note is saved as shown, and Return does not restart.
- A newer version while one is ready (D226): it installs and the pill, panel and toast show it;
  the same version installs nothing; a failed install keeps the ready one; Restart now installs a
  newer one first and relaunches once, and after a failed check relaunches into the ready one;
  `updated.json` keeps `from` as the version that ran before the first install.
