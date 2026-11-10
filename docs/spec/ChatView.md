# ChatView

The Chat mode of the terminal panel for a Claude Code session (D246). It
shows the session from its transcript (D242) in the place of the terminal
grid, with a composer that types into the session's own terminal and a live
line that shows what the agent does (D252). While the session orchestrates,
a team row takes the live line's place (D259). The overlays (notch, pill,
outline) stay above it.

Design system page: none yet; the mockups C1 (base), C2a (lines of an open
group) and C2b (the folded group) are the design intent.
Shared rules: [README.md](README.md).

## Switch

| Part | Value |
|---|---|
| Place | alone at the top right of the terminal panel, a layer of the terminal overlay under the notch: top `space-8`, right 10px (the outline inset); right 40px while the outline column's top is above the switch's bottom (34px), clear of the column (`Outline.Top`, the D237 rule, D253). The [OrchestratePill](OrchestratePill.md) is in the title bar. |
| Control | `kit.Switch` (`SegmentedBase`): "Chat", "Terminal"; label "View of this session" |
| Look | solid: on a `float-solid` fill with a 1px `float-line` border, radius 7, so the terminal text under it does not show through; the chosen segment keeps its `selected` fill (D252) |
| Shows for | a Claude Code session: an orchestrator (with a plain terminal that runs Claude Code, D190), or a terminal whose transcript path a kit hook saved (a worker, D243). A plain terminal has no switch. |
| Mode | kept for each terminal for the run of the app; Terminal is the default |
| Key | `⌘E` toggles the selected session; key chip `E` centered on the switch while ⌘ is held ([KeyHints.md](KeyHints.md)). Back in Terminal, the grid takes the focus. |

## Regions

