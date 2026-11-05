# ProjectRow

A sidebar row that names a project; the agents of the project list below it.

Design system page: component ProjectRow. Shared rules: [README.md](README.md).

## Anatomy

| Part | Value |
|---|---|
| Row | height `row-height` (30px), padding `0 10px`, gap `space-10`, radius `radius-8` |
| Icon | `project` icon at 15px, in the text color |
| Name | `body-strong`, `ink-85` |
| Meta | `small` (12px), `ink-26`, right side: branch, or "nothing open" |

## States

- **Default.**
- **Hover:** fill `hover`.
- **Empty project:** name `ink-42`, meta "nothing open"; it is a stored project with no terminal.
- **Collapsed:** the agent rows hide.

## Behavior

- One row per open project; stored projects with no terminal show as
  empty (D116).
- Click selects the project's orchestrator, else the project (the main
  checkout, or the folder of an empty project).
- A click on the project that is already selected folds or opens it. The
  icon set has no chevron yet, so the row shows no fold mark; the state is
  in the screen-reader label (expanded). A folded project hides its agent
  rows, which sit under it in the same list (#344).
- An empty project has a `+` icon button at the right of its row. It starts
  an orchestrator in the project folder (D116).

## Data

- `internal/projects` (order, hidden, recent) and the hc terminals.
- Branch from git on a goroutine; read again on a file watch of `HEAD`.

## Keys and focus

- ↑ ↓ move; ⏎ focuses the terminal (D145). A key never folds a project. ← fold and → open are not built yet.

## Edge cases

- No branch (detached or not git): meta shows the short commit or nothing.
- A long name: ellipsis; the meta keeps its width.

## Accessibility

- Tree item, expanded state, label = name and branch.

## Tokens

`row-height`, `radius-8`, `space-10`, `hover`, `ink-85`, `ink-42`, `ink-26`, `body-strong`, `small`.

## Build in MyGo

- One `ui.List` holds the projects and their agents; each row is a `kit.Row`. Do not use `ui.Sidebar`: it shows the selection in the accent color.
- Open and close a project group with `CollapsibleBase`, or keep a collapsed set in the list state.
- Reorder projects by drag with `ListState.Reorder`; store the order with `internal/projects`.
- `.ContextMenu(...)`: Open in Finder, Hide project, Remove.

## Do and do not

**Do**

- Show the projects in the owner's order.

**Don't**

- Do not show a count badge on the project row.

## Tests

- View: empty project style; fold and open; drag order writes the store.
- e2e: select a project on a scratch core and see its orchestrator selected.

## Replaces

- The project rows of `spaces.go` and the worktree rows (D52).
