# Tokens

Generated from the design system `tokens.json`. Views use the names; `internal/app/theme` holds the values. A color or shadow has one value per theme; other tokens have one value.

## Color

| Token | Dark | Light | Use |
|---|---|---|---|
| `ground` | `#0a0b0d` | `#e4e6ea` | Ground behind the window glass. Previews and the canvas use it as a stand-in for the desktop; in the app the desktop shows through the vibrancy material. |
| `glow-slate` | `#3a5a8c` | `#b2c3e0` | Canvas only: a blurred desktop glow behind the glass, top left. Never paint it in the app. |
| `glow-umber` | `#6b4a2a` | `#ebcfa9` | Canvas only: a blurred desktop glow behind the glass, bottom right. Never paint it in the app. |
| `glow-graphite` | `#2c3240` | `#cdd2db` | Canvas only: a blurred desktop glow behind the terminal. Never paint it in the app. |
| `glass-window` | `rgba(20,21,24,0.3)` | `rgba(246,246,248,0.6)` | Window fill over the vibrancy material, under every panel. With glass-blur and glass-saturate. The owner can tune its alpha in Settings (D162); the value here is the default. |
| `glass-float` | `rgba(34,35,40,0.72)` | `rgba(252,252,253,0.8)` | Floating glass over the terminal: the questions notch and the outline card. With glass-blur and glass-saturate. |
| `panel` | `rgba(20,21,24,0.55)` | `rgba(255,255,255,0.7)` | Fill of the three panels: sidebar, terminal, files. The owner can tune its alpha in Settings (D162); the value here is the default. |
| `panel-line` | `rgba(255,255,255,0.065)` | `rgba(0,0,0,0.06)` | 1px border of the three panels. |
| `window-line` | `rgba(255,255,255,0.09)` | `rgba(0,0,0,0.1)` | 1px border of the window. |
| `float-line` | `rgba(255,255,255,0.075)` | `rgba(0,0,0,0.08)` | 1px border of floating glass (notch: no top border). |
| `hairline` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.07)` | Dividers inside a panel (above the info block and the footer) and the file-tree indent guides. |
| `rail` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | The 1px rail left of an orchestrator's worker rows. |
| `hover` | `rgba(255,255,255,0.045)` | `rgba(0,0,0,0.04)` | Hover fill of rows, tabs, file rows and answer options; the fill of the picked option. |
| `hover-strong` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.055)` | Hover fill of icon buttons. |
| `press` | `rgba(255,255,255,0.08)` | `rgba(0,0,0,0.08)` | Hover fill of the small close button on a tab. |
| `selected` | `rgba(255,255,255,0.085)` | `rgba(0,0,0,0.065)` | The pill behind the active tab and the chosen question chip. Rows in the sidebar never get it: they turn ink-100 instead. |
| `selected-line` | `rgba(255,255,255,0.06)` | `rgba(0,0,0,0.06)` | The 1px inner ring of the selected pill (shadow-selected). |
| `input-line` | `rgba(255,255,255,0.1)` | `rgba(0,0,0,0.13)` | 1px border of the text field in the open notch. |
| `meter-track` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | Track of the context and plan-usage meters. |
| `meter-fill` | `rgba(255,255,255,0.55)` | `rgba(0,0,0,0.5)` | Filled part of the context and plan-usage meters. |
| `tick` | `rgba(255,255,255,0.14)` | `rgba(0,0,0,0.16)` | Outline ticks for messages already seen. 1.5:1 on panel: a mark, not text. |
| `ink-100` | `#f4f4f5` | `#141414` | The strongest ink: white in dark, near-black in light. Only what needs the owner or what is chosen: the active tab, the selected agent, new outline ticks and their count, the question text, the closed notch label. 17.1:1 on panel, 15.1:1 on glass-float. |
| `ink-95` | `#e9e9ea` | `#1e1e1e` | Default text in the window and the terminal cursor block. 15.5:1 on panel. |
| `ink-90` | `rgba(255,255,255,0.9)` | `rgba(0,0,0,0.873)` | Changed and new file names; answer option labels. 15.2:1 on panel. |
| `ink-85` | `rgba(255,255,255,0.85)` | `rgba(0,0,0,0.836)` | Project names in the sidebar. 13.6:1 on panel. |
| `ink-62` | `rgba(255,255,255,0.62)` | `rgba(0,0,0,0.679)` | Values in the info block; the meta line of the selected agent. 7.6:1 on panel. |
| `ink-60` | `rgba(255,255,255,0.6)` | `rgba(0,0,0,0.665)` | Sidebar rows at rest; the closed notch text. 7.2:1 on panel. |
| `ink-50` | `rgba(255,255,255,0.5)` | `rgba(0,0,0,0.586)` | Tabs at rest, icon buttons at rest, file rows at rest. 5.3:1 on panel. |
| `ink-45` | `rgba(255,255,255,0.45)` | `rgba(0,0,0,0.543)` | Panel headings (Projects, Servers, the files root), question chips at rest, answer option descriptions, the tab close glyph. 4.53:1 on panel, just above 4.5:1; 4.4:1 on glass-float, just below 4.5:1. Kept from the canvas. |
| `ink-42` | `rgba(255,255,255,0.42)` | `rgba(0,0,0,0.515)` | Secondary lines: agent meta, title-bar info, info labels, outline card detail, dim terminal lines. 4.1:1 on panel: below 4.5:1, kept from the canvas. |
| `ink-40` | `rgba(255,255,255,0.4)` | `rgba(0,0,0,0.495)` | Added-line counts in the file tree. 3.8:1 on panel: below 4.5:1, kept from the canvas. |
| `ink-38` | `rgba(255,255,255,0.38)` | `rgba(0,0,0,0.475)` | Row meta on the right (branch, server state) and file icons. 3.6:1 on panel: below 4.5:1, kept from the canvas. |
| `ink-35` | `rgba(255,255,255,0.35)` | `rgba(0,0,0,0.444)` | The "via orchestrator" note in the open notch. 3.2:1: below 4.5:1, kept from the canvas. |
| `ink-32` | `rgba(255,255,255,0.32)` | `rgba(0,0,0,0.41)` | Key hints and option numbers in the open notch. 2.9:1: below 4.5:1, kept from the canvas. |
| `ink-30` | `rgba(255,255,255,0.3)` | `rgba(0,0,0,0.387)` | Removed-line count in the title bar, the idle loader, the text-field placeholder, the time on the outline card. 2.7:1: below 4.5:1, kept from the canvas. |
| `ink-26` | `rgba(255,255,255,0.26)` | `rgba(0,0,0,0.339)` | Project meta (branch). 2.3:1: below 4.5:1, kept from the canvas. |
| `ink-24` | `rgba(255,255,255,0.24)` | `rgba(0,0,0,0.313)` | Deleted file names (struck through) and removed-line counts in the file tree. 2.2:1: dim on purpose, kept from the canvas. |
| `ink-22` | `rgba(255,255,255,0.22)` | `rgba(0,0,0,0.287)` | Icon and counts of a deleted file. 2:1: dim on purpose, kept from the canvas. |
| `terminal-text` | `rgba(255,255,255,0.82)` | `rgba(0,0,0,0.815)` | Default terminal text before the agent CLI paints its own colors. 12.7:1 on panel. |
| `blocked` | `#f07178` | `#c73947` | The one hue in the chrome: red for an agent that is blocked or asks the owner. Loader glyphs and the Claude glyph only, never fills or text runs. 6.5:1 on panel, 5.8:1 on glass-float. Light: #c73947, 4.9:1 on panel. |
| `claude` | `#d97757` | `#c15f3c` | Stroke of the Claude agent glyph (orchestrators) while it works or idles. 6.0:1 on panel. Light: #c15f3c, 4.1:1 on panel (#d97757 is only 2.9:1 on light). |
| `focus-ring` | `rgba(255,255,255,0.5)` | `rgba(0,0,0,0.45)` | The 1px keyboard focus ring of every button, 2px outside it. |
| `lights` | `rgba(255,255,255,0.14)` | `rgba(0,0,0,0.14)` | Previews only: the traffic-light stand-ins. macOS draws the real ones. |

