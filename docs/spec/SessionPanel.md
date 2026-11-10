# SessionPanel

The top of the files panel shows the selected agent's todos and the
artifacts it worked on, over the file tree (D197).

Shared rules: [README.md](README.md). Design: canvas board 11.

## Anatomy

Top to bottom in the files panel: Todos, Artifacts, the head "Files" with
the file tree, the Context meter. A section with no items is not shown.

| Part | Value |
|---|---|
| Section head | padding `2px 4px 4px 10px`, gap `space-8`; name `label` (12px, 500) `ink-45`; count mono 11px `ink-38` at the right, `space-6` from the edge |
| Section end | `space-20` of space; no hairline (D258) |
| Row | height `file-row-height` (26px), padding `0 10px`, gap `space-8`, radius `radius-6`; text `row-small` (12.5px), one line with an ellipsis |
| Todo icon | 14px: `todo` (empty circle) `ink-35`; `todo-done` (check circle) `ink-30`; in progress the 14px dot spinner (StatusGlyph) with its lit dots in the row ink |
| `you` tag | plain text, mono 10.5px `ink-50`; no ring (D258) |
| Artifact row | `artifact` icon 14px `ink-38`, title `ink-60`, age mono 11px `ink-38` at the right |

## States

- **Todo pending:** text `ink-60`.
- **Todo in progress:** the spinner, the `active` text, `ink-90`.
- **Todo completed:** text `ink-30` with a line through it.
- **From the owner's prompt:** the `you` tag at the right.
- **Hover:** fill `hover`.

## Behavior

- Data: `<state dir>/session/<terminal>.json` of the selected agent's hc
  terminal (package `session`, D195). The app reads it on a goroutine and
  watches it; a change shows at once. A missing or bad file shows no
  section; a bad file is logged.
- Todos head: "N of M" done. Artifacts head: the number of rows.
- Artifact age: `now`, minutes (`38m`), hours (`5h`), days (`2d`).
- A click on an artifact opens its URL in the browser.
- One row per artifact: an entry whose slug is a claude.ai link
  (`https://claude.ai/artifact/<id>`) and an entry with the same id are one
  row, the most recent.
- Not yet: the dot for "changed since last opened".
