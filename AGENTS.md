# Working on contr

contr is a native macOS client for herdr-core (`hc`), with an agent board built in. It is written in Go with MyGo's native UI.

## Goal and reference

- The Rust app is at tag `rust-final`. Read it with
  `git show rust-final:<path>`. Do not change it.
- `docs/spec/` is the source of truth for the UI (D118). `docs/DESIGN.md`
  stays the source for the parts the specs do not cover yet.
- `docs/DECISIONS.md` records why things are the way they are.
- `docs/PORT-PLAN.md` records how the Go port was built.

## Stack

- Go 1.27.3, set in `go.mod`. The `go` command downloads this toolchain by
  itself. Do not install Go or other tools.
- MyGo, native UI (package `ui`), pinned in `go.mod`. Upgrade MyGo only in a
  slice of its own.
- MyGo docs are in the module cache:
  `$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)/docs/`. Start with
  `ui/README.md`, `architecture.md`, `ui/testing.md`, `ui/performance.md`,
  `ui/drawing.md` and `ui/custom-widgets.md`. The examples are in
  `examples/` next to `docs/`. When the docs are not clear, the MyGo source
  is the final answer.
- No cgo: do not write `import "C"`. Native calls go through MyGo.

## MyGo first: do not build widgets from zero

MyGo has a large set of widgets and native APIs. Before you write a widget,
a layout helper or a native call, find the MyGo part that does it. Build
your own only when MyGo has nothing for it, and write why in the PR.

How to find it:

1. Read the catalog: `docs/ui/README.md` in the MyGo module (path above).
   It lists each widget with a link to its guide.
2. Look at `examples/gallery`: it shows the controls and layouts in use.
3. Search the API: `grep -n "^func " ui/api_gen.go` in the MyGo module.
4. Search `internal/app/kit` for contr's shared parts.

When MyGo's look is not contr's look (`docs/spec/`), use the unstyled
`*Base` version and style it. A `*Base` widget keeps the keyboard, focus,
Escape, screen-reader labels and the automation hooks that e2e tests click.
`ui/custom-widgets.md` lists them: `TabsBase`, `PopoverBase`,
`SegmentedBase`, `SwitchBase`, `ToggleBase`, `TooltipBase`, `ToastBase`,
`DialogBase`, `CollapsibleBase`, `SelectBase` and others.

| Need | Use (guide in `docs/ui/` unless noted) |
|---|---|
| Rows of any length, reorder by drag | `ui.List`, `ListState.Reorder` (`list.md`, `drag-and-drop.md`) |
| Rows in columns | `ui.Table` (`table.md`), `ui.Outline` (`outline.md`) |
| Items that open and close | `ui.Tree` (`tree.md`), `Collapsible`, `Accordion` |
| Source list with sections | `ui.Sidebar`, `SidebarSection`, `Badge` (`sidebar.md`, `badge.md`) |
| Tabs, segments | `ui.Tabs` / `TabsBase`, `ui.Segmented` / `SegmentedBase` |
| Two panes with a drag divider | `ui.Split` (`split.md`) |
| Panel below an element | `ui.Popover` / `PopoverBase` (`popover.md`, `overlays.md`) |
| Dialog, modal, alert | `ui.Modal`, `Dialog` / `DialogBase`, `AlertDialog` |
| Tip on hover or focus | `.Tooltip(...)` (`tooltip.md`) |
| Short message | `ui.Toast` / `ToastBase` (`toast.md`) |
| Menus | `MenuButton`, `.ContextMenu(...)` (`menu-button.md`, `context-menu.md`) |
| Text fields | `TextInput`, `TextArea`, `SearchField`, `Combobox`, `FindBar` |
| Clipped text | `Text(...).NoWrap().Ellipsis()` (`text.md`) |
| Scroll | `ui.Scroll` (`scroll.md`) |
| Markdown | package `ui/markdown`: `markdown.View`, `markdown.Stream` (`markdown.md`) |
| Code and diffs | package `ui/editor`: code editor, `editor.DiffView` (`code-editor.md`, `diff-viewer.md`) |
| Progress and status | `Progress`, `Spinner`, `Meter`, `Badge` |
| Keys and shortcuts | `c.Shortcut(...)`, focus (`input.md`) |
| Animation | `.Transition(...)`, `AnimatePose`, springs (`transitions.md`, `motion.md`) |
| Terminal | terminal plugin (`docs/plugins/terminal.md`) over `internal/app/ghostty` |
| File watching | `mygo.WatchFiles` (`docs/native.md`) |
| Open and save dialogs, recent documents | `mygo.Dialog`, `mygo.App.AddRecentDocument` (`docs/native.md`, `docs/documents.md`) |
| Click tests, window capture | automation plugin (`docs/plugins/automation.md`) |

