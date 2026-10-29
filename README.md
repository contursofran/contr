# contr

A minimal native macOS client for herdr, with the herdr-agents board built
in. It is written in Go and uses MyGo's native UI.

- **Left:** spaces, repos and worktrees, and the running agents, each with a
  status icon.
- **Middle:** the herdr terminal of the selected agent, with its tabs in the
  title bar.
- **Right:** the board: one row per issue or PR with its stages, what needs
  you, and a box to answer it.
- **Bottom:** plan usage (Claude, Codex), CPU and memory.

## Requirements

- macOS on Apple silicon (arm64).
- herdr, installed and running.

## Install

```sh
scripts/bundle.sh      # builds dist/bundle/contr.app
scripts/install.sh     # installs to /Applications and links `contr` in ~/.local/bin
```

## Build and check

Go downloads the toolchain that `go.mod` names.

```sh
go run ./cmd/contr     # opens the window
scripts/check.sh       # gofmt, vet, tests with -race, build without cgo
```

The Rust and GPUI app is at tag `rust-final`.

## Docs

- `docs/DESIGN.md`: the UI
- `docs/DECISIONS.md`: decisions
- `docs/PORT-PLAN.md`: how the Go port was built
- `docs/BENCHMARKS.md`: the measurements behind the move to Go
- `AGENTS.md`: rules for agents
