---
name: chat-writing
description: Writing answers for contr's chat. Use in a session that runs in contr (HC_TERMINAL_ID is set) when you write an answer the owner reads: a report, a status, a comparison, a plan, a question.
---

contr shows your answers as Markdown in its chat. It also shows some parts that plain Markdown does not have: remarks, live chips, a status board and a merge queue. This skill lists what the chat shows today and when to use each part.

## Taste

Pick the most visual form that fits the content, not text every time: a table for a comparison, a status board for workers, a queue for an order, a list for steps. Keep it minimal: no decoration, no part that carries no information. Short prose is right for a short answer.

## Headings and lists

Use headings for the parts of a long answer, and lists for items. Write a heading or a list, not a bold lead-in ("**Tests:** …"). All heading levels show at the same size, so use them for structure only. Numbered lists, bullet lists and task lists (`- [x]`, `- [ ]`) show.

```markdown
## Next
1. Merge #656.
2. Start r-665 on #665.
```

## Tables

Use a table when the items share two or more attributes (a comparison, a list of files with counts). Use a list when each item is one line of text. A column of numbers without an alignment goes to the right by itself. A cell wraps at about 340px, so keep cells short. A table wider than the text column takes the panel's width, and scrolls sideways when it is wider still: many columns are fine.

```markdown
| Option | Lines | Risk |
|---|---|---|
| Patch the parser | 40 | low |
| New reader | 300 | medium |
```

## Code blocks

Put code, commands, logs and output in a fenced block with its language after the fence (`go`, `sh`, `json`). The chat colors the code and shows the language and a Copy button. A `diff` or `patch` block colors added and removed lines.

Write each item once. Do not write a row of blocks and then a table that names the same items again. For several texts to copy with a note for each (a prompt and what to expect from it), write a numbered list: the note is the item's text, and the text to copy is a code block under it.

```diff
-	return nil
+	return err
```

## Remarks

A quote whose first line is a tag shows as a remark with a colored bar and a word. At most two remarks in one answer.

| Tag | Use for | Shows |
|---|---|---|
| `> [!DONE]` | work that is finished | Done |
| `> [!DECISION]` | a choice the owner must make; the only red one | Decision needed |
| `> [!WARNING]` | a risk or a conflict | Warning |
| `> [!UNVERIFIED]` | what nobody checked | Not verified |
| `> [!NOTE]` | other context | Note |

```markdown
> [!DECISION]
> Keep the old flag one more release, or remove it now?
```

## Live chips

contr turns names it knows into live chips: a dot with the current state, and a click that opens the item. Write the name as plain text. A name in backticks, in a code block or in a link stays text.

- A PR or an issue: `#667`. A PR chip shows its checks; a click opens it in the browser.
- A worker: its name, as `b-649`. The dot shows its state; a click selects it in the sidebar.
- A file of the session's folder: its path, with an optional line, as `internal/app/view.go:42`. A click opens it in the Review view.

So write "#667 waits for b-649, see internal/app/view.go" and not "`#667` …".

## Status board and merge queue

For coordinators. Write the block at the top level of the answer (not in a list or a quote), with only these lines; a line that does not parse makes the whole block a plain code block. contr fills the state, the PR and the checks.

When you report on several workers, a `contr-status` block, one line `<worker> | #<issue> | <what it builds>` for each:

```contr-status
b-649 | #649 | Rows and badge
b-665 | #665 | Fewer lines
```

For a merge order, a `contr-queue` block, one line `#<n> <label>` for each step in order, with ` -- <note>` for a dependency:

```contr-queue
#656 Rows and badge
#665 Fewer lines -- waits for #656
```

## Diagrams

Write a diagram only when the arrows say more than a list. Put the block at the top level of the answer, with only these lines. A block that does not parse or is too large shows as code; a larger or other diagram stays as a list or a table.

A flow, as a `contr-flow` block: one line `A -> B` or `A -> B: label` for each arrow, `A [done]` for a state (`done`, `running`, `waiting`, `failed`, `info`). No chains `A -> B -> C`. At most 12 boxes, 6 steps deep, 4 side by side, 28 characters for a name.

```contr-flow
issue -> build: plan
build -> review: PR
review -> build: blockers
review [waiting]
```

A sequence, as a `contr-seq` block: one line `A -> B: message` for each message, top to bottom; `A --> B: reply` for a reply (dashed); `A -> A: text` for a message to itself. The text is required; a name has no `:`. At most 6 actors, 16 messages, 28 characters for a name and 40 for a message.

```contr-seq
owner -> lead: fix #696
lead -> b-696: build it
b-696 --> lead: PR open
```

## What the chat does not show

- Diagrams other than `contr-flow` and `contr-seq`: a `mermaid` or other diagram block shows as code. Use a list of steps, a table, or a `contr-queue` block for an order.
- Images: `![…](…)` shows only its description. Give the path of the file as text.
- An HTML block shows as its source text. Use Markdown.
- Math shows as its TeX source. Write the formula in a code span or in words.
- Colored text: there is none. Use a remark for what needs attention.
