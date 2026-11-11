# Tokens

Generated from the design system `tokens.json`. Views use the names; `internal/app/theme` holds the values. A color or shadow has one value per theme; other tokens have one value.

## Color

Contrast numbers are for Dark unless they say Light. "On glass" is on `glass-window` over `ground`, where the sidebar and the files panel sit; "on stage" is on `stage` over that, the terminal (D250).

| Token | Dark | Light | Use |
|---|---|---|---|
| `ground` | `#0a0b0d` | `#e4e6ea` | Ground behind the window glass. Previews and the canvas use it as a stand-in for the desktop; in the app the desktop shows through the vibrancy material. |
| `glow-slate` | `#3a5a8c` | `#b2c3e0` | Canvas only: a blurred desktop glow behind the glass, top left. Never paint it in the app. |
| `glow-umber` | `#6b4a2a` | `#ebcfa9` | Canvas only: a blurred desktop glow behind the glass, bottom right. Never paint it in the app. |
| `glow-graphite` | `#2c3240` | `#cdd2db` | Canvas only: a blurred desktop glow behind the terminal. Never paint it in the app. |
| `glass-window` | `rgba(20,21,24,0.7)` | `rgba(246,246,248,0.6)` | Window fill over the vibrancy material, under the stage; the sidebar and the files panel sit on it (D250). With glass-blur and glass-saturate. The owner can tune its alpha in Settings (D162); the value here is the default. |
| `glass-float` | `rgba(34,35,40,0.72)` | `rgba(252,252,253,0.8)` | Floating glass over the terminal: the closed questions notch and the outline card. With glass-blur and glass-saturate. |
| `float-solid` | `#222328` | `#fcfcfd` | `glass-float` at full alpha: the opaque fill of floating cards with text to read, the update panel, the What's new panel, the open questions card (D212) and the projects modal (D217). Modals and dialogs never use `glass-float`. No blur. |
| `scrim` | `rgba(8,8,8,0.72)` | `rgba(20,20,20,0.18)` | Backdrop behind the projects modal. |
| `stage` | `rgba(12,13,15,0.9)` | `rgba(255,255,255,0.86)` | Fill of the terminal panel only, the stage (D250), and of the selected tab above it. The sidebar and the files panel (the wings) have no fill: they sit on `glass-window`. The owner can tune its alpha in Settings (the Panels slider, D162); the value here is the default. |
| `panel-line` | `rgba(255,255,255,0.08)` | `rgba(0,0,0,0.06)` | 1px border of the stage. The wings have no border (D250). |
| `window-line` | `rgba(255,255,255,0.09)` | `rgba(0,0,0,0.1)` | 1px border of the window. |
| `float-line` | `rgba(255,255,255,0.075)` | `rgba(0,0,0,0.08)` | 1px border of floating glass (notch: no top border). |
| `hairline` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.07)` | Lines inside floating cards and modals, and the row lines of a table in the chat (D261). The wings and the file tree draw none (D258). |
| `rail` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | Not drawn since D258 (worker rows are indented, no rail). Kept for a later cleanup of the tokens. |
| `hover` | `rgba(255,255,255,0.045)` | `rgba(0,0,0,0.04)` | Hover fill of rows, tabs, file rows and answer options; the fill of the picked option. |
| `hover-strong` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.055)` | Hover fill of icon buttons. |
| `press` | `rgba(255,255,255,0.08)` | `rgba(0,0,0,0.08)` | Hover fill of the small close button on a tab. |
| `selected` | `rgba(255,255,255,0.085)` | `rgba(0,0,0,0.065)` | The pill behind the active tab and the chosen question chip. Agent rows in the sidebar use `selected-quiet` instead (D249). |
| `selected-quiet` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | Fill of the selected agent row on the wing, with no ring (D249). Light is the black alpha whose step from the glass has the same contrast as Dark (1.19:1). |
| `selected-line` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.06)` | The 1px inner ring of the selected pill (shadow-selected). |
| `input-line` | `rgba(255,255,255,0.1)` | `rgba(0,0,0,0.13)` | 1px border of the text field in the open notch. |
| `meter-track` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | Track of the context and plan-usage meters. |
| `meter-fill` | `rgba(255,255,255,0.55)` | `rgba(0,0,0,0.5)` | Filled part of the context and plan-usage meters. |
| `tick` | `rgba(255,255,255,0.14)` | `rgba(0,0,0,0.16)` | Outline ticks for messages already seen. 1.5:1 on glass: a mark, not text. |
| `ink-100` | `#f4f4f5` | `#141414` | The strongest ink, for what is chosen or needs the owner: the active tab, the selected agent, the title of a worker that needs the owner, new outline ticks and their count, the question text, the closed notch label (D268). 17.0:1 on glass (Light 16.2:1). |
| `ink-88` | `rgba(255,255,255,0.88)` | `rgba(0,0,0,0.86)` | Text: default text in the window, live rows (working agents, changed files), project names, answer option labels, the terminal cursor block (D268). 14.5:1 on glass (Light 14.0:1). |
| `ink-64` | `rgba(255,255,255,0.64)` | `rgba(0,0,0,0.7)` | Rows at rest: sidebar rows at rest, values in the info block, the meta line of the selected agent, the closed notch text (D268). 8.0:1 on glass (Light 8.1:1). |
| `ink-48` | `rgba(255,255,255,0.48)` | `rgba(0,0,0,0.57)` | Meta and labels: tabs and file rows at rest, panel headings, agent meta, counts, key hints, times, placeholders, dim terminal lines (D268). At least 4.5:1 on each surface where text sits, in both themes (theme test): 4.95:1 on glass, 4.98:1 on stage, 4.61:1 on `selected` (Light 4.90:1, 5.09:1, 4.69:1 on `selected-quiet`). |
| `ink-30` | `rgba(255,255,255,0.3)` | `rgba(0,0,0,0.38)` | Marks only, never text (D268): icons at rest (file icons, the icon of an empty project), the idle glyph and dots, the quote bar, the scrollbar, the split divider, a disabled icon, the drag grip. 2.7:1 on glass (Light 2.6:1). |
| `terminal-text` | `rgba(255,255,255,0.82)` | `rgba(0,0,0,0.815)` | Default terminal text before the agent CLI paints its own colors. 13.0:1 on stage. |
| `blocked` | `#f07178` | `#c73947` | The one hue in the chrome: red for an agent that is blocked or asks the owner. Loader glyphs, the Claude glyph and the notch dot. Never a fill or a ring. Text in red uses `blocked-text` (D249), except the `N need you` meta of the projects modal (D207). 6.5:1 on glass, 6.8:1 on stage, 5.8:1 on glass-float. Light: #c73947, 4.5:1 on glass, 5.0:1 on stage. |
| `blocked-text` | `#f07178` | `#b1333f` | Red words on the wings (D249): "needs you" on line 2 of a worker row and the item count of an orchestrator row. Dark is `blocked`. Light is the nearest darker `blocked` with 4.5:1 on `glass-window` and on `selected-quiet` over it (`blocked` has 4.49:1 on glass). Dark 6.5:1 on glass, 5.5:1 on selected-quiet; Light 5.4:1 and 4.5:1. |
| `claude` | `#d97757` | `#c15f3c` | Stroke of the Claude agent glyph (orchestrators) while it works or idles. 6.0:1 on glass. Light: #c15f3c, 3.7:1 on glass (#d97757 is only 2.9:1 on light). |
| `focus-ring` | `rgba(255,255,255,0.5)` | `rgba(0,0,0,0.45)` | The 1px keyboard focus ring of every button, 2px outside it. |
| `lights` | `rgba(255,255,255,0.14)` | `rgba(0,0,0,0.14)` | Previews only: the traffic-light stand-ins. macOS draws the real ones. |