| Region | Value |
|---|---|
| Transcript |  `ui.List` with `FollowEnd`: it follows the end while the owner is at the end. Rows `space-18` apart, padding 44px above (the switch row), `space-20` at the sides, 32px below (the fade's height, so the fade covers no entry, D259); rows that do not fill it sit at the bottom. The pending prompts come after the rows. |
| Fade | a gradient 32px high over the bottom edge of the transcript, from the stage at 0 opacity to the stage as it shows (`StageBase`, D250); it takes no click (D259) |
| Column | the prose (prompts, Claude's text, notes, the fold rows, the live line, the team row and the composer), at most 920px wide, centered in the content width: the panel less `space-20` at each side |
| Wide block | a table, a code block or the card of an open work group: as wide as its content, at least the column and at most the content width, centered. A block that fits in 920px stays in the column, its left edge on the column's left edge; in a panel not wider than 920px and the insets, the column is the content width. Claude's text is cut into runs of prose and these blocks, each cut at the start of a line. |
| Live line | under the transcript, above the composer, `space-12` apart, padding `space-20` at the sides; see [Live line](#live-line) |
| Team row | while the session orchestrates: in the live line's place, above the composer; see [Team row](#team-row) |
| Composer | under the live line or the team row, `space-14` below; see [Composer](#composer) |
| Empty | "No transcript yet" (`body`, `ink-42`), centered, while the session has no path, no file or no row yet. The switch and the composer still work. |

## Rows

| Row | Value |
|---|---|
| Owner prompt | right-aligned, at most 560px, padding `8px 13px`, radius `radius-12`, fill `hover`, text `prose` in `ink-100` |
| Sent time | the time the entry was sent, from its transcript timestamp: one line, mono `meta`, `ink-45` (the lowest text ink with 4.5:1), `space-4` above. Under an owner prompt, right-aligned with the bubble; under Claude's text, left-aligned, once per answer: under the last text before the next prompt, work group or note. In the Mac's time zone: "14:32" today, "Yesterday 14:32", else "9 Nov 14:32"; "today" is judged at each build, and nothing ticks (D55). No timestamp, no line; a pending prompt has none. |
| Claude's text | `markdown.View`: `prose` size and line, sans; text `ink-85`, strong and links `ink-95`, muted `ink-45`; code mono `ink-90` on `hover`; code blocks with Copy on `hover` with a `hairline` line; tables with `hairline` lines and a `hover` header |
| Work group, folded | the tool calls, kit notes and background tasks between two texts, as one row 20px high, mono `value`, `ink-42` (`ink-62` on hover): "2 worker notes · 1 background task · 6 tool calls" (a message of another session counts as a worker note; a part with no item is left out, "1 tool call"), a 6px dot for each worker that sent a note, then `›`. While a call has no result, a working StatusGlyph (14px) comes first. |
| Work group, open | `⌄`; under it a card, a wide block: at least 584px (or the content width when less), as wide as its longest line (note, task and call lines alike), up to the content width, `space-6` below the row, padding `8px 12px`, radius `radius-9`, line `hairline`: the note lines, a `hairline` rule, the call lines |
| Note line | 20px, gap `space-8`, mono `meta`: the sender's dot, the name (500, `ink-85`), the text on one line (`ink-42`). An owner answer or done mark has the name "owner". A message of another Claude session or a teammate (D254) has the sender as its name ("session" when it has none) and the first line of the message; a subagent's hand-back is "subagent" and "reported". |
| Task line | a background task that ended (D254), as a note line without a dot: "Background task" (500, `ink-85`), its status (`ink-42`), then "· " and its summary on one line (`ink-42`), without a leading "Background command " |
| Call line | 20px, gap `space-8`, mono `meta`: a working StatusGlyph while it runs, the name (`ink-62`, at least 48px; an MCP tool shows its server, "contr"), the summary on one line (`ink-42`), "error" in `blocked` when the result is an error, the duration (`ink-38`): "0.4s", "12s", "2m 5s", from the timestamps of the call and its result, none when one is missing |
| Failed task | a task line whose status is "failed" or "killed" stays outside the fold: no dot, "Background task" and the status in `ink-100`, the summary in `ink-85`; no red (D259). Every other note folds, a worker's message too ("ended its turn without a report", "NAME: blocked, …"): the team row and the notch tell what needs the owner. |
| Dot | 6px, round: `ink-100` for a blocked worker (no red: the team row's "N needs you" is the chat's one red for it, D259), `ink-85` for a working one, else `ink-40` |

Hidden entries do not show: meta lines, system reminders, hook output (an
attachment, a user line in a `<…-hook>` tag, or "Stop hook feedback:"),
the kit's playbook, local command output, compact summaries, interrupt
marks, system lines, thinking marks and the lines of subagents. Other
harness texts hide too (D254): "[Image: source: …]" lines, "The user
hasn't heard from you…" nudges, and a user line that is all inside one
`<tag …>…</tag>` pair whose name is lowercase letters and dashes
(`<command-message>`, `<local-command-caveat>`). A slash command
(`<command-name>`) stays a prompt: "/name args". A prompt that carries a
`<system-reminder>` or a hook's tag shows only its own text.

## Composer

| Part | Value |
|---|---|
| Field | `TextAreaBase`, label "Message", placeholder "Message <session name>…" (or "Message…" when the agent is not known) in `ink-38`; text `prose` size in `ink-100`. One line at first; it grows to 8 lines, then scrolls. |
| Box | the column's width, at least 44px high, padding `0 14px`, gap `space-12`, radius `radius-12`, 1px `input-line` border, `hover` fill |
| Stop | an outlined card button "Stop", right of the field, only while the agent works |
| Meta | at the right, mono `value`, `ink-42`: the model of line 2 of the agent's row and the size of its context from the Context line, "opus high · 212k"; a part that is not known is left out |
| Error | the last failure, under the box, `small`, `ink-62`, padding `space-6 14px 0`; a change of the text clears it |

- The field takes the focus as it appears: when the chat shows (the switch,
  ⌘E). It keeps the focus after a send, and takes it back after a click on
  Stop or a choice.
- Enter sends; ⇧Enter breaks the line. A blank field sends nothing. Send
  types the text into the selected session's own terminal and presses Enter
  through the agent source's `Send`, as the orchestrate pill does. The field
  clears at once and the text shows at the end of the transcript as a
  pending prompt (the prompt bubble at 55% opacity) until the transcript
  brings a prompt with the same text; a reset drops it. A failed send drops
  the pending prompt, puts the text back in an empty field and shows the
  error. One send runs at a time.
- Text can be sent while the agent works. While it waits on a menu, Enter
  sends nothing and keeps the text: "Pick a choice above first, or switch to
  Terminal." A stray Enter never picks a choice (D51).
- Esc in the field interrupts the agent while it works (the agent source's
  `Interrupt`, the stop key that Esc is in the terminal); so does Stop. With
  nothing running, Esc does nothing: it closes and switches nothing.
- The keys of the field work only with the focus in it; ⌘ shortcuts keep
  working.

## Live line

| State | Value |
|---|---|
| Working | a row 20px high, gap `space-8`, mono `value`: the working StatusGlyph (14px), what the agent does now in `ink-62` on one line, and the time since the turn started in `ink-38`: "8s", "2m 5s", "1h 4m". What it does: the last tool call with no result, "Bash · go test ./..." (the name as a call line has it); else "Thinking" when a thinking mark is the last entry; else "Working". The turn starts at the time of the last owner prompt; with no time there, no time shows. Hidden entries and the lines of subagents do not count. |
| Needs you | the agent is blocked or in error: a 6px `blocked` dot and the question of its menu (`body`, `ink-100`), and under it the choices as outlined card buttons "1 Yes", "2 No" (the first nine). The menu is read from the screen as the questions card reads it (`questions.Menu`, D51): again on each change of the agent's turn or message, with no timer. A choice reads the screen again and sends its keys only when the menu is the one shown; else "The prompt changed; look again." A choice that asks for typed text switches to Terminal. While a menu shows, a digit typed into the empty field sends that choice at once and clears the field; the menu takes no Enter (D140). With no menu read: "The terminal needs you" and the outlined button "Switch to Terminal". |
| Idle | no line |

The time asks for a redraw each second only while the agent works, the
chat shows and the turn has a time (D55). Idle, nothing is asked.

While the session orchestrates, the working line shows at the right end of
the team row and not as a line of its own; the needs-you line stays, above
the team row.

## Team row

One row, 20px high (the live line's height), in the column, above the
composer, while the selected session runs and orchestrates (D191, the
orchestrate pill's rule). Not orchestrating: no row; the live line shows
alone. No hover fill, no chips, nothing opens (D259).

| Part | Value |
|---|---|
| Mascot | the still Claude glyph (14px) in its orchestrating color, as the [OrchestratePill](OrchestratePill.md) has it |
| Head | `space-8` after the mascot: "Orchestrating · 4 agents", "Orchestrating · 1 agent", "Orchestrating" with no agent; `label`, `ink-85`. The agents are the workers the session started that hc still lists, ended ones too. |
| Count | `space-14` after the head, mono `value`, `ink-45`, the parts `space-6` apart with a "·" between: "1 needs you · 2 working · 1 idle · 1 done". In this order; a part with no agent is left out. "N needs you" ("1 needs you", "2 need you", as the notch) is the number of the orchestrator row's badge (D240): the questions, to-dos and blocked menus of the session and its workers. It is in `blocked-text`, the one red of the row, and a click on it opens the notch's card as ⌘J does; the rest of the row takes no click. A blocked worker counts only there; "working" is a live worker that works, "idle" any other live worker, "done" an ended one. |
| Live text | at the right end, `space-14` after the count, while the session works: the working line's glyph, what it does and its time ([Live line](#live-line)) |
| Narrow | the row clips from the right: first the live text's words shorten (`…`), then the count drops its last parts; the mascot, the head and the live text's glyph and time stay |

The row redraws when the agent state changes; it has no timer of its own.
Only the live text's time ticks, as the live line's does (D55, D257).

## Behavior

- The view reads the transcript only while it shows: a read runs off the
  UI thread and its entries are applied on it. A change of the file
  (chatsource, D243) reads what was added; a moved path or a shorter file
  reads again from 0. Nothing polls, and no timer runs (D55). While the
  chat does not show, a change of the file asks for no frame (D257).
- A click on a group row opens or closes it; the list holds its place for
  that frame.
