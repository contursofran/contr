# contr (Go)

A minimal native client for herdr, with the herdr-agents board built in.
This is the Go port of the Rust and GPUI contr. It uses MyGo's native UI.
When it reaches parity, it replaces the Rust app.

- **Left:** spaces, repos and worktrees, and the running agents, each with a
  status icon.
- **Middle:** the herdr terminal of the selected agent, with its tabs in the
  title bar.
- **Right:** the board: one row per issue or PR with its stages, what needs
  you, and a box to answer it.
- **Bottom:** plan usage (Claude, Codex), CPU and memory.

State: milestone 0, an empty window. See `docs/PORT-PLAN.md`.

## Build and run

Go downloads the toolchain that `go.mod` names.

```sh
go run ./cmd/contr     # opens the window
scripts/check.sh       # gofmt, vet, tests with -race, build without cgo
```

## Docs

- `docs/DESIGN.md`: the UI
- `docs/PORT-PLAN.md`: milestones and their state
- `docs/DECISIONS.md`: decisions
- `docs/BENCHMARKS.md`: the measurements behind the move to Go
- `AGENTS.md`: rules for agents
