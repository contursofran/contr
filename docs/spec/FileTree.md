# FileTree

The files panel answers one question: what did the agents change? It opens on the changed files; a switch shows the whole project tree. Changed files show their +/− line counts.

Design system page: component FileTree. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![FileTree, dark](images/FileTree-dark.png) | ![FileTree, light](images/FileTree-light.png) |

## Anatomy

| Part | Value |
|---|---|
| Head | "Files", under the agent's todos and artifacts ([SessionPanel.md](SessionPanel.md), D197); the Changed \| All switch at the right (D264) |
| Switch | `kit.Switch`: "Changed N" (N is the count of changed files; "Changed" until the first read) and "All" |
| Row | height `file-row-height` (26px), padding `0 10px` (D228), gap `space-7`, radius `radius-6` |
| Icon | `folder`, `folder-open`, `file-go`, `file-md`, `file` at 15px, `ink-38` |
| Name | `row-small` (12.5px), `ink-50`; ellipsis |
| Counts | mono 11px (`meta`), `space-6` apart: added and removed both `ink-50`; the sign says which (D264) |
| Path row | Changed view only: a folder that holds changed files, named by its path from the root (`internal/app`), name `ink-90`, with the folder icon. A long path clips at its end with an ellipsis; the row's tooltip gives the whole path (#693) |
| Untracked folder row | Changed view only: a folder that git lists as one untracked entry (its path ends with `/`, as a nested worktree). One path row with the `folder` icon, no row under it, no counts (#693) |
| Indent | each level indents `space-16` plus `space-4` (D228); no guide lines (D258) |

## States

- **Default:** name `ink-50`.
- **Hover:** fill `hover`.
- **Changed:** name `ink-90`, counts show.
- **New:** name `ink-90`, "+N" only.
- **Deleted:** name `ink-50` with a line through it; icon `ink-38`; "−N" `ink-50` (D264).
- **Changed view:** only changed files, under one level of path rows; no unchanged rows.
- **All view:** the whole tree, with the deleted files put back.

## Behavior

- The tree of the selected agent's worktree, else the project folder.
- Changed files show +/− counts.
- The switch opens on Changed when the folder has changes, else on All
  (D264). The owner's pick stays with that folder while the app runs; another
  folder opens on its own default. With no folder the switch does not show.
- Click opens the file in the Review view; ⌘-click reveals it in Finder.
- A click on an untracked folder row of the Changed view does nothing: it
  has no rows to show. ⌘-click reveals it in Finder. In All it is a folder
  as any other (#693).
- The list scrolls in its own area, in Changed and in All (`ui.Scroll` of
  `kit.Panel`). The todos and artifacts, the head and the Context line keep
  their places (#693).

## What "changed" means

- A file is changed when the work tree differs from the merge base of `main`
  and `HEAD`: committed, staged and unstaged edits, deleted files and new
  files that git does not ignore (`review.Repo.Changes`). A deleted file is
  put back in its folder, struck through. A new or deleted file shows one
  count; a binary file shows none.
- The Changed view is made from the changed paths only. At the root: one
  path row per folder that holds changed files (`internal/app`,
  `internal/app/kit`, each its own row), then the changed files of the root.
  Under a path row: its changed files, one level in. Path rows start open.
  A new folder that git does not look into (a nested repository) is one
  change whose path ends with `/`: it is one path row at the root with
  nothing under it.

## Data

- `internal/review` on a goroutine; `mygo.WatchFiles` on the folder starts a
  new read. Big folders: read on open of a folder, not all at once.
- Respect `.gitignore`.

## Keys and focus

- ↑ ↓ move, ← → close and open, ⏎ opens.

## Edge cases

- No folder (no project open): one line, "Files of the open project show here" (`small`,
  `ink-42`), in place of "No folder" (D255). A narrow panel cuts it with an ellipsis.
- Not a git repo: no counts.
- A folder with thousands of files: the list is virtual (`ui.Tree`).

## Accessibility

- Tree roles from `ui.Tree`; labels with the counts.

## Tokens

`file-row-height`, `radius-6`, `space-4`, `space-6`, `space-7`, `space-10`, `space-16`, `hover`, `ink-90`, `ink-50`, `ink-38`, `row-small`, `meta`.

## Build in MyGo

- `ui.Tree` with `TreeItem`s (the row is a `TreeItem` with an empty label;
  the icon, name and counts are added to it). The mock has no disclosure arrow: a folder shows `folder` or
  `folder-open`. `TreeItem` always builds an arrow, so the tree sits in
  `ui.Themed` with `TextMuted` clear (the arrow is not seen) and
  `SurfaceHover` set to `hover`, and the row takes back the room of the
  arrow with a negative start margin (D170). It gives the keys (← → open and close, ↑ ↓ move) and the screen-reader roles.
- Counts: `kit.CountsWith`.
- Switch: `kit.Switch` over `ui.SegmentedBase`.
- Data: `internal/review` on a goroutine; `mygo.WatchFiles` triggers a new read.
- Click opens the file in the Review view; ⌘-click reveals it in Finder.

## Do and do not

**Do**

- Sort folders first, then files, by name.

**Don't**

- Do not color file names by state: weight and ink do it.

## Tests

- Unit: tree build and counts on a temp repo.
- View: opens on Changed with changes and on All without; the pick stays per
  folder; path rows; deleted style; no folder shows the D255 line and no switch;
  an untracked folder is one row; the path tooltip; a long list scrolls in
  its area (#693).

## Replaces

- The v0.2 right panel (board and worker). The right panel shows only files (D122).
