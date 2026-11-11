# Plan: setup checklist (#713)

Issue #713 (author: owner). Step 1 is this plan plus a design board. Step 2 builds it in the slices below.

## 1. Checks: what each one reads and how

Each check is one pure function: `Check(ctx, env) Result{ID, State, Detail, Action}`. State is `ready`, `missing`, `action` (needs action) or `optional`. All checks run on a goroutine. Results reach the view through `Window.Update` (AGENTS.md: never block the UI thread).

| ID | Required | Reads | Existing code |
|---|---|---|---|
| `hc` | yes | binary found; `hc --version` output (2 s timeout) | `hc.Find(path, home, dirs)` in `internal/hc/client.go` (PATH, then `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`) |
| `core` | yes | the frame's live connection state, not a new connection | the frame state behind `sidebar.ServerLabel` ("This Mac, local" / "This Mac, offline") |
| `plugin` | yes | `claude plugin list` (JSON output if the CLI has it, else text); plugin `contr` (from `kit/.claude-plugin/plugin.json` `"name": "contr"`) is listed and enabled; version compared with the kit version | nothing yet; new runner like `github.Find` for `claude` |
| `meter` | yes | `usage.ReadClaude(dir)` returns non-nil and `ReadAt` is less than 24 h old (open question Q4) | `internal/usage/claude.go` `ReadClaude`, `ClaudeFile`; the hint text in `scripts/install.sh` `print_hint` |
| `gh` | yes | `gh` found; `gh auth status` exit code | `github.Find(home, dirs)` and the runner in `internal/github/github.go` |
| `codex` | no | `codex` found; Codex usage readable | `internal/usage/codex.go` (`ReadAt` from the rollout), `codexapp.go` |
| `contr` (CLI link) | no | `contr` on PATH (the `statusline` command needs it) | `install.sh` `link_cli`, `print_hint` |

> [!UNVERIFIED]
> I did not check whether `claude plugin list` has a JSON flag, or `hc --version` output format. Slice S1's builder confirms both with `--help` and writes fakes from it. No check reads `~/.claude` or `~/.codex` files directly.

## 2. Actions: run vs copy (owner choices, see Q1–Q3)

| Check | Action | Recommendation | Risk |
|---|---|---|---|
| `hc` missing | Copy the install command | Copy only | Install scripts from the net run with the owner's rights; contr cannot verify them |
| `core` offline | "Start core" button: `hc server start` | Run (contr already starts `hc connect`) | Low. Never stop or restart the core |
| `plugin` missing | "Install": `claude plugin marketplace add …` + `claude plugin install contr` | Run, through Claude's own CLI, after a confirm | Medium: Claude's CLI writes its own settings; contr writes nothing itself |
| `plugin` disabled | "Enable": `claude plugin enable contr` | Run | Low |
| `meter` | Show the `statusLine` line to copy, with a Copy button | Copy only (keeps D-rule "contr does not edit Claude Code's settings", see `print_hint`) | Writing `~/.claude/settings.json` can break the owner's status line or race Claude Code writes; JSON comments and key order get lost |
| `gh` signed out | Copy `gh auth login` | Copy only (interactive, needs a terminal) | none |
| `codex` | Copy install hint | Copy only | none |

## 3. Where it lives

- A fourth Settings tab, `Setup`, first in `SettingsTabs` (Q5): `{"Setup", "General", "Agents", "Updates"}`.
- First launch: when a required check is not ready and the owner has not dismissed it, Settings opens on `Setup` once (store `setupSeen` in `internal/prefs`).
- Good to go: a header line in the tab, "Good to go" in `ink-88` when every required check is `ready`; else "N steps left" in `ink-100`. The Settings tab label gets the D146 dot while a required check is not ready (Q6).

## 4. Slices

### S1: read-only checklist in Settings (~350 lines)

