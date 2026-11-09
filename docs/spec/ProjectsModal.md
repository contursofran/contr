# ProjectsModal

One palette dialog that shows, adds and hides the projects of the sidebar (#494, D207).

Opens from the "Open projects" item of the Projects head `+` menu (D171). The owner-approved mock-up of #494 is the picture; no spec image exists yet (the sheet part comes in slice 6).

## Anatomy

```
┌ dialog 560, glass-float ────────────────────────────────────────────┐
│ ⌕  Search projects, or paste a GitHub repo or a path            esc │  search
├─────────────────────────────────────────────────────────────────────┤
│ Open · 2                              drag to reorder the sidebar   │  section + hint
│ ⋮⋮ [C ] contr       ~/code/contr · main · orchestrator idle  Hide ⏎ switch │
│ ⋮⋮ [CR] contr-rune  ~/code/contr-rune · main · 1 needs you          │
│ Recent                                                              │
│    [S ] site        ~/code/site · main                      10m ago │
├─────────────────────────────────────────────────────────────────────┤
│ ↑↓ move   ⏎ switch   ⌘⌫ hide                  [Open folder… ⌘O]    │  footer
└─────────────────────────────────────────────────────────────────────┘
        toast: "<name> hidden from the sidebar"  Undo
```

| Part | Value |
|---|---|
| Dialog | Overlay with `scrim`. Fill `glass-float`, border `float-line`, shadow `shadow-float`, width 560, radius `radius-14`, 110 px from the top, centred. Enter motion: up 8 px and fade in, 0.22 s. |
| Search | One `ui.SearchField`, padding 14×16, no border, `hairline` below. Text 15 px `ink-100`; placeholder `ink-30`; search icon `ink-38`; a `kbd` `esc` at the right. It has the focus when the dialog opens. |
| List | One `ui.List`; padding 6; scrolls after 360 px. |
| Section | Padding 10×10×4. Name 11 px, weight 500, `ink-45`. Hint 11 px `ink-30`, at the right. |
| Row | Padding 7×10×7×4, radius `radius-8`, gap `space-10`. Columns: grip 12, tile 30, text, actions. Hover fill `hover`; selected fill `selected`. |
| Grip | `⋮⋮` 12 px `ink-30`. Shows only on a selected or hovered open row, and only when the field is empty. It keeps its space. |
| Tile | 30×30, radius 8, fill `selected` (the mock-up uses 6% ink; there is no tile token), initials 11 px weight 600 `ink-62`. An add row shows an icon (`download` or `folder`) in `ink-62`. |
| Name | Weight 600 `ink-100`; a recent name is weight 500 `ink-85`. One line, ellipsis. The part that matches the field is underlined in `ink-100`. |
| Meta | Mono 11.5 px `ink-38`, one line, ellipsis. |
| Need you | The `N need you` part of the meta is `blocked` text. This is the one exception to the `blocked` rule (Q1). |
| Row actions | `Hide`: 12 px `ink-45`, radius 6, hover fill `hover`; shows on hover or selection. The hint is mono 11 px `ink-38`, only on the selected row. A recent row shows its age (mono 11 px `ink-30`) instead; the age hides on hover or selection. |
| Progress | 2 px line, radius 2, track `hairline`, bar `ink-62`, indeterminate (Q3). |
| Error line | Mono 11.5 px `blocked`, wraps, under the meta. |
| Empty | Text `ink-45`, centred, padding 22×12. |
| Footer | Padding 10×16, `hairline` above, text 11.5 px `ink-45`, gap 14. `kbd`: mono 10.5 px weight 500 `ink-45`, border `input-line`, radius 5, padding 3×5. |
| Ghost button | `Open folder… ⌘O`: border `input-line`, radius 7, padding 5×10, 12 px `ink-62`; hover fill `hover` and `ink-100`. Hidden when the app has no folder picker. |
| Toast | Shown in the frame's toast viewport, so it outlives the dialog. Text `<name> hidden from the sidebar`; action `Undo`: weight 600 `ink-100`, no fill. |

## States

The states match the state bar of the mock-up.

| State | The list shows | Enter label |
|---|---|---|
| At rest | `Open · N`, then `Recent`. | `switch` on an open row, `open` on a recent row |
| Filter `rune` | Open and Recent rows whose name has the text (case-insensitive), with the match underlined. The hint, the grips and drag are off. `Open · N` still counts all open projects. | as above |
| Paste a GitHub repo | Section `Add` with one row: `Clone owner/name`, meta `into <parent>/<name>`, action `Change folder`. No Open or Recent rows. | `clone` |
| Paste a path | Section `Add` with one row: `Open <folder>`, the path as meta. | `open` |
| Cloning | Row `Cloning owner/name`, the progress line, action `Cancel`. | `wait` (Enter does nothing) |
| Clone failed | Row `Could not clone owner/name`, a red error line, action `Try again`. | `try again` |
| No match | The empty text. | none |

The field is a repo when it is `owner/name`, `https://github.com/owner/name` (with or without `www.`, `.git` or a final `/`) or `git@github.com:owner/name` (with or without `.git`). It is a path when it starts with `/` or `~`. Any other text filters.

## Copy

Exact strings:

- Placeholder: `Search projects, or paste a GitHub repo or a path`.
- Sections: `Add`, `Open · N`, `Recent`.
- Heading hint (not while filtering): `drag to reorder the sidebar`.
- Add rows: `Clone owner/name`, meta `into <parent>/<name>`, `Change folder`, `Cancel`, `Try again`, `Open <folder>` (the path is the meta).
- Hide: `Hide`; toast `<name> hidden from the sidebar` with `Undo`.
- Empty: `No project matches "<text>". Paste a GitHub repo or a path to add one.`
- Footer: `↑↓ move`, `⏎ <action>`, `⌘⌫ hide`, ghost `Open folder… ⌘O`.
- A zero count never shows: no `0 worktrees`, no `0 need you`. The count reads `1 needs you` or `N need you` (N > 1).

## Behavior

- Every change saves at once (D63). There is no `Done`. Esc or a click outside closes the dialog.
- The meta of an open row is `<path with ~> · <branch> · <live state>`. A part with no value is left out.
- Live state (Q5): the `Hold` text of the top holder of the main worktree row, for example `orchestrator idle`. When worktree rows need the owner (`HoldYou`), `N need you` takes its place. A 6 px `claude` dot comes before the live state when an orchestrator holds the row.
- Hide (D70) hides the project in contr only. It never closes an hc terminal or stops an agent. It also touches the project in `recent`, so it is the first row of Recent (Q8). The toast has `Undo`: it shows the project again and restores its old place in the order. Undo still works after the dialog closes (Q7).
- Open row: Enter or a click switches the sidebar to the project and closes the dialog.
- Recent row: Enter or a click shows the project again (D70) and adds it to the sidebar. A hidden project shows in Recent while it has a `recent` entry.
- Recent keeps at most 10 entries, newest first, each project once (D116).
- Clone: the destination is the parent folder of the last opened project (D117), else the first of `~/Developer`, `~/code` and `~/Documents/code` that exists, else the home folder. `Change folder` opens the folder dialog. Closing the dialog cancels a running clone and adds nothing. A second clone waits for the first. On success the folder joins the store and `recent`, like Open folder (D116).
- Path row: Enter expands `~` and checks the folder on a goroutine. A path that is not a folder shows a red line under the row, `not a folder: <path>`, with no `Try again` (Q6).
- Drag reorders only in the Open section and only when the field is empty. A drop on Recent is ignored.
- Editing the field moves the selection to the first row and clears a clone error.
- The pointer moves the selection; the keys move it too.

## Keys

| Key | Does |
|---|---|
| `↑` `↓` | Move the selection; stop at the ends. |
| `⏎` | Run the selected row (see the Enter label). |
| `⌘⌫` | Hide the selected open row. |
| `⌘O` | Open the folder dialog (same as the ghost button). |
| Esc | With text in the field: clear it. With an empty field: close the dialog (Q2). |

## Data

- `internal/projects` (order, hidden, recent) and the repo groups of the sidebar. `git.ParseRemote` reads a repo field.
- Branch and live state come from the groups already loaded; the dialog starts no scan.
- Clone and the folder check run on goroutines. They never block the UI thread.

## Edge cases

- Many projects: the list scrolls and the selected row stays in view.
- A long name or path: ellipsis; the actions keep their width.
- No folder picker: the ghost button and `Change folder` are hidden, and `⌘O` does nothing.
- No store: no Hide and no toast.
- A repo field with no clone parent folder: the text filters like any other text.

## Accessibility

- The dialog label is `projects modal`; the field label is `Search projects, or paste a GitHub repo or a path`; the list label is `projects list`.
- A row label is `project <name>`; the `Hide` label is `Hide <name>`. The add rows are `clone row` and `path row`.
- The selected row reports selected. The progress line says `Clone progress`.

## Tokens

`scrim`, `glass-float`, `float-line`, `shadow-float`, `radius-14`, `radius-8`, `hairline`, `hover`, `selected`, `input-line`, `ink-100`, `ink-85`, `ink-62`, `ink-45`, `ink-38`, `ink-30`, `blocked`, `claude`, `space-10`, `focus-ring`.

## Build in MyGo

- One `ui.List` over one slice of rows. `ListState.Header` names the sections. `ListState.Reorder` is on only while the field is empty (`list.md`, `drag-and-drop.md`).
- `ui.SearchField` for the field (`search-field.md`); it clears on the first Esc.
- `ui.Progress`, indeterminate, restyled, for the clone line (`progress.md`).
- `ui.Toast` with an action for Hide (`toast.md`); the frame owns it.
- Use `kit` parts where they fit. Do not draw rows by hand.

## Decided questions (#494, D207)

- Q1: `N need you` may use `blocked` text in this one place. The `blocked` row of `tokens.md` says so.
- Q2: the first Esc clears the field; Esc on an empty field closes.
- Q3: the clone line is indeterminate; no percent.
- Q4: surface `glass-float`, width 560, radius `radius-14`, shadow `shadow-float`.
- Q5: live state is the `Hold` text of the main worktree row.
- Q6: a path that is not a folder shows a red line under the row.
- Q7: Undo works after the dialog closes.
- Q8: a hidden project goes to the top of Recent; opening it shows it again.
- Q9: a `ProjectsModal` sheet part covers the states that `scripts/see.sh` cannot reach (slice 6).

## Tests

- View tests (`ui.NewTester`): Esc clears, then closes; filter underlines; no-match text; keys and Enter per row kind; `⌘⌫` hides and saves; no zero counts; footer label per row kind; clone, cancel, error and path rows; Hide toast and Undo, also after the dialog closes.
- e2e: open the dialog from the menu; `scripts/see.sh projects dark "Open projects"` and `light`.

## Replaces

The v0.2 "Projects modal" of `docs/DESIGN.md`: the header with `esc`, the filter field, the `open in contr` and `recent` lists with `close` and `open` buttons, `+ open folder…`, the separate clone field with `Choose folder…` and `Clone`, and `Done`.
