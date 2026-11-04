# contr design (v0.2)

Source of truth: the owner's claude.ai design canvas "sidebar mockup".

- Page **Native · final**: artboards 1 (orchestrator tab), 2 (worker tab),
  3 (orchestrator quiet), 4 (needs a look), 5 (worker with failed CI),
  6 and 7 (narrow window), and the notes "Rules on these screens" and
  "Needs new data before building".
- Artboards **5 · Projects modal** and **SideFinal** for the projects modal.
- Artboards **O · PR viewer** (PrView, PrViewFiles) for the PR viewer.
- The answer card and the context sheet did not change on Native · final
  ("unchanged"), so they come from screens 1 and 2 of the archived "Final"
  page. Other archived screens and mockup G (v0.1) are not a source.

The Native · final screens draw the reference app's own look: a proportional
font, coloured project tiles, blue links and a blue focus border. contr
keeps its own look (tokens below) and takes from these screens the layout,
the parts, the texts, the behaviour and the status colours of the rules
note (D76).

**(not built)** marks a part that the code does not have yet. Everything
else is in the app today. "Open points" at the end lists what v0.2 does not
settle.

## Window (1440×900 reference)

```
┌ sidebar 280 ──┬ top bar 40: ◆ orch · worker tabs · shell ········· [Review] [▣] ┐
│ ● ● ●   contr │┌ terminal card ─────────────────────────┐┌ panel 380 ────────────┐│
│ Spaces  + new ││ header 34: icon · program · agent · cwd ││ follows the top tab:  ││
│ This computer ││                                         ││ orchestrator → board  ││
│  ▸ project    ││ output                                  ││ worker → that worker  ││
│    worktrees  ││                                         ││                       ││
│               ││   card, sheet, pop-ups float over here  ││                       ││
│ Agents        ││ prompt                                  ││                       ││
│ Orchestrators │└─────────────────────────────────────────┘└───────────────────────┘│
│ Blocked 1     │ bottom row 32: needs-you tabs ·········· ctx · ●3 ◷0 !0 · board ▴ ⌘J│
│ Working 3     │                                                                    │
├───────────────┴ footer 26: claude ▬ 38% · codex ▬ 12% · CPU MEM ··· status ────────┤
```

- The sidebar spans the full height, title-bar area included.
- Top bar 40, then the content row, then the bottom row 32, then the footer
  26. The bottom row and the footer keep their heights at all times (D67).
- The content row holds the terminal card and the panel. Each is a card with
  an 8 margin (terminal: top and left; panel: top, left and right), radius
  10, border `line` **(not built)**.
- The terminal size changes only with the window, the panel toggle, the
  narrow-window rule and the PR viewer pane. The card, the context sheet,
  the board pop-up, the narrow panel and popovers are overlays over the
  terminal; they never resize it (D67).
- Panel width 380 (v0.1: 440) **(not built)**. The panel toggle in the top
  bar hides it; hidden, the terminal takes the width.
- The sidebar and the panel widths can be dragged at the 1 px divider line
  (resize cursor on hover). The start values are the default widths above
  (sidebar 280, panel 440 as built). Limits: sidebar 200–480, panel 280–720.
  A hidden side keeps its width for when it shows again. The widths are kept
  across launches (D111).
- Below 1300 px window width the panel hides by itself (D77)
  **(not built)**.

## Tokens

