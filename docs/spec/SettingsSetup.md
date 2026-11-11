# SettingsSetup

The Setup tab of the Settings modal (D278): a read-only checklist of what contr needs on this
Mac. It is the first tab. The tab bar is in [SettingsAgents.md](SettingsAgents.md).

Shared rules: [README.md](README.md).

## Anatomy

1. Head: `Setup` (`ink-88`), then the status: `Good to go` (`ink-88`) when every required check
   is ready, else `N steps left` (`ink-100`; `1 step left` for one); `Checking…` (`ink-48`)
   while a pass runs. Then `Recheck` (outlined button; dim while a pass runs).
2. `Required` (`ink-48`), then its rows; `Optional` (`ink-48`), then its rows.
3. Row, a `hairline` above: the state glyph (12px box), the name (`ink-88`) over the detail in
   mono (`value` style, `ink-48`, one line with an ellipsis).
4. A row with a step has a copy line under it, in line with the name: the command in mono
   (`ink-88`) on `hover`, 1px `input-line`, card radius, and `Copy` (plain button) at the
   right. After a click it says `Copied` for 1.5 s.
5. Foot, `hairline` above: `Done` (plain button).

| Glyph | State |
|---|---|
| `todo-done` icon, `ink-48` | ready |
| 8px dot, `blocked` | missing, or a step for the owner |
| 8px ring, 1px `ink-30` | optional part not there |

## Checks

| Row | Id | Required | Ready when | Copy line |
|---|---|---|---|---|
| hc | `hc` | yes | `hc --version` prints `hc <version>` | install text (hc missing) |
| hc core | `core` | yes | the frame follows a core (its live state; no new connection) | none |
| contr plugin | `plugin` | yes | `claude plugin list --json` lists `contr` enabled | `claude plugin enable contr@contr` or `claude plugin install contr@contr` |
| Claude meter | `meter` | yes | the meter data that `contr statusline` writes is under 24 h old | the `statusLine` line |
| gh | `gh` | yes | `gh auth status` ends with no error | `gh auth login`, or `brew install gh` |
| codex | `codex` | no | `codex` is found | `brew install codex` |
| contr command | `contr` | no | `contr` is found | `scripts/install.sh` |

## Behavior

- The checks run off the UI thread, each command with a 5 s limit, when Settings opens, on
  `Recheck`, and when the app becomes active while the tab shows. They also run once at start,
  after the window shows, so the dot shows without opening Settings (D280). Nothing else runs
  while Settings is closed, and no timer runs them again.
- First launch (D280): when that start pass finds a required check not ready, the hc core not
  counted (it can still be connecting), and `setupSeen` is not set in the settings file,
  Settings opens on the `Setup` tab and `setupSeen` is stored. It never opens by itself again.
- A command that times out shows `timed out`; output in another format shows
  `unexpected … output`.
- No check reads files in Claude Code's or Codex's folders. contr runs no command for the owner
  here and does not edit Claude Code's settings.
- While a required check is not ready, the `Setup` tab and the Settings button of the sidebar
  have the 6px `ink-100` dot (D146); the button is named `Settings, setup steps left`.