### Content

The content palette (D261) colors what Claude writes in the chat, never the chrome. A content color shows only as a dot, a word, a 2px bar or a 1px line: never a fill and never a row. Diff lines are the one exception: a line added or removed in a diff block has a tint of its color (`DiffTint`, the color at 9% alpha). Each color has 4.5:1 as text on the stage; the code colors on a code block (`hover` over the stage); `content-done` and `content-failed` also on their diff tint. The Light values of the canvas board were estimates: `content-done` is #1e763b, not #1f7a3d (4.3:1 on its tint), and `content-failed` is `blocked-text`, not `blocked` (4.1:1 on its tint).

| Token | Dark | Light | Use |
|---|---|---|---|
| `content-done` | `#7fb88a` | `#1e763b` | Done, merged, passed: the Done remark, added diff lines, the dot of a chip whose PR passed or merged or whose worker is done (D262). Dark 8.4:1 on stage, 6.7:1 on its tint; Light 5.6:1 and 4.5:1. |
| `content-running` | `#7aa7e6` | `#1f5fbf` | Work in progress: the dot of a chip whose PR checks run or whose worker works (D262). Dark 7.9:1, Light 6.0:1 on stage. |
| `content-waiting` | `#d9b36a` | `#8a5a00` | Waits, at risk: the Warning remark, the dot of a chip whose worker waits (D262). Dark 9.8:1, Light 5.8:1 on stage. |
| `content-failed` | `#f07178` | `#b1333f` | Failed, needs the owner: the Decision needed remark, removed diff lines, the dot of a chip whose PR failed or whose worker needs the owner (D262). The chrome's red text (`blocked-text`). Dark 6.8:1 on stage, 5.6:1 on its tint; Light 6.0:1 and 4.9:1. |
| `content-info` | `rgba(255,255,255,0.64)` | `rgba(0,0,0,0.7)` | Neutral remarks: Note, Not verified; hunk lines of a diff; the dot of a chip whose PR is open with no checks or closed (D262). Dark 8.2:1, Light 8.6:1 on stage. |
| `code-keyword` | `#b79ce6` | `#8250df` | Keywords in code blocks. Dark 7.6:1, Light 4.5:1 on a code block. |
| `code-function` | `#86b4ee` | `#1f5fbf` | Functions, types, tags. Dark 8.3:1, Light 5.5:1 on a code block. |
| `code-string` | `#9cc58f` | `#1e763b` | Strings, escapes, regular expressions. Dark 9.2:1, Light 5.1:1 on a code block. |
| `code-number` | `#e0b36a` | `#8a5a00` | Numbers and constants. Dark 9.2:1, Light 5.3:1 on a code block. |
| `code-comment` | `rgba(255,255,255,0.48)` | `rgba(0,0,0,0.586)` | Comments, in italic. Dark 4.9:1, Light 5.3:1 on a code block. |