Issue #241 lists contr code that still rebuilds MyGo parts. When you touch
that code, move it to the MyGo part in its own small PR.

## Layout

| Path | Holds |
|---|---|
| `cmd/contr` | `main` and the command line |
| `internal/app` | the window content: frame and views |
| `internal/app/kit` | shared view parts: list row, +/− counts, switch, code view, card button |
| `internal/app/view` | what each view gets from the frame: `Env`, `Runner`, `Latest` |
| `internal/app/ghsource` | the gh reads of the workers: pull request per worktree, issue titles, the queue of gh runs, the PR or issue entry |
| `internal/app/settings` | the Settings modal: General, Agents and Updates tabs, update checks and install |
| `internal/app/apptest` | test helpers that the tests of more than one `internal/app` package use |
| `internal/app/reviewview` | the Review view: branches ahead of main, their files or commits, the file as a code view |
| `internal/app/ghostty` | `Conn`: an hc terminal as the connection of MyGo's terminal plugin (D79) |
| `internal/usage` | plan usage: Claude status line, `codex app-server`, Codex logs |
| `internal/statedir` | the folder where contr keeps its own files, and the agent terminal variable |
| `internal/prompt` | reads a blocked worker's menu from terminal text: question and choices |
| `internal/review` | git reads for the Review view: branches ahead of main, files, commits, texts |
| `internal/github` | GitHub pull requests read through the `gh` command line: runner, client, PR |
| `internal/hc` | herdr-core's JSON-RPC API over `hc connect`: calls and event subscriptions |
| `internal/agents` | worker agents in hc terminals: profiles, spawn, list, send, interrupt, stop, read, watch |
| `internal/mcp` | a small MCP server over stdio: tools only |
| `internal/projects` | project order, hidden and recent projects, in one JSON file |
| `internal/prefs` | settings the owner picks in Settings (Appearance), in one JSON file |
| `internal/widths` | sidebar and panel widths the owner dragged, in one JSON file |
| `internal/owner` | questions and to-dos for the owner, in one locked JSON file |
| `internal/jsonfile` | atomic JSON file writes (temp file and rename) and loads, for the stores above |
| `assets` | fonts, status icons, app icon |
| `kit` | Claude Code plugin for agent work: agent definitions and the coordinator playbook |
| `scripts` | `check.sh`, `bundle.sh`, `mygo-config.sh`, `install.sh`, `next-version.sh`, `release-notes.sh`, `agent-metrics.py` |

Add packages under `internal/` when a change needs them.

## Rules

- `scripts/check.sh` must pass before you report done: gofmt, `go vet`,
  `go test -race`, and a build with `CGO_ENABLED=0`.
- Never block the UI thread. MyGo runs views and handlers on the main
  thread. hc I/O, file reads and git scans run on goroutines, and their
  results reach the views through `Window.Update` or `Invalidate`.
- Performance is not a goal by itself (owner, 2026-11-03, D55). Keep the
  app responsive and do not use CPU while it is idle. Fix a slow part when
  someone sees it. Do not add benchmarks, perf gates or tuning work.
- No `panic` in non-test code: return errors. Do not drop errors silently:
  log them or show them.
- The UI follows `docs/spec/` exactly (D118), and `docs/DESIGN.md` for the
  parts the specs do not cover. Views use token names only, never a color
  value, so each theme works (D119). Sans for interface text, mono for
  values and the terminal; greys (inks) with `ink-100` only for what is
  selected or needs the owner; hues only `blocked` and `claude`.
- Build views from `internal/app/kit` and MyGo's components (`ui.List`,
  `ui.Segmented`, `editor.DiffView`, `Text.NoWrap().Ellipsis`); do not build
  rows, clipping, scroll lists or code views by hand again.
- Small changes: one issue, one branch, one PR, ideally under about 400
  changed lines. Test-heavy changes stay whole.
- Adapted code: `NOTICE` credits the projects that code comes from. When you
  port a Rust file that starts with `// Adapted from Herdr UI (Apache-2.0);
  changes: …`, keep that comment in the Go file and add "ported to Go" to
  the changes. Code for herdr-core's framed mode
  keeps its note "Written from herdr-core's wire layout; see NOTICE." To adapt code from MyGo's examples or plugins (MIT),
  add the same kind of header comment and a `NOTICE` entry.