## ANSI

The 16 colors a program in the terminal asks for (SGR 30-37, 90-97, palette 0-15). The terminal plugin takes the palette of the theme (docs/spec/Terminal.md); a theme change swaps it with no reconnect. Dark is the v0.2 `TerminalPalette`. Light is Ghostty's GitHub Light Default tuned for contrast on the light panel (D129): the dim yellow is a dark ochre, and white and bright white are mid and dark greys, because a light grey would not show on a light panel. Each Light color has at least 4.5:1 on the flattened panel (`panel` over `glass-window` over `ground`: #f7f7f9 in Light, #111214 in Dark); black has 13.7:1. Dark black and bright black are below 4.5:1 on purpose: programs use them as fills and dim text, as in the v0.2 palette.

| Token | Dark | Light | Contrast on panel |
|---|---|---|---|
| `ansi-black` | `#1e1e1e` | `#24292f` | Dark 1.1:1, Light 14.1:1 on panel. |
| `ansi-red` | `#e06c6c` | `#cf222e` | Dark 5.8:1, Light 5.2:1 on panel. |
| `ansi-green` | `#8cc07a` | `#116329` | Dark 8.9:1, Light 7.1:1 on panel. |
| `ansi-yellow` | `#e0c070` | `#7d4e00` | Dark 10.7:1, Light 6.8:1 on panel. |
| `ansi-blue` | `#6ca8e8` | `#0969da` | Dark 7.5:1, Light 5.0:1 on panel. |
| `ansi-magenta` | `#c080d8` | `#8250df` | Dark 6.5:1, Light 4.9:1 on panel. |
| `ansi-cyan` | `#5cc0c8` | `#1a7980` | Dark 8.8:1, Light 5.0:1 on panel. |
| `ansi-white` | `#d0d0d0` | `#656d76` | Dark 12.2:1, Light 5.1:1 on panel. |
| `ansi-bright-black` | `#5c5c5c` | `#57606a` | Dark 2.8:1, Light 6.2:1 on panel. |
| `ansi-bright-red` | `#ff8080` | `#a40e26` | Dark 7.7:1, Light 7.6:1 on panel. |
| `ansi-bright-green` | `#a8e090` | `#1a7f37` | Dark 12.3:1, Light 4.9:1 on panel. |
| `ansi-bright-yellow` | `#ffd890` | `#633c01` | Dark 13.8:1, Light 9.3:1 on panel. |
| `ansi-bright-blue` | `#90c4ff` | `#0550ae` | Dark 10.3:1, Light 7.3:1 on panel. |
| `ansi-bright-magenta` | `#dca0f0` | `#6639ba` | Dark 9.2:1, Light 7.1:1 on panel. |
| `ansi-bright-cyan` | `#80dce4` | `#0e6b78` | Dark 11.8:1, Light 6.0:1 on panel. |
| `ansi-bright-white` | `#ffffff` | `#424a53` | Dark 18.7:1, Light 8.7:1 on panel. |

