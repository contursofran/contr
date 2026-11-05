# contr UI spec (v0.3, from-zero redesign)

Sources: the owner's from-zero canvas (2026-11-04) and the design system
page (https://claude.ai/artifact/45Kp6p9DoVg3iEMx9Qh7QH). The design system
page draws each component in both themes. These specs hold the same
values, so a builder does not need the page.

Where a spec covers a part, the spec is the source of truth and replaces
`docs/DESIGN.md` v0.2 for that part (D118). For a part that no spec covers
yet, `docs/DESIGN.md` stays the source. "Open points" at the end lists
what the redesign does not settle.

## Files

| Spec | Part |
|---|---|
| [tokens.md](tokens.md) | every token, its dark and light value, and its use |
| [Window.md](Window.md) | glass window, title bar row, three panels |
| [Panel.md](Panel.md) | rounded area with a head and a footer |
| [TitleBar.md](TitleBar.md) | traffic lights, tabs, diff summary, icon buttons |
| [Tab.md](Tab.md) | one hc terminal of the selected agent |
| [IconButton.md](IconButton.md) | square button with one line icon |
| [DiffSummary.md](DiffSummary.md) | changed lines and files of the project |
| [StatusGlyph.md](StatusGlyph.md) | braille spinner and Claude glyph |
| [ServerRow.md](ServerRow.md) | one machine that runs hc |
| [ProjectRow.md](ProjectRow.md) | one project in the sidebar |
| [AgentRow.md](AgentRow.md) | one orchestrator or worker |
| [Terminal.md](Terminal.md) | the hc terminal of the selected tab |
| [QuestionsNotch.md](QuestionsNotch.md) | glass tab at the top of each terminal of the project |
| [QuestionsCard.md](QuestionsCard.md) | the open notch: one question, its choices, a free answer |
| [Outline.md](Outline.md) | message ticks on the right edge of the orchestrator terminal |
| [OutlineCard.md](OutlineCard.md) | the card of a hovered tick |
| [FileTree.md](FileTree.md) | the files panel tree with +/− counts |
| [UsageMeter.md](UsageMeter.md) | context and plan usage under the file tree |

Each component spec has these sections: purpose, anatomy, states,
behavior, data, keys and focus, edge cases, accessibility, build in MyGo,
tests, and what it replaces.

## Window

```
┌ glass window ─────────────────────────────────────────────────────────────────┐
│ ● ● ●                    │ tab · tab · +      ··········  +120 −126 9 files ⧉ ⌕ ▣ │  title bar 46
│┌ sidebar 312 ─────┐┌ terminal (fills) ─────────────────────┐┌ files 280 ──────┐│
││ Servers        + ││          ╭ 2 questions ╮              ││ Files        ⧩  ││
││ ▣ This Mac local ││                                       ││ ▸ internal      ││
││                  ││                                   ── ││   app.go  +4 −1 ││
││ Projects       + ││                                   ── ││                 ││
││ ▢ contr     main ││                                    3 ││                 ││
││  ✱ orchestrator  ││                                   ── ││                 ││
││  │ ⠋ worker      ││ output                            ── ││ Context  45%    ││
││  │ ⠶ worker      ││ prompt                                ││ ▬▬▬▬▬▬▬──────── ││
│└──────────────────┘└───────────────────────────────────────┘└─────────────────┘│
└───────────────────────────────────────────────────────────────────────────────┘
```

- One glass sheet: the title bar row, then three panels in a row.
- No top bar, bottom row or footer: the title bar takes the tabs, the
  notch takes the questions, the files panel takes the usage.
- Overlays (notch, card, outline, menus) float over the terminal. They
  never resize it (D67).

## Themes

- Two themes now: **Dark** and **Light**. The setting is **System**
  (follows macOS), **Dark** or **Light**; the default is System (D119).
- Each theme gives a value to every token name in [tokens.md](tokens.md).
  Views use token names only, never a color value. A new theme adds values
  for the same names; no view changes.
- `internal/app/theme` holds one palette per theme. The view reads the
  palette of the current theme.
- Apply the setting with `mygo.Theme.SetSource(mygo.ThemeSystem |
  ThemeDark | ThemeLight)`. Read the result with `mygo.Theme.IsDark()`.
  `mygo.Theme.OnUpdated(...)` tells when macOS or the setting changes it;
  then repaint through `Window.Update`.
- Copy the palette into MyGo's `ui.Theme` (Background, Surface,
  SurfaceHover, SurfacePressed, Border, Text, TextMuted and the others)
  with `Context.SetTheme`, so MyGo's own widgets match.
- `mygo.Theme.Preferences()`:
  - **ReduceMotion:** every StatusGlyph is still; no enter motions.
  - **ReduceTransparency:** glass becomes a solid fill (open point O13).
  - **HighContrast:** open point O13.
- The terminal has a 16-color ANSI palette per theme. The dark palette is
  today's `TerminalPalette`; the light palette is open point O9.
- Settings has an **Appearance** row: System, Dark, Light. The value is
  stored with the other settings, in `settings.json` in the user config
  folder (`internal/prefs`). An unknown stored value is System.

## Type

- Two families: **sans** for interface text, **mono** for values, ids,
  counts, keys and the terminal. This replaces "mono font everywhere"
  (D118).
- Sans: Inter, else the system font (open point O7). Mono: JetBrains Mono,
  in `assets` today.