## ANSI

The 16 colors a program in the terminal asks for (SGR 30-37, 90-97, palette 0-15). The terminal plugin takes the palette of the theme (docs/spec/Terminal.md); a theme change swaps it with no reconnect. Dark is the v0.2 `TerminalPalette`. Light is Ghostty's GitHub Light Default tuned for contrast on the light stage (D129): the dim yellow is a dark ochre, and white and bright white are mid and dark greys, because a light grey would not show on a light stage. Each Light color has at least 4.5:1 on the flattened stage (`stage` over `glass-window` over `ground`: #fdfdfd in Light, #0c0d10 in Dark, D250); black has 14.4:1. Dark black and bright black are below 4.5:1 on purpose: programs use them as fills and dim text, as in the v0.2 palette.

| Token | Dark | Light | Contrast on stage |
|---|---|---|---|
| `ansi-black` | `#1e1e1e` | `#24292f` | Dark 1.2:1, Light 14.4:1 on stage. |
| `ansi-red` | `#e06c6c` | `#cf222e` | Dark 6.0:1, Light 5.3:1 on stage. |
| `ansi-green` | `#8cc07a` | `#116329` | Dark 9.2:1, Light 7.3:1 on stage. |
| `ansi-yellow` | `#e0c070` | `#7d4e00` | Dark 11.1:1, Light 7.0:1 on stage. |
| `ansi-blue` | `#6ca8e8` | `#0969da` | Dark 7.8:1, Light 5.1:1 on stage. |
| `ansi-magenta` | `#c080d8` | `#8250df` | Dark 6.8:1, Light 5.0:1 on stage. |
| `ansi-cyan` | `#5cc0c8` | `#1a7980` | Dark 9.1:1, Light 5.0:1 on stage. |
| `ansi-white` | `#d0d0d0` | `#656d76` | Dark 12.6:1, Light 5.2:1 on stage. |
| `ansi-bright-black` | `#5c5c5c` | `#57606a` | Dark 2.9:1, Light 6.3:1 on stage. |
| `ansi-bright-red` | `#ff8080` | `#a40e26` | Dark 8.0:1, Light 7.7:1 on stage. |
| `ansi-bright-green` | `#a8e090` | `#1a7f37` | Dark 12.7:1, Light 5.0:1 on stage. |
| `ansi-bright-yellow` | `#ffd890` | `#633c01` | Dark 14.3:1, Light 9.5:1 on stage. |
| `ansi-bright-blue` | `#90c4ff` | `#0550ae` | Dark 10.7:1, Light 7.5:1 on stage. |
| `ansi-bright-magenta` | `#dca0f0` | `#6639ba` | Dark 9.6:1, Light 7.2:1 on stage. |
| `ansi-bright-cyan` | `#80dce4` | `#0e6b78` | Dark 12.3:1, Light 6.1:1 on stage. |
| `ansi-bright-white` | `#ffffff` | `#424a53` | Dark 19.4:1, Light 8.8:1 on stage. |

