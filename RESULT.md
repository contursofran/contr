# #376 Agent rows: CI line and arrow keys

- PR: see below (draft into main). Both parts are in one PR (about 300 lines of code, rest tests and docs).
- Part 1 (CI): source is the worker's PR checks. `WorkerPRs` (D104) already runs `gh pr view` on an agents sync; it now reads `statusCheckRollup` too (`github.Client.BranchState`) and keeps the unfinished-check count. A live, not-working worker with pending checks shows "CI" on line 2 after the model, and ", waiting on CI" in the label. Pending answers hold 1 min, not 5; no timer.
- Part 2 (keys): Up, Down, Enter on the sidebar list, in the icon column too. Enter focuses the terminal.
- D145 records both. Specs AgentRow and ProjectRow updated.
- PNGs (Dark, Light, after a key move): tmp/e2e/agentrows-dark-keys.png, tmp/e2e/agentrows-light-keys.png (under the worktree).

## Deviations and questions for the owner
- ui.List's own `Selected` keys paint the row in the accent color, so the keys use `Handle.OnShortcut` instead.
- A click on a row gives the terminal the focus (AutoFocus, as before), so the list keys need the list focused (Tab). A key-chosen terminal does not steal the focus. Question: should a click on a sidebar row leave the focus on the list? That would make Enter useful but would stop typing right after a click.
- ← fold and → open are not built (not in the issue).
- No unit test for the terminal-focus handoff (`keyNav`, `focusTerm`); the e2e covers Up and Down in the real window, Enter is not click-tested.
- The CI line in a screenshot is not captured: it needs a real PR with pending checks; it is covered by view tests.