## Type

- sans: `Inter, -apple-system, system-ui, sans-serif`
- mono: `"JetBrains Mono", ui-monospace, monospace`
- Inter (regular 400, medium 500) and JetBrains Mono are bundled in
  `assets/fonts` (D121); the rest of each stack is the fallback. In code:
  `theme.Style(name)`, `TextStyle.Family()`, `TextStyle.Stack()`.

| Style | Family | Size | Line | Weight | Use |
|---|---|---|---|---|---|
| `question` | sans | 14px | 20px | 400 | The question in the open notch; the headline on the outline card. |
| `body` | sans | 13px | 18px | 400 | Sidebar rows, answer option labels. The window default. |
| `body-strong` | sans | 13px | 18px | 500 | Project names in the sidebar. |
| `row-small` | sans | 12.5px | 18px | 400 | Tabs, file rows, the outline card detail, the notch text field. |
| `small` | sans | 12px | 17px | 400 | Answer option descriptions, the question context, info labels, row meta. |
| `label` | sans | 12px | 18px | 500 | Panel headings: Projects, Servers, the files root. |
| `chip` | sans | 11.5px | 16px | 400 | Question chips in the open notch; "via orchestrator". |
| `terminal` | mono | 13px | 20px | 400 | Terminal grid. The terminal plugin sets it, not the view. |
| `glyph` | mono | 17px | 18px | 400 | Braille status glyph in sidebar rows; 14px in tabs and the notch. |
| `value` | mono | 11.5px | 16px | 400 | Title-bar change summary; values in the info block. |
| `meta` | mono | 11px | 16px | 400 | Agent meta line, file diff counts, key hints, option numbers, card time. |
| `count` | mono | 11px | 14px | 500 | Count of new orchestrator messages above the outline ticks. |

## Spacing

| Token | Value | Use |
|---|---|---|
| `space-2` | `2px` | Gap between sidebar rows and between tabs. |
| `space-4` | `4px` | Gap above nested worker rows; heading padding. |
| `space-6` | `6px` | Gap inside the file-row diff counts; padding of the outline tick column. |
| `space-7` | `7px` | Gap between icon and name in a file row; info block line gap. |
| `space-8` | `8px` | Gap in tabs and the title bar; file row padding. |
| `space-9` | `9px` | Gap between outline ticks; rail indent inside agent groups. |
| `space-10` | `10px` | Gutter between and around panels; row padding; gap between projects. |
| `space-12` | `12px` | Panel top padding; option padding; gap on the outline card. |
| `space-14` | `14px` | Title bar right padding; indent of a file-tree level. |
| `space-16` | `16px` | Outline card padding left; gap in the title-bar info. |
| `space-18` | `18px` | Title bar left inset for the window buttons; terminal top padding. |
| `space-26` | `26px` | Terminal side padding. |

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
| `titlebar-height` | `46px` | Title bar, drawn by the view, with the tabs. |
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
