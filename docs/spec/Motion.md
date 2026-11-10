# Motion

Short transitions (D256, issue #659). Each one ends in 300 ms or less, and
after its end no frame is drawn (D55). All of them use MyGo's own motion:
`ElementTransition`, `AnimateColor`, `AnimatePose`, `ContentTransition`
(`Numeric`) and `AnimateWith`. The helpers are in
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

## Where each is used

| Change | Motion | Token | Where |
|---|---|---|---|
| A row or tab is selected | its fill, border and inks fade (`kit.FadeTransition`, `kit.FadeInk`) | `motion-select` | agent rows and the icon column, flat rows (projects, servers), card rows (Review and PR files), title bar tabs, the Chat \| Terminal switch (`kit.Switch`) |
| A worker row turns to needs you | its title fades to `ink-100` and the "needs you" words fade in; no fill changes (D249); the badge and the notch already spring (D240) | `motion-state` | agent rows (`kit.RowMotion` picks `motion-state` in the frame the need changes) |
| A popover opens or closes | scale 0.96 to 1 from its anchor, with a fade (`kit.PopIn`) | `motion-pop-in`, `motion-pop-out` | orchestrate popover, update panel, What's new |
| A modal opens or closes | the backdrop fades, the card scales 0.96 to 1 as it comes in; it goes with the fade only (`kit.PopDialog`) | `motion-pop-in`, `motion-pop-out` | Settings, the projects modal |
| A number changes | the digits that change roll, up or down as the value goes (`kit.RollNumber`) | `motion-number` | the values of the Context meter and the plan meters |
| A meter changes | the fill moves to its new width (`kit.MeterFill`) | `motion-meter` | the Context meter and the plan meters |

Selection stays instant for the keyboard: the selected state changes at
once in logic (focus, the tabs and the right panel follow at once); only the
colors fade. No position or size is animated: the tab pill does not slide.

## Not animated

- Native macOS menus (context menus, menu buttons): the system draws them.
- MyGo's drawn menus (`PopoverMenuButton`, `PopoverMenu`): MyGo gives no way
  to set a transition on their panel.
- The questions card: it opens from the notch, and the notch opening is a
  later issue, with the tab pill slide, panel show and hide, and rows that
  arrive or leave.
- Inner controls of the modals (Settings tabs and segments, the projects
  list): they show inside a card that is already in motion as it opens.

## Reduce Motion

MyGo follows the desktop setting (`Preferences().ReduceMotion`). Positions,
sizes and scales change at once; fades stay. So a selection and a needs-you
row fade as without it; a popover and a modal fade without the scale; a
number cross-fades in place of the roll; a meter fill is at its width at
once.
