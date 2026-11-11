# SettingsUpdates

The Updates tab of the Settings modal: the versions of contr and hc, contr's update status, and
the herdr-core block (D277). The tab bar is in [SettingsAgents.md](SettingsAgents.md).

## Anatomy

- `Versions` (`ink-48`), then `contr <version>` (`ink-88`).
- The hc row: `hc <version>` (`ink-88`, grows) and the outlined button `Update hc` on its right.
  The button shows only when contr can run hc. Under the row, while an update runs, the line
  `updating hc…` (`ink-64`); after it, the result (`ink-64`) or the error (`blocked`).
- `Updates` (`ink-48`), contr's status line, the notes, and the foot buttons `Check for updates`,
  `Install and restart` or `Restart now` (D176), `Done`.

## Behavior

- A click on `Update hc` opens a confirm (MyGo `AlertDialog`): title `Update hc?`, body
  `hc downloads the newest version and restarts the core. Running terminals keep running.`,
  buttons `Cancel` and `Update`. `Cancel` and Escape close it and run nothing.
- `Update` runs `hc update --json` off the UI thread, with a 5 minute limit. The button is
  dim while it runs. contr never runs it without the confirm.
- hc has no check flag, so the tab does not tell of a newer hc before the update.
- `hc update` puts the new hc in place and restarts the core by itself; the terminals keep
  running. The texts after it:
  - `hc <new> installed: the core restarted on it, terminals kept running`
  - `hc <new> installed: restart the core to use it` (hc did not restart the core)
  - `hc is up to date`
  - `hc update failed: <hc's message>`, also as a toast; a time limit gives
    `hc update failed: no answer after 5m0s`.
- contr has no button that restarts the core.
