# contr

A minimal native client for [herdr](https://herdr.dev), in Rust and [GPUI](https://www.gpui.rs), with the herdr-agents board built in.

The terminal stays for commands and the coordinator agent. Everything the herdr-agents mod draws in the terminal today, the sidebar of workers, what needs you and the answer box, becomes native UI around it:

- **Left:** spaces, repos and worktrees, and the running agents, each with a status icon.
- **Middle:** the herdr terminal of the selected agent, with its tabs in the title bar.
- **Right:** the board: one row per issue or PR with its plan, build, review, CI and you stages, what needs you, and a box to answer it.
- **Bottom:** plan usage (Claude, Codex), CPU and memory.

macOS first; Linux and Windows later if needed. Architecture: a GPUI app that paints the herdr daemon's terminal cells itself, without wrapping the TUI.

Design: [docs/DESIGN.md](docs/DESIGN.md). Work plan: the tracker issue and the v0.1 milestone.

## Install (macOS)

`scripts/bundle.sh` builds `contr.app` with a release build. It needs the Rust toolchain from `rust-toolchain.toml`. The app is signed ad hoc. It has no Developer ID signature and is not notarized yet.

```sh
scripts/bundle.sh   # builds target/bundle/contr.app
scripts/install.sh  # installs it
```

`scripts/install.sh` puts these items in these places:

| Item | Place |
|---|---|
| The app | `/Applications/contr.app` |
| The CLI link | `~/.local/bin/contr`, a symbolic link to `/Applications/contr.app/Contents/MacOS/contr` |

The script replaces an earlier build of contr. It stops, and changes nothing, when `/Applications/contr.app` or `~/.local/bin/contr` is something else. One example is an older `Contr.app` with another bundle identifier. To replace such an item, run `scripts/install.sh --replace`.

Other options: `--app PATH` installs a bundle that you already have, `--dest DIR` changes the app folder, `--bin-dir DIR` changes the link folder, and `--no-link` skips the link. If `~/.local/bin` is not in your `PATH`, the script prints a hint.

### Use the CLI

`contr statusline` works from any shell that has `~/.local/bin` in `PATH`. For the `statusLine` setting of Claude Code, use the link path, `~/.local/bin/contr statusline`. That command does not need `PATH`.

### Install a CI build

The `Bundle` workflow builds the app on macOS 15 (arm64). It uploads the artifact `contr-macos-arm64`, which holds one zip file. The workflow runs on pull requests that change the bundle files, and on manual runs. The artifact is kept for 14 days.

1. Download the artifact from the workflow run page and unzip it.
2. Unzip the file `contr-<version>-macos-arm64.zip` that it holds: `ditto -x -k contr-<version>-macos-arm64.zip .`
3. Install the app: `scripts/install.sh --app path/to/contr.app`

A downloaded app has the quarantine flag. macOS blocks its first start. Open System Settings, go to Privacy & Security, and choose "Open Anyway". Or remove the flag: `xattr -dr com.apple.quarantine /Applications/contr.app`. An app that you build on your Mac has no quarantine flag.

### Finder and the Dock

An app that starts from Finder or the Dock gets the environment of the login session. It does not get the variables that you set only in a shell. `HERDR_SOCKET_PATH` and `XDG_CONFIG_HOME` are two examples. Without them, contr uses `~/.config/herdr/herdr.sock`. To use another socket when you start contr from Finder or the Dock, set the variable for the login session, for example with `launchctl setenv`.

### Uninstall

```sh
rm -rf /Applications/contr.app
rm -f ~/.local/bin/contr
```
