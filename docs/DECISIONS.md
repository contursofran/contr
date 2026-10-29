# Decisions

Do not delete rows. Mark a row "Replaced by Dn" when a later decision
replaces it.

| ID | Date | Decision | Why |
|---|---|---|---|
| D1 | 2026-10-29 | Port contr from Rust and GPUI to Go with MyGo's native UI. The Go app replaces the Rust app when it reaches parity. | Measured in `docs/BENCHMARKS.md`. MyGo's runtime is close to GPUI (glyph atlas, partial repaint). Cold builds use about 5× less CPU, and parallel agents do not block each other. |
| D2 | 2026-10-29 | The reference is Rust contr at `fae6154`. Port its behavior and its test cases. Rust features merged after `fae6154` get a new port-plan row only when the owner asks. | One fixed target for parity. |
| D3 | 2026-10-29 | The module path is `github.com/contursofran/contr`. | The Go app can replace the Rust app in the same repository without renames. |
| D4 | 2026-10-29 | MyGo is pinned to v0.7.3. No cgo. Upgrade MyGo only in a slice of its own that runs the checks and the grid benchmark. | MyGo is young (first release 2026-09), so its API can change. |
| D5 | 2026-10-29 | Do not port the Rust dev control (`contr dev`, `dev_control`, `dev_protocol`). Agents drive and capture the app with MyGo's automation plugin under the `e2e` build tag. | The plugin lists elements, clicks, types and captures PNGs. That is the dev control's job, without about 3,900 lines of our own. |
| D6 | 2026-10-29 | The dev herdr server of this repository lives in `contr-go-dev-herdr` under the temporary folder, not in the Rust repository's `contr-dev-herdr`. | The Rust and Go repositories can each run a dev server at the same time. |
| D7 | 2026-10-29 | Rust code comments cite decisions of the Rust tracker (for example D10, D38). The port keeps the behavior that they describe and cites them as "Rust D10". | Those decisions stay valid. Their text is in the Rust issues and comments. |
| D8 | 2026-10-29 | No GitHub remote, issues, PRs or CI until parity. Slices are tracked outside the repository. Builders work on local branches in worktrees. A reviewer approves the exact head, and the orchestrator merges into local `main` after `scripts/check.sh` passes on the merged tree. After the owner compares the two apps at M13, the Go code moves into the `contursofran/contr` repository and CI starts there. `.github/workflows/ci.yml` stays for that move. | Owner's choice: keep the port local until it proves better than the Rust app. |
| D9 | 2026-10-29 | `state.Visibility` follows the window through MyGo's public `Window` events: `IsVisible`, `OnShow`, `OnHide`, `OnMinimize`, `OnRestore`. A window that other windows cover sends no public event. For that case MyGo itself holds frames while nothing of the window shows (`platform.OccludableSurface`, `windowDidChangeOcclusionState:`): the view function, `Draw` and the work that `After` drives wait until some of it shows. Background work starts from `Visibility` and from view-driven `After` timers, and runs no loop or timer of its own while `Visible()` is false. | "No work while the window is hidden" (PORT-PLAN M4). MyGo v0.7.3 has no public occlusion event. If a covered window must also stop host sampling, that needs a MyGo change in a slice of its own. |
