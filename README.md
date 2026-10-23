# contr

A minimal native client for [herdr](https://herdr.dev), in Rust and [GPUI](https://www.gpui.rs), with the herdr-agents board built in.

The terminal stays for commands and the coordinator agent. Everything the herdr-agents mod draws in the terminal today, the sidebar of workers, what needs you and the answer box, becomes native UI around it:

- **Left:** spaces, repos and worktrees, and the running agents, each with a status icon (Orca-style sidebar).
- **Middle:** the herdr terminal of the selected agent, with its tabs in the title bar.
- **Right:** the board: one row per issue or PR with its plan, build, review, CI and you stages, what needs you, and a box to answer it.
- **Bottom:** plan usage (Claude, OpenCode Go, Codex), CPU and memory.

macOS first; Linux and Windows later if needed. Architecture: a GPUI app that paints the herdr daemon's terminal cells itself, without wrapping the TUI.

Design: [docs/DESIGN.md](docs/DESIGN.md). Work plan: the tracker issue and the v0.1 milestone.