| Token | Value | Use |
|---|---|---|
| font | JetBrains Mono 12/18 (terminal 13/20) | everything |
| font.small | 11/17; 10 for stage column heads; 9 uppercase, 0.08em spacing for role tags | meta, second lines |
| bg.window | #141414 | window, panel card |
| bg.sidebar / bars | #101010 | sidebar, top bar, bottom row, footer |
| bg.terminal | #121212 | terminal card |
| bg.card | #1C1C1C + 1px #2C2C2C inset, radius 7 | selected row, selected tab |
| bg.hover | #1C1C1C | row hover |
| bg.answer | #1A1A1A, border #262626, radius 10 | answer card |
| bg.overlay | #161616, border #2E2E2E, radius 12 | modal, context sheet |
| bg.popup | #181818, border #3A3A3A, radius 10, shadow 0 16 48 rgba(0,0,0,.55) | board pop-up, narrow panel |
| scrim | rgba(8,8,8,.72) | behind the projects modal |
| line | #222222 | dividers, card borders |
| line.strong | #3A3A3A | strip base line, pop-up borders, focus border of the terminal card |
| text | #EDEDED | primary |
| text.2 | #BDBDBD | secondary |
| text.dim | #7A7A7A / #6A6A6A | labels, meta |
| text.faint | #4A4A4A | not-started dots, empty values |
| you | #EDEDED bg + #111111 bold text | anything waiting on the owner (badges, "you" column) |
| status.failed | #F07178 | failed CI, blocked dots, "Blocked N" label (D76) |
| status.attention | #E5C07B | working dot, "Working N" label, "no output" after 5 min, overlap warnings, review blockers (D76) |
| status.attention.line | #5A4A2A | border of the Interrupt button |
| status.failed.box | bg #22191B, border #4A2F33 | the CI failed box |
| diff.added | #A6D189 | added lines and + counts (D34) |
| diff.deleted | #F07178 | deleted lines and − counts (D34) |

Buttons (all 12 px, radius 6):

| Kind | Style |
|---|---|
| primary | `you` bg, #111111 600 text, padding 5×12 |
| secondary | text `text`, 1px #333333 border, padding 4×11 |
| ghost | text #8A8A8A, no border; `text` on hover |
| icon | 28×28, radius 6, #8A8A8A; hover bg #1E1E1E |
| key | secondary with an 18×18 key cap (radius 4, bg #2A2A2A, 700 11 px) before the label, padding 3×10×3×4; the first option uses the primary colours with a #BDBDBD key cap |

Hues (D76): white means the owner acts. Red (`status.failed`) means failed
or blocked. Amber (`status.attention`) means working or worth a look. The
two diff colours stay in diffs and +/− counts. Everything else is grey.
The reference app's other colours (project tiles, blue links, blue focus border,
green "online" dots, orange program icons) are not used: links use the text
colour with `↗` and an underline on hover.
The terminal shows program colours as they are; these rules apply to
contr's own UI only.

## Status icons (16 px viewBox, drawn at 12–14 px)

