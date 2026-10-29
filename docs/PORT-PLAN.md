# Port plan: Rust contr to Go with MyGo

Reference: Rust contr at `fae6154` (2026-10-24), about 43,600 lines of Rust
and about 1,240 tests. The port is done when the owner runs the coordinator
in the Go contr instead of a terminal for a day: answering decisions,
ticking to-dos and switching between workers without `/hrd-side`. That is
the v0.1 rule of the Rust tracker.

## How to port

- One milestone is one or more slices (`M2.1`, `M2.2`, …). One slice is one
  branch, ideally under about 400 changed lines; test-heavy slices stay
  whole. The orchestrator tracks slices outside the repository and merges
  them into local `main` (decision D8).
- Port the behavior and the Rust test cases of each module (the "Tests"
  column). A milestone is done when its ported tests pass and the views
  match `docs/DESIGN.md`.
- Pure modules first: they need no window and carry most of the tests.
- herdr protocol code keeps its wire format byte for byte. Port the Rust
  tests that pin the bytes.
- Views: build them with MyGo's `ui` package. Check each visible change with
  a PNG capture from the automation plugin and look at it.

## Milestones

"Rust" paths are relative to `crates/` in the reference. "After" names the
milestones that must merge first. Milestones without a shared "After" can
run in parallel.

| # | Milestone | Rust source (lines) | Tests | Go package | After |
|---|---|---|---:|---|---|
| M0 | Module, empty MyGo window, checks, CI file for later | `contr/src/main.rs` (window part) | — | `cmd/contr`, `internal/app` | — |
| M1 | Theme tokens, status icons, `--icons` legend | `theme.rs` (169), `status_icon.rs` (297), `legend.rs` (147) | ~10 | `internal/app/theme`, `internal/app/icons` | M0 |
| M2 | herdr JSON API: wire, model, reader, commands, tabs, agents | `contr-herdr/src/{wire,model,reader,command,tabs,agents}.rs` (4,623) | ~137 | `internal/herdr` | M0 |
| M3 | herdr endpoint socket: codec, messages, surface, conn, link, OSC 52 | `contr-herdr/src/endpoint/*` (4,789) | ~137 | `internal/herdr/endpoint` | M0 |
| M4 | Frame: three columns, visibility, menus and actions, herdr glue, selection, status message | `frame.rs` (580), `visibility.rs` (99), `menus.rs` (125), `actions.rs` (176), `herdr.rs` (338), `selection.rs` (109), `status_message.rs` (159) | ~40 | `internal/app` | M1, M2 |
| M5 | Terminal view: grid metrics, colors, paint, box drawing, decorations, keys and IME, wheel, selection and copy, panes | `contr/src/terminal/*` (7,256) | ~215 | `internal/app/terminal` | M3, M4 |
| M6 | Title bar: agent tabs, "+", split, panel toggle, tab commands | `title_bar.rs` (787), `tab_commands.rs` (151) | ~25 | `internal/app` | M4 |
| M7 | Sidebar: git scan, spaces model and column, agents list | `git.rs` (554), `spaces_model.rs` (698), `spaces.rs` (1,970), `agents_list.rs` (1,243) | ~120 | `internal/git`, `internal/app` | M4 |
| M8 | Board, read-only, and the Report tab | `board_model.rs` (1,115), `board.rs` (773), `board_panel.rs` (1,943), `board_work.rs` (588), `board_needs.rs` (404), `report.rs` (681), `board_report.rs` (1,078) | ~170 | `internal/board`, `internal/app` | M4 |
| M9 | Answer card and actions: undo window, outbox | `board_card.rs` (1,717), `board_actions.rs` (857), `board_outbox.rs` (884) | ~90 | `internal/board`, `internal/app` | M8 |
| M10 | Status bar: plan usage, host CPU and memory, `contr statusline` | `contr-usage/src/*` (1,754), `usage.rs` (517), `host.rs` (494), `status_bar.rs` (721) | ~100 | `internal/usage`, `internal/app` | M4 |
| M11 | Command line: `contr`, `--icons`, `statusline`, `--help` | `main.rs` (520) | ~10 | `cmd/contr` | M1, M10 |
| M12 | App bundle and install script | `scripts/bundle.sh`, `scripts/install.sh`, their tests | — | `scripts` | M11 |
| M13 | Owner test and move to the contr repository (no side-by-side run, D25) | — | — | — | all |