- New package `internal/setup`: `Result`, `State`, the checks in §1 with injected runners (`Runner func(ctx, name string, args ...string) ([]byte, error)`, `Now`, `Home`) so tests use fakes.
- `internal/app/settings/settings_setup.go`: `setupTab(c)`; rows from `kit.Row`; `Recheck` button; runs on open and on `Activated()`.
- `settings.go`: add `TabSetup` to `SettingsTabs`.
- Tests: `internal/setup/setup_test.go` — one test per check per state, and error paths: binary missing, command times out, bad output, stale meter file, missing meter file. `settings_setup_test.go` (`ui.NewTester`): all ready shows "Good to go"; one missing shows "1 step left"; Recheck reruns. Each fails if the check or row is removed.
- Copy commands only in S1 (Copy button via MyGo clipboard).
- Docs: new `docs/spec/SettingsSetup.md` (format of `SettingsAgents.md`), `sheet_test.go` part, a D-row in `docs/DECISIONS.md`, update D186 row text pointer ("Replaced by Dn for the tab list").
- Verify: `scripts/check.sh`; `scripts/sheet.sh SettingsSetup`; `scripts/see.sh` new fixture `setup-missing`.

### S2: first launch opens Setup (~150 lines) — needs S1

- `internal/prefs`: `SetupSeen bool`. Frame: on start, after the first check pass, open Settings on `TabSetup` if a required check fails and `!SetupSeen`.
- Tests: prefs round trip; frame test opens once, not twice. e2e `TestSetupFirstLaunch` in `cmd/contr` with the harness.

### S3: actions contr runs (~250 lines) — needs S1, and Q1–Q3 decided

- Start core, Install / Enable plugin, each with a confirm, a spinner, the command's last stderr line on failure, then a recheck.
- Tests: fake runner success, failure (shows error text), timeout; buttons absent for copy-only rows.

### S4 (only if Q2 = write): meter set-up writes `statusLine`

- Backup file, atomic write with `internal/jsonfile`, chain the old command as `contr statusline -- <old>`. Separate PR, separate decision row.

| Slice | Dependency |
|---|---|
| S1 | independent |
| S2 | needs S1 merged |
| S3 | needs S1 merged |
| S4 | needs S3 merged |

## 5. Layout (for the design board)

Settings modal, tab `Setup` selected.

- Header: title "Setup" (sans, `ink-88`); right: status "Good to go" (`ink-88`) or "2 steps left" (`ink-100`); `Recheck` icon button (`hover-strong` on hover).
- Section "Required", then "Optional" (labels `ink-48`), rows separated by `hairline`.
- Row: state glyph left (ready: check mark `ink-48`; missing / action: dot `blocked`; optional missing: ring `ink-30`); name (`ink-88`); detail line 2 in mono `ink-48` (e.g. "hc 0.9.2 · core running", "not signed in", "last data 3 days ago"); right: one button ("Install", "Enable", "Start", "Copy").
- Copy row opens below: a mono code line on `hover` fill, `input-line` 1px border, `radius-12`, with Copy at the right; "Copied" for 1.5 s.
- Running action: button turns into a spinner; error shows as line 2 in `blocked-text`.
- Ready rows have no button. Focus ring `focus-ring` on each button.

## Open questions

1. Q1 plugin install: (a) run `claude plugin …` after a confirm, (b) copy only. Recommend (a).
2. Q2 meter: (a) copy only, (b) contr writes `statusLine` with a backup. Recommend (a); it keeps "contr does not edit Claude Code's settings".
3. Q3 hc install: (a) copy only, (b) run the installer. Recommend (a).
4. Q4 meter fresh: data younger than (a) 24 h, (b) 1 h, (c) any file. Recommend (a).
5. Q5 tab place: (a) first, (b) last. Recommend (a).
6. Q6 dot on Settings while a required check fails: (a) yes, (b) no. Recommend (a).
7. Q7 `gh` required or optional? Recommend required (PR panes need it).
