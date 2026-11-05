# QuestionsNotch

A small glass tab that hangs from the top of every terminal of the project while an agent asks the owner a question or something else needs the owner; click opens the questions card.

Design system page: component QuestionsNotch. Shared rules: [README.md](README.md).

| Dark | Light |
|---|---|
| ![QuestionsNotch, dark](images/QuestionsNotch-dark.png) | ![QuestionsNotch, light](images/QuestionsNotch-light.png) |

## Anatomy

| Part | Value |
|---|---|
| Box | height `notch-height` (30px), padding `0 16px 0 12px`, gap `space-9`, radius `0 0 radius-12 radius-12` |
| Glass | `glass-float`, blur `glass-blur`, line `float-line`, shadow `shadow-notch` |
| Glyph | StatusGlyph, needs-you, 14px |
| Label | 12px, `ink-100`: "1 question", "2 questions"; with to-dos or blocked menus, "3 items"; "1 item" for one to-do or menu alone (D124, D135) |

## States

- **Hidden:** no open questions, to-dos or blocked menus.
- **Closed:** as above.
- **Open:** see QuestionsCard.

## Behavior

- Shows while the project has at least one open question, to-do or blocked
  worker menu, at the top center of each terminal of the project.
- To-dos (`do`, `know`) and the menu of a blocked worker (D51) are more
  chips after the questions, in the card (D124). The count in the label
  covers all of them.
- Click or `⌘J` opens the QuestionsCard.
- Worker questions reach the owner through the orchestrator.

## Data

- `internal/owner` (questions file), watched with `mygo.WatchFiles`; read on a goroutine.

## Keys and focus

- `⌘J` opens and closes the card. Space and ⏎ when focused.

## Edge cases

- Questions, to-dos and blocked menus of another project: not counted here.
- The questions file is broken (unreadable or locked): the model logs the error once (D135) and the notch shows "Questions file unreadable" in `blocked`, in the same place and in every terminal of the window, with the error in a tooltip and no click (D146). The card closes. It goes when the file reads again.

## Accessibility

- Label: "2 questions. Open". With to-dos or blocked menus: "3 items. Open".

## Tokens

`glass-float`, `float-line`, `shadow-notch`, `notch-height`, `radius-12`, `blocked`, `ink-100`, `label`.

## Build in MyGo

- `ButtonBase` in the terminal overlay; glass from `Material(glass.Blur{...})` under a `glass-float` fill (glass plugin, D135).
- It shows in each terminal of the project, orchestrator and workers alike, so the owner can answer from any of them.
- Data: `internal/owner` (questions file), through the agent state (`agentstate`), which watches it and reads on a goroutine (D135). A worker question reaches the owner through the orchestrator.
- Shortcut: ⌘J opens the card.

## Do and do not

**Do**

- Say the count in words: "2 questions".

**Don't**

- Do not show the notch when nothing needs the owner.
- Do not animate the notch in a loop: only the glyph moves.

## Tests

- View: hidden at 0, "1 question", "2 questions".
- View: a to-do and a blocked menu count with the questions ("3 items").
- View: shows in an orchestrator and a worker terminal of the same project.

## Replaces

- The question tabs of the v0.2 bottom row (D69).