| State | Icon | Where |
|---|---|---|
| needs you | white filled circle, dark `!` | worktree waits on the owner |
| decide | white filled circle, dark `?` | question (D…) |
| do | rounded square outline with → | to-do of kind `do` (T…) |
| fyi | grey circled `i` | to-do of kind `know` |
| blocked | sidebar and tabs: red 7 px dot; card and strip: white octagon with a dark bar | stuck at a prompt |
| working | sidebar only: ring #3A3A3A 2 px with an amber quarter arc, spins 1.4 s; elsewhere a static amber 7 px dot | running |
| quiet | no icon; the second line says `no output · <age>` | mid-turn with no output |
| waiting | grey clock | CI or another agent |
| idle | hollow grey circle (r 4.5, 1.6 px, #6A6A6A) | turn ended, nothing to do |
| done | grey check (#BDBDBD) | stage passed |
| failed | red ✕ | CI failed |
| not started | 3 px dot `text.faint` | stage ahead |
| warning | amber triangle with `!`, 11 px | overlap, conflict |
| you | head icon (column head), white 8 px dot (cell) | owner column |

Spinners show only in the sidebar. Tabs and stage columns use the static
amber dot for the current stage (rules note). Each icon has an accessible
label.

## Sidebar (280)

From the top: window buttons and `contr`, Spaces, then Agents at the bottom,
then Settings.

- **Spaces** header with a `projects` pill and `+ new` (D57: folder picker,
  then an orchestrator in that folder). The pill opens the projects modal
  (D113).
- **Device group:** a header row `This computer` (11 px, dim, with a monitor
  icon). Only the local Mac shows (D68); `+ Add machine` and other device
  groups wait for #149 **(not built)**. The header has no CPU text, memory
  bar or connection dot: CPU and MEM are in the footer (D113). While contr
  does not follow the hc core, the problem line shows under the header.
- **Project rows:** one row per open project, padding 6×12, radius 6; a
  20×20 tile with the project initials (bg #1C1C1C), the name with an
  ellipsis, and at the right the count of commits behind its upstream
  (`↓N`, red) **(not built)**. The project of the selected tab has the
  `bg.card` style. Worktree rows sit under their project, as today: a row
  shows `+N −N` (diff colours, D52) at its right, hidden for 0 and 0 or when
  git fails. The projects and rows come from the hc terminals (orchestrators,
  workers and plain terminals) and the git scan of their folders, not from
  panes or tabs of another host. The second line of a row is `<name> <state>` of its top
  terminal, plus `+N` for the others. A click on a row selects its worktree
  and Review follows it; it does not focus any terminal (D92).
- **Agents** section (12 px dim header), then sub-headers (11 px):
  `Orchestrators N` (dim) first, whatever the state of its agents, then
  `Blocked N` (red), then `Working N` (amber) **(groups by state not built:
  today one `Agents N running` list)**. Empty groups are hidden. The design
  draws no group for workers in other states (see open points).
- **Agent row:** padding 5×12, margin 0×6, radius 6, gap 3. Line 1: the
  program icon (14), the name with an ellipsis, the status icon (12) at the
  right. Line 2 (11 px, indent 22, `text.2`), only when there is something
  to say **(not built; today `<role> · <state> · <age>`, D54)**:
  - `running <command> · <age>` while a command runs;
  - `no output · <age>` while the agent is mid-turn and quiet: grey, then
    amber after 5 min, with no spinner, and an `Interrupt` button under it
    (11 px, padding 2×8, amber text, `status.attention.line` border);
  - `permission prompt · <age>` (red) when blocked;
  - `idle · <age>` when the turn ended.
  An agent that waits after its turn is idle and never warned. Ages are
  `5s`, `3m`, `2h` and move on a state change or a repaint, never on a timer
  (D35, D40).
- A click on a row selects that agent: the top bar selects its tab, the
  terminal shows its hc terminal (D80), the panel follows (D44).
- **Bottom:** a divider, then `Settings` (gear icon). The settings screen is
  not designed yet **(not built)**.

## Top bar (40)

- Tabs of the open project only (D50): the orchestrator first, then its
  workers, then plain terminals (D82). A tab is 28 high, padding 0×10, radius 7,
  gap 7: the program icon (14), the label, and a 7 px dot: amber while the
  agent works, red while it is blocked. The red dot stays until the owner
  has seen that tab (D73) **(dots not built; today the status icon)**.
- The selected tab has the `bg.card` style. The first orchestrator tab keeps
  its white needs badge (D50).
- A plain terminal is a login shell in an hc terminal. Its tab shows the
  terminal's title (set by the shell with OSC), else `$ <folder>`.
- A `+` after the last tab opens a plain terminal in the open project, in
  the first orchestrator's folder, else the home folder. `⌘T` does the same.
  It is always shown, also with no tabs. The terminal area needs only hc;
  the Spaces column shows the hc problem then (D82, D92).
- `⌘W` closes the selected plain terminal and its tab (D85). The next tab is
  selected. An orchestrator or worker tab ignores it.
- Right side: `Review` (D37), then the panel toggle. There is no tab row in
  the panel.

## Terminal

- A card with a header row 34 high (12 px, padding 0×14, bottom border):
  the program icon, the program name and the agent name in 600
  (`<program> · <agent>`), then the terminal's working folder, dim, with
  an ellipsis **(header not built)**.
- The output and the program's own prompt fill the rest. contr draws
  nothing between them: the strip and the card do not move the prompt.
  #150 later draws them between the output and the prompt (D67).
- The selected orchestrator or worker shows its hc terminal (D80); any other
  selection shows `select an agent to see its terminal`. contr only watches
  until the owner types in it: the screen shows the terminal at hc's size, so
  another viewer keeps its size. The first key makes contr's view size the
  terminal (D91).

## Panel (380)

The panel follows the selected tab (D44). An orchestrator (or nothing)
selected shows the board; a worker shows that worker.

### Board (orchestrator tab)

- **Header** (padding 12×14×10, bottom border): line 1 the program icon, the
  project name (600, 13 px, ellipsis), at the right the state (a 7 px dot
  and the word, or `no output · <age>` in amber with `Interrupt`); line 2
  `Board · orchestrator · <model>` (dim).
- **Sections**, in this order, each with a dim head (padding 10×10×4):
  1. `Needs a look · N` (head in `text`), only when not empty: blocked
     workers, and PRs that need a person with no worker on them (conflict
     with main) **(not built)**.
  2. `Work · N running`: the project's workers (orchestrators excluded,
     D46).
  3. `Next`: the ready list, next in line first **(not built; needs a
     source)**.
  4. `Merged`, newest first, with a ghost `all N` at the right
     **(not built)**. Next and Merged rows have opacity 0.9.
- **Stage columns** at the right of the first section head: `P B R CI` and
  the head icon for you, each 16 wide, gap 6, 10 px. A cell holds the
  stage icon: done (check), current (amber dot; red dot when blocked),
  failed (red ✕), not started (faint dot), or nothing. `P` shows only for a
  task that had a planner. The you cell is a white 8 px dot when the owner
  must act. Stages come from roles (D46); the CI column needs a CI source
  **(CI not built)**.
- **Work row** (padding 7×10, radius 7, gap 4, hover `bg.hover`):
  - line 1: the title with an ellipsis, then the stage cells;
  - line 2: the link (`#N ↗` for a PR, `issue #N ↗` for an issue; shown
    once, and it is the link), then `<worker> · <activity>` (`text.2`);
  - line 3, when it applies (11 px): `⚠ touches <file> like #N` in amber
    (another open PR changes the same file); `✕ CI failed · N tests ↗` in
    red; `blocked · waiting at a permission prompt` in red with a small
    secondary `open tab` button; `conflicts with main · no worker on it`
    in amber.
  A click on a row selects that worker's tab (D46). A click on a link opens
  the PR viewer (PRs) or the browser (issues).
- **No Wrap up or Keep going button** (D71), no Report tab.

### Worker (worker tab)

- **Header** (padding 12×14×10, gap 8, bottom border):
  - line 1: program icon, the name (600, 14 px), the role tag (`build`,
    `review`, `plan`: 9 px uppercase, 1px #333333 border, radius 3, padding
    0×4), at the right the activity (`running <command> · <age>`,
    `no output · <age>`);
  - line 2 (`text.2`): `<provider> · <model> · <worktree> · ctx N%`;
  - line 3: the stage cells, then `build · review · CI · you` (dim).
- **Body** (padding 12×14, gap 14), in this order, each part only when it has
  data **(not built; today the last 40 lines of the terminal)**:
  - **CI failed box** (`status.failed.box`, radius 8, padding 10×12): `✕ CI
    failed` (red, 600) and `N failed · M passed`; `job <name> · <time>`;
    the first failing test (mono 11, `text`); a primary `Open log` and
    `+N more failing test`.
  - **Task:** dim head `Task` with a ghost `TASK.md ↗` at the right, then the
    task text (`text.2`).
  - **PR line:** branch icon, `#N ↗`, the PR state (`draft`,
    `changes needed`), at the right the CI result (check or ✕, then `CI`).
  - **Review:** `Review · from <reviewer> · N blockers` (dim), then one line
    per blocker with an amber `●`.
  - **Branch vs main:** dim head with the `+N −N` total, then one row per
    file (padding 4×8, radius 6): status letter (`M`, `A`, `D`, 12 wide),
    the path (mono 11), `+N −N`.
- **Footer** (padding 10×12, top border): secondary `Open diff` at the left
  (opens the Review view on this branch), secondary `Stop` at the right
  (grey; a second click confirms, D44). `Interrupt` shows in the header
  while the worker is quiet.

### Narrow window (D77) **(not built)**

Below 1300 px the panel hides and the terminal takes the width. The bottom
row shows `panel ▴`. A click floats the panel over the terminal: 360 wide,
8 from the top, right and bottom, `bg.popup`, with a ghost `×` in its
header; the button then reads `panel ▾` with the `bg.card` style. Esc (with
the focus in the panel), `×` or a click outside closes it.

## Bottom row (32)

Directly under the terminal and the panel, full width of the main column,
`bg.bars` (D67). It never hides, so the terminal does not move. Left to
right:

- **Needs-you tabs** (D69): blocked workers first, then questions (`D<n>`),
  then to-dos of kind `do` (`T<n>`), then to-dos of kind `know` (fyi icon).
  The scope is the open project; `N in <project> ›` (ghost) after the tabs
  jumps to the next project with open items. A tab is 32 high, padding
  0×12, gap 7, 1px border, margin-right 2: the icon (12), the id in 700, a
  short title (max 110, ellipsis). Off: bg #151515, text #8A8A8A, border
  #242424. On: bg #1A1A1A, text `text`, border `line.strong`, joined to
  the open card. The tab opens toward the card: while the card floats above
  the row, the tab has radius 0×0×8×8 and no top border; when #150 moves
  the card under the strip, it has radius 8×8×0×0 and no bottom border. A
  resolved item has opacity 0.45 until "clear done". **(tab shape not
  built; today one-line tabs, D42)**
- A spacer.
- **Counts** (12 px, `text.2`, gap 16) **(not built)**: `ctx <used> /
  <limit>` of the selected agent; the amber dot and the number working; the
  clock and the number waiting; the blocked icon and the number blocked
  (`N blocked` with a red dot when not 0).
- `board ▴` on a worker tab: it opens the board pop-up. The orchestrator tab
  with the panel open has no `board ▴` (the panel is the board). Below
  1300 px it reads `panel ▴` (D77).
- `⌘J` (dim): opens the context sheet for the picked item.

## Answer card

Floats over the bottom of the terminal, just above the bottom row and
joined to the picked tab, `bg.answer`, padding 14×16, gap 12, fixed 168
high (D44). Its edge toward the tab has no border and no radius (the
design's 10 radius stays on the other corners; today all four corners are
round). Enter motion: up 10 px and fade in, 0.22 s,
cubic-bezier(0.2, 0.8, 0.2, 1) **(motion not built)**.

- Header: icon (14), the id (700), the title (600), the PR link when the
  item has one (`#N`, underlined, opens the PR viewer), the meta (dim,
  ellipsis), then ghost `context ⌘J` and ghost `esc`.
- The question (`text.2`).
- Options: one row each, a 200 wide column with a key button (`1`, `2`,
  `3`; the recommended one first and filled), then its impact line (#8A8A8A)
  **(impact lines not built: need data)**. A free-answer field. Enter sends
  the typed text, else the recommended option; `1`–`4` send an option.
- A resolved item shows `<answer> · sent`, ghost `undo` and secondary
  `next ›`.
- A to-do shows Done. A blocked worker shows its menu (D51), with the
  menu's detail above the question: for a permission, the tool and its
  command (D86).
- The answer goes to the coordinator that asked, as
  `Owner answered <ID>: <answer>` (D36).

## Context sheet (⌘J)

Opens from `⌘J`; `⌘J` again closes it. It is an overlay over the top 470 px
of the terminal area (left edge to the panel, or to the PR pane when open),
bg #161616, bottom border `line.strong`, padding 14×22×16, gap 12. The
terminal does not resize (D67). Esc closes it only while it has the focus
(D72). The shadow and the enter motion are **(not built)**.

- Header: icon, `id  title` (600), meta (dim: from, age), `‹`, `N of M`, `›`,
  then ghost `esc`. The items are the open questions of the open project in
  the order of the needs-you row. `‹` and `›` stop at the ends.
- Left column (grows): `Why it asks`, then the question text in a box (bg
  #111111, border #262626, radius 8, padding 12×14).
- Right column (420): `Options`, one block each (top border `line`, padding
  8×0): the option text, `recommended` for the recommended one (first), and
  its Answer button. Answer goes to the coordinator that asked (D108).
- Not built, and not planned until `owner.Question` carries the data: impact
  line per option, source excerpt and reference, related files and PRs,
  `Show in terminal` (#146), `Ask principal`, `undo`.

## Board pop-up **(not built)**

`board ▴` on a worker tab floats the board over the terminal, right-aligned
above the bottom row: 620 wide, 8 above the row, `bg.popup` with shadow
0 −12 40 rgba(0,0,0,.55), padding 10×6×8. The same sections and rows as the
board panel, with the stage columns 44 wide (`plan build review CI you`).
Enter motion: up 10 px and fade in, 0.22 s. Esc (with the focus), `board ▾`
or a click outside closes it.

## Projects modal **(being built)**

Opens from the `projects` pill in the Spaces header (D113); there is no menu item and no shortcut. Built today: the header, the filter field (narrows the list by name; paste of a path or URL and drag-reorder under a filter are off), the open list with drag reorder and `close`, and `Done`. **(not built)**: `recent`, `+ open folder…` and `clone from GitHub…`. An overlay over the whole
window with the `scrim`; the dialog is 580 wide, 110 from the top, centred,
`bg.overlay`, shadow 0 24 80 rgba(0,0,0,.6). Enter motion: up 8 px and fade
in, 0.22 s.

- Header (padding 16×18×12): `Projects` (700, 14 px), `N open` (dim), then a
  ghost `esc`.
- Filter field (margin 0×18×12, border #2E2E2E, radius 8, padding 8×12):
  placeholder `> filter, or paste a path or a GitHub URL…` (#555555).
- `open in contr · drag to reorder the sidebar` (11 px, dim): one row per
  open project (padding 8×10, radius 8, gap 12): a grip `⋮⋮` (#3A3A3A), a
  28×28 tile (radius 7, bg #1F1F1F, initials 700 11 px), the name (700) over
  `<folder> · <N worktrees · N need you>` (11 px, #5A5A5A), then a secondary
  `close`. Drag changes the sidebar order.
- `recent`: closed projects, the same row without the grip, the tile and
  name dimmer, and a primary `open`.
- Footer (top border #262626, padding 12×18, gap 14): ghost `+ open folder…`,
  ghost `clone from GitHub…`, spacer, primary `Done`.
- `close` hides the project in contr only (D70); the order, hidden and recent lists live in `projects.json` (D63). Esc (with the focus in the
  modal), `esc` or `Done` closes the modal.

## PR viewer (#151–#154) **(being built)**

A PR number (card header, sheet refs, a board link, the worker panel) opens
the PR in a 560 wide pane at the right of the terminal (D74). The pane
pushes the terminal; it is the one accepted resize besides the window and
the panel. Enter motion: left 24 px and fade in, 0.25 s. While it is open,
needs-you tabs show only the icon and the id, and the bottom row hides
`board ▴`.

- **Header** (padding 12×16×10, bottom border, gap 6): line 1 `#N` (700), the
  title (600, ellipsis), ghost `GitHub ↗` (opens the browser), icon `×`;
  line 2 (dim, wraps): repo, `branch → main`, author agent, `+N −N · N
  files`; line 3: the state badge (padding 1×8, radius 4: `approved` with a
  #5A5A5A border, other states with a #3A3A3A border and `text.2`, `merged`
  in the `you` style), the CI summary with its icon, and at the right
  `for <item>` (dim).
- **Tabs** (padding 10×16×0): a segmented control (bg #1A1A1A, radius 7,
  padding 2; the selected segment bg #2A2A2A and `text`):
  `Overview`, `Files N`, `Checks N`.
- **Overview:** the description (`text.2`), then `Timeline`: one row per
  event (top border #1F1F1F, padding 6×0, gap 10): icon (12), who (700, 74
  wide), text (`text.2`), time (dim).
- **Files:** the file list (rows padding 4×8, radius 6: path, `+N −N` in the
  diff colours), then the diff in the kit code view (D34) with the review
  threads under their lines: a box (margin 6×10×8×48, bg #1C1C1C, border
  #2A2A2A, radius 8, padding 8×10): who (700) and `on line N` (dim), the
  text, ghost `reply` and `resolve`.
- **Checks:** one row per check (top border #1F1F1F, padding 8×0): icon
  (14), name, time (dim), ghost `log`.
- **Footer** (top border, padding 10×16×12, gap 8): a comment field (border
  #2E2E2E, radius 8, padding 7×10, placeholder `Leave a comment on #N…`),
  then secondary `Comment`, secondary `Approve` (hidden once approved),
  spacer, primary `Squash and merge`. While CI runs the merge button is
  replaced by the clock icon and `merge waits for CI`. After a merge:
  `merged · principal told`; the item is the worker's issue, and the project's
  orchestrator gets the message `contr: PR #N merged at <short sha>` (D105).
- Keys while it has the focus: Esc closes, `c` comments, `⇧⌘M` merges.

Slice 1 (#235, D59) builds the pane, the header lines 1 and 2, the state
badge and the CI summary, the tabs and the Overview. The tab switch is
`kit.Switch`, so the Review mode switch has the same look. It opens from the `#N` badge
of a sidebar worktree row and the `#N` label of a board work row (the
worker's PR, found from its branch; with no PR the label opens the issue in
the browser, D104). The worker panel and the answer card of a blocked
worker have a button that does the same (`Open PR #N`, else `Open issue #N`,
D106). Line 3 of the header ends with `for #N`, the issue of the worker that
opened the pane (D106). While it
is open, the needs-you tabs and the title bar tabs show icon and id only.
Slice 5 (#152, D101) builds the Checks tab. Slice 6 (#153, D102) builds
the Files tab. Slice 9 (#154, D103) adds `reply` and `resolve` to an open
thread: `reply` opens a field in the box (Enter or `Reply` sends), `resolve`
sends at once. Each write runs off the UI thread, then the threads are read
again; an error shows at the top of the tab and typed text stays. A resolved thread
shows as one line, `resolved · who`; the owner has not decided this yet
(D25). The thread box starts 12 px from the gutter, since the code view
already leaves the gutter free. A thread whose line is not in the diff is
not shown. Slice 8 (#154, D103) builds the footer and its keys. Not built yet: the
other entry points and `for <item>`. The bottom row has no `board ▴` yet, so there is nothing
to hide.

The footer (#154, D103) shows for an open PR: the comment field (Enter or
`Comment` sends), `Approve` and `Squash and merge`. `Approve` is hidden on the
owner's own PR and once the PR is approved. The merge is pinned to the head
commit that the pane shows. A failed CI does not stop the merge: the button
stays and the CI summary is red. A write error shows above the field. After a
write the pane reads the PR again. After a merge from the pane the footer
shows one line and no buttons (D105): `merged · principal told`; `merged ·
no one told` when the project has no orchestrator; or `merged` and the error
when the send failed.

## Review

Shown in the terminal area while the top bar's `Review` button is on (D37).
The repo is the selected worktree's root, or the folder of the selected
agent. With no selection it shows `Select a project`.

- Three columns: branches (260), files or commits (260), code (grows).
  Branches are all local branches ahead of `main`.
- Labels: `branch <name>`, `file <path>`, `commit <short>`; the switch
  `review mode` has `Whole branch` and `By commit`.
- Rows and the code view come from `internal/app/kit`; the only colours are
  the two diff colours (D34).
- No git runs while the view or the window is hidden.

## Footer (26)

`bg.bars`, top border `line`, 11 px dim. Left: plan usage, `claude` and
`codex` with a 36×3 bar and the percent left (no OpenCode Go segment).
Right: the message slot (errors from `+ new` and other actions), host CPU
and memory, the contr version, and the hc core's version (`hc 0.1.18`)
while contr follows the core (#245).

## Motion

- Enter motions: card and pop-ups up 10 px, modal up 8 px (0.22 s); sheet
  down 32 px (0.3 s); PR pane left 24 px (0.25 s); a new output line up
  6 px (0.4 s, ease-out). All use cubic-bezier(0.2, 0.8, 0.2, 1) unless
  noted. Exit is instant.
- Flash (D73): a new blocked item's tab pops in (scale 0.8 to 1, 0.3 s), then
  flashes to the `you` colours 4 times (0.9 s each). Then it keeps a static
  unseen style until seen. Flashes and spins run at 30 fps or less (D26).
- Reduce Motion: no enter motions and no flashes; the static unseen style
  stays. The working arc does not spin.

## Keys

| Key | Does | Where |
|---|---|---|
| `⌘T` | open a plain terminal in the open project (D82) | window, File menu |
| `⌘W` | close the selected plain terminal (D85); does nothing on an orchestrator or worker tab | window, File menu |
| `⌥1`–`⌥6` | pick needs-you tab 1–6 and open its card (D75) | window |
| `1`–`4` | send that option | card, with the focus |
| Enter | send the typed answer, else the recommended option | card, with the focus |
| `⌘J` | open or close the context sheet | window |
| Esc | close the sheet, card, pop-up, modal or PR pane | only the part with the focus (D72) |
| `c`, `⇧⌘M` | comment, squash and merge | PR pane, with the focus |

Esc is never a window shortcut: it interrupts Claude Code in the terminal.

## Data the design needs

From the note "Needs new data before building". The UI shows a field when
its source has it and leaves it out when not.

| Data | Source today |
|---|---|
| Next (ready) list | none |
| Review verdict and blockers | none |
| Worker ctx % | none |
| Option impact lines, source message, refs | owner store (D32) has no fields for them |
| Jump to the message | none |
| +/− per file | git (D52 reads totals per worktree) |
| Time since last output | hc events (`LastOutput`, D35) |
| Failed CI job and test names, CI state, conflict state per PR | GitHub (PR viewer client, #151) |
| Running command and how long it has run | hc foreground command (D35); no start time |
| Files shared between open PRs | git or GitHub, not read yet |
| Interrupt action | built (`AgentSource.Interrupt`, D44) |

## Open points

v0.2 does not settle these:

1. **Projects control.** Settled (D113): a `projects` pill in the Spaces
   header, next to `+ new`. Native · final draws a pill next to a settings gear;
   v0.2 has no gear here.
2. **Bottom row tabs.** Native · final draws the bottom row with counts only
   (no open items). v0.2 puts the needs-you tabs at its left, as the Final
   strip did, and flips the tab shape toward the floating card. The row is
   32 high here (Native draws 30, the Final strip tabs are 32).
3. **Sidebar width.** Native · final draws 196 in a proportional font; v0.2
   keeps 280 for the mono font.
4. **Agent row second line.** The design shows activity; D54 shows
   `<role> · <state> · <age>`. The design form needs the running command's
   start time.
5. **PR pane and panel.** With the panel open, whether the PR pane replaces
   the panel or sits beside it is not drawn.
6. **Footer.** The reference footer shows the device, branch, agent count and
   version. v0.2 keeps contr's usage footer.
7. **Board pop-up.** Native · final draws `board ▴` but not the open
   pop-up; v0.2 uses the Final page's pop-up.
8. **Settings screen** and **`+ Add machine`** are not designed.
9. **Ages.** `merged <age>` and `no output · <age>` need a repaint to move; D35
   forbids a timer. When they move is open; so is when `no output` turns
   amber at 5 min.
10. **Other agent groups.** The sidebar draws `Blocked` and `Working` only.
    Where idle, waiting and done workers go is open.
11. **Hues.** D76 follows the rules note (red and amber for status). It
    replaces the greyscale rule of v0.1; the owner can still keep
    greyscale.
