# DiffSummary

A mono button in the title bar that counts the changed lines and files of the selected project and opens the Review view.

Design system page: component DiffSummary. Shared rules: [README.md](README.md).

## Anatomy

`+120  −126  9 files`, in style `value` (mono 11.5/16):

- Added count and file count: `ink-42`.
- Removed count: `ink-30`.

The counts sit `space-8` apart. The button has padding `4px 8px` and radius `radius-7`.

## States

- **Default:** as above.
- **Hover:** fill `hover`.
- **No changes:** the button hides.
- **Only additions:** the removed count hides.

## Behavior

- Click opens the Review view (D37) on the selected project.

## Data

- `internal/review` counts on a goroutine. Read again on a file watch event
  in the project folder, or on a branch change from hc. No timer.

## Keys and focus

- Space and ⏎ when focused.

## Edge cases

- git fails or the folder is not a repo: hide the button and log the error.
- No changes: hide. Only additions: hide the removed count.

## Accessibility

- Label: "120 lines added, 126 removed, 9 files. Open review".

## Tokens

`value` type style, `ink-42`, `ink-30`, `hover`, `radius-7`, `space-8`.

## Build in MyGo

- `ButtonBase` with three `Text` runs; the counts come from `kit.Counts`. Do not write a second counter.
- Data: `internal/review` reads the counts on a goroutine; `Window.Update` sets them. Read again on a file watch event (`mygo.WatchFiles`), not on a timer.
- Click opens the Review view (`editor.DiffView`).

## Do and do not

**Do**

- Use the minus sign "−" (U+2212), not a hyphen.

**Don't**

- Do not color the counts green or red here: the diff colors are only for the diff view.

## Tests

- View: counts render; zero hides; git error hides.
- Unit: the label text for each case.

## Replaces

- The `Review` button of the v0.2 top bar.