## Type

- sans: `Inter, -apple-system, system-ui, sans-serif`
- mono: `"JetBrains Mono", ui-monospace, monospace`
- Inter (regular 400, medium 500) and JetBrains Mono are bundled in
  `assets/fonts` (D121); the rest of each stack is the fallback. In code:
  `theme.Style(name)`, `TextStyle.Family()`, `TextStyle.Stack()`.

| Style | Family | Size | Line | Weight | Use |
|---|---|---|---|---|---|
| `question` | sans | 14px | 20px | 400 | The question in the open notch; the headline on the outline card. |
| `prose` | sans | 13.5px | 21px | 400 | The chat view: the owner's prompts and Claude's Markdown (docs/spec/ChatView.md). |
| `body` | sans | 13px | 18px | 400 | Sidebar rows, answer option labels. The window default. |
| `body-strong` | sans | 13px | 18px | 500 | Project names in the sidebar. |
| `row-small` | sans | 12.5px | 18px | 400 | Tabs, file rows, the outline card detail, the notch text field. |
| `small` | sans | 12px | 17px | 400 | Answer option descriptions, the question context, info labels, row meta. |
| `label` | sans | 12px | 18px | 500 | Panel headings: Projects, Servers, the files root. |
| `chip` | sans | 11.5px | 16px | 400 | Question chips in the open notch; "via orchestrator". |
| `terminal` | mono | 13px | 20px | 400 | Terminal grid. The terminal plugin sets it, not the view. |
| `value` | mono | 11.5px | 16px | 400 | Title-bar change summary; values in the info block. |
| `meta` | mono | 11px | 16px | 400 | Agent meta line, file diff counts, key hints, option numbers, card time. |
| `count` | mono | 11px | 14px | 500 | Count of new orchestrator messages above the outline ticks. |
| `badge` | mono | 11px | 16px | 600 | The number of open items of an orchestrator row, in `blocked-text`, with no pill (D240, D249). |

## Spacing

| Token | Value | Use |
|---|---|---|
| `space-2` | `2px` | Gap between sidebar rows and between tabs. |
| `space-4` | `4px` | Gap above nested worker rows; heading padding. |
| `space-6` | `6px` | Gap inside the file-row diff counts; padding of the outline tick column. |
| `space-7` | `7px` | Gap between icon and name in a file row; info block line gap. |
| `space-8` | `8px` | Gap in tabs and the title bar; title bar right padding. |
| `space-9` | `9px` | Gap between outline ticks; rail indent inside agent groups. |
| `space-10` | `10px` | Gutter between and around panels; row padding (file rows too); gap between projects. |
| `space-12` | `12px` | Panel top padding; terminal bottom padding; option padding; gap on the outline card. |
| `space-14` | `14px` | Terminal top padding; padding in the projects modal and the update panel. |
| `space-16` | `16px` | Indent of a file-tree level (with `space-4`); outline card padding left; gap in the title-bar info. |
| `space-18` | `18px` | Title bar left inset for the window buttons. |
| `space-20` | `20px` | Terminal side padding: content sits 20px from a panel side in every panel (D228). |