Not ported (decision D5): `dev_client.rs`, `dev_protocol.rs`,
`dev_control/*` (about 3,900 lines, 93 tests). MyGo's automation plugin does
that job. Port a dev-control test case only when it checks app behavior, not
the control itself; it then becomes a view test or an e2e test.

## Notes per milestone

- **M2, M3:** pure code with no UI. They carry the protocol tests. Port the
  fake servers from `endpoint/testing.rs` first. Live tests
  (`contr-herdr/tests/*live.rs`) run only against `scripts/dev-herdr.sh` and
  only behind a build tag.
- **M4:** the Rust `visibility` module sets the "no work while hidden"
  rule. Find the MyGo signal for a hidden, minimized or covered window and
  write it down in `docs/DECISIONS.md`.
- **M5:** herdr sends ready cell grids. contr draws cells; it does not
  emulate a terminal. Use a `Box(...).Draw` painter with MyGo's glyph atlas
  (`Painter.Text`, `Fill`). MyGo's terminal plugin (`plugins/terminal`,
  MIT) paints a cell grid and box glyphs. Read it before you write the
  painter. For IME, read MyGo's `ui/text-input-client.md`. Measure the
  painter with the grid benchmark in `docs/BENCHMARKS.md`. It must stay at
  or under the numbers recorded there.
- **M8, M9:** the board files come from the herdr-agents mod (board schema
  v1). Keep the file formats exactly. The coordinator mod reads what contr
  writes.
- **M10:** usage sources read Claude Code's status line input and Codex's
  rollout logs. Tests use fixtures only. They never read `~/.claude` or
  `~/.codex`.
- **M13:** measure idle and load CPU and memory of the Go app as in
  `docs/BENCHMARKS.md`. Then the owner's one-day test. There is no
  side-by-side run of the Rust and Go apps (D25). If the owner finds the Go app better, the code moves into the
  `contursofran/contr` repository in place of the Rust app, and CI starts
  there (decision D8).

## State

| # | State |
|---|---|
| M0 | Done in the first commit |
| M1 | Done (M1.1). The `AgentState` to icon mapping moves to M4. |
| M2 | Done (M2.1–M2.7). Live tests run with `-tags live` against `scripts/dev-herdr.sh`. |
| M3 | Done (M3.1–M3.6). Live tests run with `-tags live` against `scripts/dev-herdr.sh`. |
| M4 | Done (M4.1–M4.4). The frame test for title bar clicks moves to M6. The redraw test checks that a slot builds once per redraw (MyGo is immediate mode). |
| M5 | Done (M5.1–M5.7f). Held keys repeat: the app registers ApplePressAndHoldEnabled = NO in its own defaults (D20). The Shift+wheel check needs a real device and is open for the owner. |
| M7 | Done (M7.1–M7.4). Rows redraw one by one through cached layers (D12). The spaces column rescans git every 10 s while the window is shown, as Rust does (D15). |
| M8 | In progress |
| M10 | Done (M10.1–M10.5). Host stats use purego Mach calls (D11). The `contr statusline` command line comes in M11. |
| M6 | Done (M6.1). The panel toggle sits right of split, as DESIGN.md orders them. |
| M9 | In progress |
| M11 | Done (M11). The usage text has no `dev` line (D5). The traffic-light inset is not set: MyGo v0.7.3 has no option for it. |
| M12 | Done (M12). `go build` replaces cargo in `bundle.sh` (D17). `scripts/check.sh` runs both script tests in temp dirs. |
| M13 | Not started |
