# FlowSection

The Flow section of Settings → Agents: how the orchestrator takes an issue to a merge (D179).
It shows and edits `agents.Flow`, the `flow` key of `agents.json` (D177).

Shared rules: [README.md](README.md). Design: board 10 of the from-zero canvas.

## Anatomy

1. Head: `Flow` (bold, `ink-95`) and the hint `How the orchestrator takes an issue to a merge`
   (`ink-50`), `space-8` apart.
2. Presets, a segmented control (`SegmentedBase`): `Builder only`, `Build and review`,
   `Plan, build, review`. Track: `hover` fill, `space-2` padding, card radius. Chosen segment:
   `selected` fill, `ink-100` text; the others `ink-62`.
3. Row of steps on a `hover` fill, card radius, `space-8` padding: `Planner` → `Builder` →
   `Reviewer` → `Fix rounds` → `Merge`. Each step has its name (`ink-85`) and a detail (`meta`,
   `ink-50`): Planner `when needed` / `always` / `off`; Reviewer `code PRs` / `every PR` / `off`;
   Fix rounds `debugger after N`; Merge `after your OK` / `when CI and review pass`. The arrows
   are `ink-38`. An off step is dim: name and detail `ink-38`.
4. Rows, label at the left (`ink-62`), control at the right:
   - `Plan first`: segments `Never` / `When needed` / `Always`.
   - `Review`: segments `None` / `Code PRs` / `Every PR`. With `Code PRs`, a note below (`meta`,
     `ink-50`): `docs-only PRs: the orchestrator reads the diff`.
   - `Debugger after`: the number (`value`, `ink-95`), `failed fix rounds` (`ink-50`) and a MyGo
     `Stepper`.
   - `Workers at once`: the number, `reviewers count` and a `Stepper`.
   - `Merge`: segments `Ask me` / `When CI and review pass`.

## Behavior

- A preset sets `plan` and `review`: Builder only = never, none; Build and review =
  when-needed, code; Plan, build, review = always, code. The other values stay.
- The preset whose `plan` and `review` match the flow is chosen; a flow that matches none
  chooses no preset.
- Each control calls `onChange` with the new flow. The steppers stop at 1 (and at 99).

## Data

- `FlowSection(c, flow, onChange)`: the caller loads the flow, saves it off the UI thread, and
  builds the section again with the saved flow.