## Radius

| Token | Value | Use |
|---|---|---|
| `radius-1` | `1px` | Outline ticks. |
| `radius-2` | `2px` | Meter track and fill. |
| `radius-4` | `4px` | The close button on a tab. |
| `radius-6` | `6px` | File rows; question chips. |
| `radius-7` | `7px` | The title-bar change summary button. |
| `radius-8` | `8px` | Sidebar rows, tabs, icon buttons, text fields, the focus ring. |
| `radius-9` | `9px` | Answer options in the open notch. |
| `radius-12` | `12px` | Panels; floating glass (notch: bottom corners only). |
| `radius-14` | `14px` | The window. |

## Size

| Token | Value | Use |
|---|---|---|
| `titlebar-height` | `46px` | Title bar, drawn by the view, with the tabs: 8px above and below the 30px tab row (D177). |
| `sidebar-width` | `312px` | Sidebar panel at rest; the owner can drag it. |
| `files-width` | `280px` | Files panel at rest; the owner can drag it. |
| `row-height` | `30px` | One-line sidebar rows and tabs. |
| `file-row-height` | `26px` | File-tree rows. |
| `icon-button` | `28px` | Icon buttons (24px in the footer, 26px for new tab). |
| `tab-max-width` | `180px` | Widest a tab grows; the title clips with an ellipsis. |
| `notch-height` | `30px` | Closed questions notch. |
| `notch-open-width` | `600px` | Open questions notch. |
| `outline-card-width` | `380px` | Outline hover card. |
| `tick-height` | `2px` | One outline tick. |
| `meter-height` | `4px` | Context and usage meters. |
| `thumb` | `64px` | Image thumbnails in the chat (D269). |

## Effect

| Token | Value | Use |
|---|---|---|
| `glass-blur` | `48px` | Backdrop blur of glass-window and glass-float. |
| `glass-saturate` | `140%` | Backdrop saturation of glass-window and glass-float. |

## Shadow

| Token | Dark | Light | Use |
|---|---|---|---|
| `shadow-window-top` | `inset 0 1px 0 rgba(255,255,255,0.05)` | `inset 0 1px 0 rgba(255,255,255,0.7)` | Top highlight inside the window border. |
| `shadow-selected` | `inset 0 0 0 1px rgba(255,255,255,0.06)` | `inset 0 0 0 1px rgba(0,0,0,0.06)` | Ring of the selected pill. |
| `shadow-float` | `0 12px 32px rgba(0,0,0,0.28)` | `0 12px 32px rgba(0,0,0,0.12)` | Outline card. |
| `shadow-notch` | `inset 0 -1px 0 rgba(255,255,255,0.03), 0 12px 32px rgba(0,0,0,0.28)` | `inset 0 -1px 0 rgba(255,255,255,0.6), 0 12px 32px rgba(0,0,0,0.12)` | Questions notch, closed and open. |

## Motion

Short transitions (D256, D263, [Motion.md](Motion.md)). Each one eases out, unless its row gives another easing, and ends; after its end no frame is drawn (D55).

| Token | Value | Use |
|---|---|---|
| `motion-select` | `120ms` | The fill and inks of a row, tab or switch segment that is selected. |
| `motion-pop-in` | `140ms` | A popover or a modal comes in: scale 0.96 to 1 and a fade. |
| `motion-pop-out` | `100ms` | A popover or a modal goes. |
| `motion-state` | `300ms` | The title ink and the "needs you" words of a worker row that turns to needs you. |
| `motion-number` | `200ms` | The digits of a value roll. |
| `motion-meter` | `300ms` | The fill of a meter moves to its new width. |
| `motion-tab` | `180ms` | The pill of the selected tab slides to the tab selected now (D263). |
| `motion-panel` | `220ms` | The sidebar or the files panel comes in as the owner shows it: 24px and a fade, along cubic-bezier(.2, 0, 0, 1) (D263). |
| `motion-notch-open` | `260ms` | The questions card grows from the notch (D263). |
| `motion-notch-close` | `160ms` | The notch shrinks back from the card, easing in (D263). |
| `motion-row-in` | `200ms` | An agent row that arrives comes down 4px with a fade (D263). |
