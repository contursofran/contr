# Working on contr (Go)

contr is a native macOS client for herdr, with the herdr-agents board built
in. This repository is the Go port of the Rust contr. When the port reaches
parity, it replaces the Rust app in the `contursofran/contr` repository.

## Goal and reference

- Goal: feature parity with the Rust contr at commit `fae6154` (2026-10-24).
  `docs/PORT-PLAN.md` lists the milestones in order and their state.
- The Rust code is the reference, and its tests are the spec. Port the
  behavior and the test cases. Do not copy the Rust structure where Go or
  MyGo has a simpler form.
- Read the Rust code in the read-only export that your task brief names, or
  with `git show fae6154:<path>` in a Rust contr checkout. Do not change the
  Rust repository.
- `docs/DESIGN.md` is the source of truth for the UI.
- `docs/DECISIONS.md` records why things are the way they are.

## Stack

- Go 1.27.3, set in `go.mod`. The `go` command downloads this toolchain by
  itself. Do not install Go or other tools.
- MyGo v0.7.3, native UI (package `ui`), pinned. Upgrade MyGo only in a slice
  of its own.
- MyGo docs are in the module cache:
  `$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)/docs/`. Start with
  `ui/README.md`, `architecture.md`, `ui/testing.md`, `ui/performance.md`,
  `ui/drawing.md` and `ui/custom-widgets.md`. The examples are in
  `examples/` next to `docs/`. When the docs are not clear, the MyGo source
  is the final answer.
- No cgo: do not write `import "C"`. Native calls go through MyGo.

## Layout

| Path | Holds |
|---|---|
| `cmd/contr` | `main` and the command line |
| `internal/app` | the window content: frame and views |
| `internal/herdr` | herdr's JSON API: wire, model, reader, command, tabs, agents |
| `internal/herdr/endpoint` | herdr's binary endpoint socket: codec, messages, surface, conn, link, osc52 |
| `internal/usage` | plan usage: Claude status line, Codex logs |
| `assets` | fonts, status icons, app icon |
| `scripts` | `check.sh`, `dev-herdr.sh` |

`docs/PORT-PLAN.md` names the package for each Rust module. Add packages
under `internal/` when a milestone needs them.

## Rules

- `scripts/check.sh` must pass before you report done: gofmt, `go vet`,
  `go test -race`, and a build with `CGO_ENABLED=0`.
- Never block the UI thread. MyGo runs views and handlers on the main
  thread. herdr I/O, file reads and git scans run on goroutines, and their
  results reach the views through `Window.Update` or `Invalidate`.
- Performance is a requirement: no polling, no timers in the hot path, and
  no work while the window is hidden. Read MyGo's `ui/performance.md` before
  you add a loop, a timer, an animation or a goroutine that runs forever.
- No `panic` in non-test code: return errors. Do not drop errors silently:
  log them or show them.
- The UI follows `docs/DESIGN.md` exactly: mono font, greyscale, and white
  only for what needs the owner.
- Small slices: one slice, one branch, ideally under about 400 changed
  lines. Test-heavy slices stay whole.
- Adapted code: `NOTICE` credits the projects that code comes from. When you
  port a Rust file that starts with `// Adapted from Herdr UI (Apache-2.0);
  changes: …`, keep that comment in the Go file and add "ported to Go" to
  the changes. Endpoint code keeps its note "Written from herdr's wire
  layout; see NOTICE." To adapt code from MyGo's examples or plugins (MIT),
  add the same kind of header comment and a `NOTICE` entry.
- Do not name or link other GitHub users or repositories anywhere except
  `NOTICE`, and do not @-mention people. Import paths and `go.mod` are
  exempt.
- Write no local paths (such as `/Users/...`) in the repository or in
  commit messages. The code moves to a public repository later.
- Docs, comments and commit messages: concise ASD-STE100 Simplified
  Technical English.

## herdr safety

- Never run the herdr command line against the owner's herdr, not even
  `--help` or `--version`. Never connect to the owner's herdr sockets or
  send input to them.
- For a live herdr, use only `scripts/dev-herdr.sh`. It starts an isolated
  server with its own home and sockets: `eval "$(scripts/dev-herdr.sh)"`,
  then `scripts/dev-herdr.sh stop` when you are done.
- Tests use fakes: fake servers on sockets in temporary folders, as the Rust
  `endpoint/testing.rs` does. No test touches the owner's herdr, `~/.claude`,
  `~/.codex` or a real repository.
- Do not read `~/.claude` or `~/.codex`. Do not install the app.

## Testing

- Unit and view tests run with `go test`. Test a view with `ui.NewTester`:
  it builds the view without a window, clicks by label, sends keys and
  reads texts.
- Port each Rust test case for the behavior that you port. When a Go test is
  a direct port, give it the Rust test's name in Go style, so that parity
  can be checked.
- End-to-end tests use MyGo's automation plugin, in files with
  `//go:build e2e`. The plugin lists elements, clicks, types and captures a
  window as a PNG. To check a visible change, capture the window and look at
  the PNG yourself. This replaces the Rust dev control (decision D5).
- Run the app only against `scripts/dev-herdr.sh` and a scratch project under
  `/tmp`, never against the owner's herdr.
- On the owner's Mac, run one heavy job at a time: an app run with dev herdr,
  an e2e run or a release build. `go build` and `go test` are light and can
  run in parallel.

## Workflow (local, decision D8)

This repository has no GitHub remote, no issues, no PRs and no CI until the
port reaches parity. Work tracking, review and merges are local.

- Your brief names a slice ID (for example `M2.1`), a worktree and a branch
  `agent/<slice>-<slug>`. Work only there. Do not edit the main checkout.
  Temporary files go in `tmp/`, which git ignores.
- Commit on your branch with clear messages. No `Co-Authored-By`, AI
  attribution or generated-by lines. Do not rewrite commits that a reviewer
  has seen.
- Before you report done, merge local `main` into your branch if it moved,
  and run `scripts/check.sh` on the final head. `scripts/check.sh` is the
  gate in place of CI.
- Report the branch, the head SHA, the `scripts/check.sh` result and any
  deviation from the brief, in RESULT.md when the brief asks for it.
- Do not merge into `main`. The orchestrator merges after a reviewer
  APPROVE at the exact head and a passing `scripts/check.sh` on the merged
  tree.
- When your change makes a doc wrong (`docs/DECISIONS.md`,
  `docs/DESIGN.md`), update it on your branch. A new decision gets a new row
  in `docs/DECISIONS.md`. Do not delete old rows; mark them "Replaced". The
  orchestrator keeps the state table in `docs/PORT-PLAN.md`; do not edit it.
