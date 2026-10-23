# Working on contr

- Rust 2024, GPUI. `cargo fmt`, `cargo clippy --all-targets -- -D warnings` and `cargo test` must pass before a PR.
- No `unwrap`/`expect` in non-test code; no `unsafe`.
- Never block the UI thread: herdr I/O and file reads run on background tasks and reach views as events.
- Small PRs: one issue, one PR, ideally under ~400 changed lines. Split anything bigger.
- UI follows docs/DESIGN.md exactly: mono font, greyscale, white only for what needs the owner.
- Study other implementations freely, but write our own code; do not copy code 1:1 from other projects.
- Do not name or link other GitHub users or repositories in code, docs, commits or PRs.