- Do not name or link other GitHub users or repositories anywhere except
  `NOTICE`, and do not @-mention people. Import paths and `go.mod` are
  exempt.
- Write no local paths (such as `/Users/...`) in the repository or in
  commit messages. The code moves to a public repository later.
- Docs, comments and commit messages: concise ASD-STE100 Simplified
  Technical English.

## hc safety

- The same rules apply to herdr-core (`hc`): never stop, restart or send
  input to the owner's core. Tests that need a real core use a scratch core
  with its own `HC_SESSION` and XDG folders, under the `hccore` build tag
  (see `internal/hc/core_test.go`).
- Never connect to the owner's hc sockets or send input to them.
- For a live app, use a scratch hc core: `hctest.New` in
  `internal/hc/hctest` starts one with its own session and XDG folders and
  stops it when the test ends.
- Tests use fakes: fake servers on sockets in temporary folders. No test touches the owner's core, `~/.claude`,
  `~/.codex` or a real repository.
- Do not read `~/.claude` or `~/.codex`. Do not install the app.

## Testing

- Unit and view tests run with `go test`. Test a view with `ui.NewTester`:
  it builds the view without a window, clicks by label, sends keys and
  reads texts.
- Add a test for each new behavior, error paths included.
- End-to-end tests use MyGo's automation plugin, in files with
  `//go:build e2e`. The plugin lists elements, clicks, types and captures a
  window as a PNG. To check a visible change, capture the window and look at
  the PNG yourself (decision D5).
- To look at the app, run `scripts/see.sh <fixture> [dark|light] [label…]`
  (D178). It starts a fixture against a scratch core, presses the labels in
  order, and writes `tmp/see/<fixture>-<theme>.png` and a `.txt` of the
  elements. Check the data in the `.txt`; open the PNG for layout and color.
  `scripts/see.sh` alone lists the fixtures. Add a fixture in
  `cmd/contr/e2e_see_test.go` when none shows your state.
- To drive a live scratch app with agent-desktop, run
  `scripts/explore.sh <fixture> [dark|light]` (#522). It builds the release
  app as `contr-explore`, loads the fixture in a scratch core, keeps the app
  open and prints one JSON line (`app`, `pid`, `window_id`, `scratch`,
  `hc_session`). Always pass `--app contr-explore --window-id <id>`, never
  `--app contr`: that is the owner's app. Read terminal text with `hc read`
  on the scratch core. `scripts/explore.sh stop` ends it and removes the
  scratch folder.
- New e2e tests use the harness in `cmd/contr/e2e_harness_test.go`
  (`newHarness`, `h.spawn`, `h.owner`, `h.theme`, `h.launch`, `c.press`).
  Do not set up the scratch home and env by hand.
- To compare a component with its spec, run `scripts/sheet.sh [Part…]`
  (D179). It draws each part of `sheetParts` (`internal/app/sheet_test.go`)
  under its image in `docs/spec/images`, in Dark and Light, and writes
  `tmp/sheet/<Part>-<theme>.png`. It needs no window and no hc. Run it for
  the parts your change touches, look at each sheet, and give the paths in
  your report. Add a part when you build a spec'd component that has none.
- Run the app only against a scratch hc core and a scratch project under
  `/tmp`, never against the owner's core.
- On the owner's Mac, run one heavy job at a time: an app run with a scratch core,
  an e2e run or a release build. `go build` and `go test` are light and can
  run in parallel.

## Workflow (decision D27)

Work is tracked in GitHub issues and pull requests.

- One issue, one branch `agent/<issue>-<slug>`, one PR into `main`. Ideally
  the PR is under about 400 changed lines.
- `scripts/check.sh` and the `CI` check must pass before you ask for review.
  A PR that changes only Markdown (`*.md`) does not run `CI`: it runs the
  `Docs` check (conflict markers and decision ids), and that must pass.
- Update a branch by merging `main` into it. Never force-push. Do not
  rewrite commits that a reviewer has seen.
- Commit with clear messages. No `Co-Authored-By`, AI attribution or
  generated-by lines. Temporary files go in `tmp/`, which git ignores.
- Squash merge is the default. A reviewer approves the exact head. The
  coordinator merges that head once CI passes on it (owner, 2026-11-03).
  A builder does not merge its own PR.
- When your change makes a doc wrong (`docs/DECISIONS.md`, `docs/DESIGN.md`,
  `docs/spec/`),
  update it in the same PR. A new decision gets a new row in
  `docs/DECISIONS.md`. Do not delete old rows; mark them "Replaced by Dn".
- Report the PR number, the head SHA, the check status and any deviation.
