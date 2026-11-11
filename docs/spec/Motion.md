# Motion

Short transitions (D256, issue #659) and the second tier, what moves and
changes place (D263, issue #679). Each one ends in 300 ms or less, and
after its end no frame is drawn (D55). All of them use MyGo's own motion:
`ElementTransition` (with `Match` for the second tier), `AnimateColor`,
`AnimatePose`, `ContentTransition` (`Numeric`) and `AnimateWith`. The helpers are in
`internal/app/kit/motion.go`; the tokens are in [tokens.md](tokens.md#motion)
and in `internal/app/theme/motion.go`. Views use the token names, never a
number.

| Token | Value | Ease |
|---|---|---|
| `motion-select` | 120 ms | ease-out |
| `motion-pop-in` | 140 ms | ease-out |
| `motion-pop-out` | 100 ms | ease-out |
| `motion-state` | 300 ms | ease-out |
| `motion-number` | 200 ms | ease-out |
| `motion-meter` | 300 ms | ease-out |
| `motion-tab` | 180 ms | ease-out |
| `motion-panel` | 220 ms | cubic-bezier(.2, 0, 0, 1) |
| `motion-notch-open` | 260 ms | ease-out |
| `motion-notch-close` | 160 ms | ease-in |
| `motion-row-in` | 200 ms | ease-out |

R9 gives the tab pill and the notch opening as springs. A MyGo spring of
180 ms or 260 ms comes to rest only after about 420 ms or 600 ms, past the
300 ms limit, so both are curves of the same length (D263).

## Where each is used

| Change | Motion | Token | Where |
|---|---|---|---|
| A row or tab is selected | its fill, border and inks fade (`kit.FadeTransition`, `kit.FadeInk`) | `motion-select` | agent rows and the icon column, flat rows (projects, servers), card rows (Review and PR files), title bar tabs, the Chat \| Terminal switch (`kit.Switch`) |
| A worker row turns to needs you | its title fades to `ink-100` and the "needs you" words fade in; no fill changes (D249); the badge and the notch already spring (D240) | `motion-state` | agent rows (`kit.RowMotion` picks `motion-state` in the frame the need changes) |
| A popover opens or closes | scale 0.96 to 1 from its anchor, with a fade (`kit.PopIn`) | `motion-pop-in`, `motion-pop-out` | orchestrate popover, update panel, What's new |
| A modal opens or closes | the backdrop fades, the card scales 0.96 to 1 as it comes in; it goes with the fade only (`kit.PopDialog`) | `motion-pop-in`, `motion-pop-out` | Settings, the projects modal |
| A number changes | the digits that change roll, up or down as the value goes (`kit.RollNumber`) | `motion-number` | the values of the Context meter and the plan meters |
| A meter changes | the fill moves to its new width (`kit.MeterFill`) | `motion-meter` | the Context meter and the plan meters |
| A tab is selected | its pill slides from the tab selected before, under the tab's content; the inks fade as above (`kit.SlideTransition`) | `motion-tab` | title bar tabs |
| The owner shows the sidebar or the files panel | the panel comes in from 24px toward its outer edge with a fade; the layout, so the terminal grid, changes once, at the start (`kit.PanelIn`) | `motion-panel` | the sidebar, the files panel |
| The questions card opens | it grows from the box of the notch to its own; its content keeps the card's width, and the card's clip shows it (`kit.GrowFrom`) | `motion-notch-open` | the questions card |
| The questions card closes | the notch shrinks back from the box of the card (`kit.ShrinkFrom`) | `motion-notch-close` | the questions notch |
| An agent row arrives | it comes down 4px into its place with a fade (`kit.RowIn`); its room in the list is there at once | `motion-row-in` | sidebar agent rows |

Selection stays instant for the keyboard: the selected state changes at
once in logic (focus, the tabs and the right panel follow at once); only the
look moves. A row's fill and inks fade; the tab pill slides (D263).

## Not animated

- Native macOS menus (context menus, menu buttons): the system draws them.
- MyGo's drawn menus (`PopoverMenuButton`, `PopoverMenu`): MyGo gives no way
  to set a transition on their panel.
- A panel that the owner hides: MyGo plays an `Exit` only for an element
  whose parent stays, and the split that holds the panel goes with it. The
  terminal takes the room at once and would cover a copy of the panel.
- A panel that shows or hides as the window gets wider or narrower: MyGo
  does not animate what a window resize changes.
- The width of a panel: each frame of a width change would resize the
  terminal grid, and the shell in it would draw again on each one. The
  panel moves and fades in place; the grid resizes once.
- An agent row that leaves, and the room of a row that arrives: rows of a
  `ui.List` are placed by the list, and do not come, go or collapse with a
  transition. A row that leaves goes at once.
- The content of the questions card does not fade in: the card takes the
  focus in its first frame, and that frame drops a first-frame pose in MyGo.
  The card's clip shows the content as it grows.
- Inner controls of the modals (Settings tabs and segments, the projects
  list): they show inside a card that is already in motion as it opens.

## Reduce Motion

MyGo follows the desktop setting (`Preferences().ReduceMotion`). Positions,
sizes and scales change at once; fades stay. So a selection and a needs-you
row fade as without it; a popover and a modal fade without the scale; a
number cross-fades in place of the roll; a meter fill is at its width at
once. The tab pill is at its tab at once; a panel that shows and a row that
arrives fade in place; the questions card and the notch are at their size
at once.