- Styles (size, line height, weight) are in [tokens.md](tokens.md). Use the
  style name, not a size.

## Color

- Greys carry the hierarchy. They are inks: white at an opacity on dark,
  black at an opacity on light, matched so each ink has the same contrast
  in both themes.
- `ink-100` marks what is selected or what needs the owner.
- Two hues only: `blocked` (red) for an agent that needs the owner, and
  `claude` for the Claude glyph. Diff counts are greys. Terminal output
  keeps its own colors. This replaces D76 for the parts these specs cover.

## Icons

- 16px line drawings, stroke 1.2 to 1.4, drawn in the text color
  (`ui.Icon`), so a state changes only the text color.
- Names: `project`, `computer`, `server`, `folder`, `folder-open`,
  `file-go`, `file-md`, `file`, `close`, `plus`, `split`, `search`,
  `sidebar-right`, `filter`, `terminal`. They go in `assets`.
- The Claude mark is open point O8.

## Data

- hc I/O, git, file reads and usage reads run on goroutines. Results reach
  the view through `Window.Update` or `Invalidate`.
- No polling: read again on an hc event, a file watch event
  (`mygo.WatchFiles`) or a user action.
- Timers only for motion on screen: a StatusGlyph that works or waits.
  Idle and needs-you glyphs use no timer.

## Keys

| Key | Does | Where |
|---|---|---|
| `⌘T` | new tab: a plain terminal in the selected project (D82) | window |
| `⌘W` | close the selected tab when it is a plain terminal (D85) | window |
| `⌘D` | split terminal (open point O12) | window |
| `⌘F` | search in the terminal (open point O12) | window |
| `⌘⇧E` | show or hide the files panel | window |
| `⌘J` | open or close the questions card | window, while a question is open |
| `1`–`9` | pick that choice | questions card, with the focus |
| `⏎` | send the typed answer, else the picked choice; go to the next question | questions card |
| `⇥` | next question chip | questions card |
| `↑` `↓` | move the selection | sidebar, file tree |
| `←` `→` | close, open a folder | file tree |
| `⏎` | focus the terminal of the selected agent | sidebar |
| Esc | close the card, menu or tooltip | only the part with the focus (D72) |

Esc is never a window shortcut: it interrupts Claude Code in the terminal.

## Focus

- The focus ring is 1px `focus-ring`, 2px offset, on every control.
- Overlays over the terminal (notch, outline) take no keys unless they
  have the focus. A click on the terminal gives the focus back to it.

## Accessibility

- Each control has a screen-reader label. Toggle buttons report
  pressed. A StatusGlyph says its state: "working", "waiting", "idle",
  "needs you".
- Hit areas are at least 20×10 px, even for a 2px tick.

## Tests

- Each spec lists its view tests (`ui.NewTester`: build, click by label,
  send keys, read texts) and one e2e path (`//go:build e2e`).
- A visible change: capture the window in **both** themes and look at the
  PNGs (D5).

## What the redesign replaces

| v0.2 part (`docs/DESIGN.md`) | v0.3 part | Status |
|---|---|---|
| Top bar 40: tabs of the open project | TitleBar 46: tabs of the selected agent | replaced |
| Sidebar: Spaces, `This computer` header | ServerRow section, then ProjectRow | replaced |
| Sidebar: Agents section in state groups | AgentRow under its project: orchestrator first, workers on a rail | replaced |
| Panel 380: board or worker | Files panel 280: FileTree and UsageMeter | replaced; board is O1 |
| Bottom row 32: needs-you tabs and counts | QuestionsNotch | questions replaced; to-dos and blocked workers are O2 |
| Answer card | QuestionsCard | replaced |
| Context sheet (`⌘J`) | QuestionsCard context line | O3 |
| Footer 26: plan usage | UsageMeter | replaced; CPU, memory, versions and messages are O4 |
| Pin bar (removed) | Outline and OutlineCard | new |
| Board pop-up, PR viewer, Review, projects modal, settings | unchanged; restyle with the tokens | O5 |

## Open points

The coordinator asks the owner these before the issues that need them.

1. **O1 Board.** The right panel shows files now. Where do the board
   (needs a look, work, next, merged) and the worker detail (task, PR,
   CI, review, branch) go?
2. **O2 To-dos and blocked workers.** The notch shows questions. Where do
   to-dos (`do`, `know`) and a blocked worker's menu (D51) show?
3. **O3 Context sheet.** Keep `⌘J` for a sheet with the source, refs and
   option impacts (D114), or put them in the card?
4. **O4 Footer.** Where do CPU, memory, the contr and hc versions, and
   action messages go?
5. **O5 PR viewer and Review.** Their place in the new frame.
6. **O6 Narrow window.** Which panel hides first below which width.
7. **O7 Sans font.** Bundle Inter (OFL, a `NOTICE` entry) or use the
   system font.
8. **O8 Claude mark.** Which drawing, and the right to use it.
9. **O9 Light terminal palette.** The 16 ANSI colors on the light panel.
10. **O10 Ages.** The design system page moves ages once a minute while a
    row shows. D35 and D40 forbid a timer. Until the owner decides, ages
    follow D35.
11. **O11 Servers.** Remote servers need a source (#149). Only this Mac
    shows now.
12. **O12 Split and search.** What split means for an hc terminal, and
    whether search uses MyGo's `FindBar` over the terminal text.
13. **O13 Reduce transparency and high contrast.** The fill and ink values
    for each.
