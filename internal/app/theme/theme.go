// Package theme holds the design tokens of docs/DESIGN.md. The UI is
// greyscale: white means the owner acts, greys mean everything else. Views
// use these constants and have no hex colors of their own.
//
// Ported from the Rust contr theme module. Sizes are in DIPs and colors are
// 0xRRGGBB values; Color turns one into a MyGo color.
package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Color returns the MyGo color for a 0xRRGGBB token.
func Color(rgb uint32) ui.Color {
	return ui.RGB(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb))
}

// FontFamily is the family of every font in the UI.
const FontFamily = "JetBrains Mono"

// Font
const FontSize = 12
const LineHeight = 18
const TerminalFontSize = 13
const TerminalLineHeight = 20
const TermPadding = 8 // space between the terminal view's edge and its grid

// Backgrounds
const BgWindow = 0x141414 // window, panel
const BgBars = 0x101010   // sidebar, title bar, status bar
const BgTerminal = 0x121212
const BgCard = 0x1C1C1C     // selected row
const CardBorder = 0x2C2C2C // 1px inset
const CardRadius = 7
const BgAnswer = 0x1A1A1A // answer card
const AnswerBorder = 0x262626
const AnswerRadius = 10
const Line = 0x222222 // dividers

// Text
const Text = 0xEDEDED     // primary
const Text2 = 0xBDBDBD    // secondary
const TextDim = 0x7A7A7A  // labels
const TextDim2 = 0x6A6A6A // meta
const YouBg = 0xEDEDED    // anything waiting on the owner
const YouText = 0x111111  // bold, on YOU_BG

// Status icons (16px viewBox)
const IconSize = 14
const IconSizeSmall = 12
const IconYou = YouBg      // owner acts: needs you, decide, do, blocked, working arc
const IconGrey = TextDim   // waiting, idle, done, fyi
const IconFaint = 0x4A4A4A // not started, working ring (owner-decided default)
// SpinPeriod is one turn of "working".
const SpinPeriod = 1400 * time.Millisecond

// SpinFPS is how often the working arc paints a second. At the display's
// rate the arc was the whole idle CPU of the app (M13.4, D26).
const SpinFPS = 30

// Window and frame sizes
const WindowWidth = 1440
const WindowHeight = 900
const SidebarWidth = 280
const TitleBarHeight = 40
const PanelWidth = 440
const StatusBarHeight = 26

// Sidebar spaces (planner defaults; docs/DESIGN.md has no sizes for these)
const SidebarPadX = 12       // left/right padding of sidebar content
const TrafficLightInset = 78 // room for the native window buttons (x 14, 3×12 + 2×8 gaps, + 12)
const SpacesRowHeight = 26   // one-line row: section header, repo, collapsed host
const WorktreeRowHeight = 42 // two-line worktree row (18 + 18 + 6)
const RowIndent = 14         // worktree rows under their repo
const CardInsetX = 4         // selected-card margin (same 4 px as the agent rows)
const BadgePadX = 5
const BadgeRadius = 4
const HostDotSize = 6 // herdr connection dot on the host card
const SpacesGap = 8   // icon to text, name to branch, between a row's parts
const MemBarWidth = 60
const MemBarHeight = 4
const SidebarToggleBox = 16   // clickable box around the toggle drawing
const SidebarToggleWidth = 14 // toggle outline, border included
const SidebarToggleHeight = 12
const SidebarToggleRadius = 2
const SidebarToggleFill = 4 // filled left part of the outline

// Agents list (planner defaults; docs/DESIGN.md has no sizes for these)
const AgentsHeaderHeight = 28 // "Agents" and "N running"
const AgentRowHeight = 28     // Rust D42: one line per agent
const AgentRowPadX = 8
const AgentGap = 8          // icon to name, name to detail, detail to link
const AgentsMaxShare = 0.45 // share of the sidebar height (Rust D46)

// Status bar (planner defaults; docs/DESIGN.md has no sizes for these)
const StatusPadX = 12    // left and right padding
const StatusGap = 8      // between label, bar and percent, and around the "·" separators
const UsageBarWidth = 36 // plan usage track
const UsageBarHeight = 4
const UsageBarRadius = 2
const UsageLowLeft = 20 // under this % left the fill and percent are white (owner acts)

// Title bar tabs and buttons (planner defaults; docs/DESIGN.md has no sizes for these)
const TabPaddingX = 12
const TabGap = 6 // icon to label
const TabMaxWidth = 180

// TabMinWidth: when the tabs do not fit, each tab shrinks to this width and
// its label ends in an ellipsis, then the tabs row clips the rest (Rust D62).
const TabMinWidth = 72
const TabUnderline = 2     // active tab, bottom edge
const TitleButtonSize = 28 // "+", split, panel toggle: square hit areas
const TitleGlyphWidth = 14 // split and panel glyphs, border included
const TitleGlyphHeight = 12
const TitleBarPadEnd = 8 // title bar: room right of the panel toggle
const TitleGlyphFill = 4 // panel glyph: filled right part when the panel is open

// Board panel (planner defaults; docs/DESIGN.md has no sizes for these). The "you" chip reuses
// BADGE_PAD_X and BADGE_RADIUS.
const PanelPadX = 12
const PanelHeaderHeight = 36 // Board | Report switch and the wrap label
const BoardLineHeight = 28   // summary, section heads, needs-you rows
const NeedKeyWidth = 29      // four characters of JetBrains Mono 12 px
const NeedGap = 8            // needs-you head and rows: between their parts
const WorkRowHeight = 42     // two text lines (18 + 18) + 6
const StageColWidth = 44     // DESIGN.md: five 44 px stage columns
const WorkLabelWidth = 52    // "#123456"
const SegmentPadX = 8        // Board | Report segment
const SegmentPadY = 4
const SegmentRadius = 5
const BoardGap = 8 // "Work" to "clear done"

// Answer card (DESIGN.md fixes the height at 168; the rest are planner defaults)
const AnswerCardHeight = 168
const AnswerPad = 12                     // inside the card's border
const AnswerGap = 8                      // between the card's head, text, actions and foot lines
const AnswerTextHeight = LineHeight * 3. // the text takes at most three lines
const AnswerButtonHeight = 24
const AnswerButtonPadX = 10
const AnswerButtonRadius = 6
const AnswerButtonGap = 8 // between two buttons
const AnswerMargin = 8    // card to panel edges

// Report tab (planner defaults; docs/DESIGN.md has no sizes for these)
const ReportGap = 12         // between "Open in editor" and "back"
const ReportIndent = 14      // per list depth (same as ROW_INDENT)
const ReportMarkerWidth = 22 // "•" or "10." column
const ReportBlockGap = 9     // above level 1 and 2 headings
const ReportBlankHeight = 9  // half a line
const ReportBodyPadTop = 6   // between the divider and the first line
const ReportCodePadX = 6     // inside a code line's background
const ReportCodeBg = BgBars  // grey
const ReportQuotePad = 8     // between a quote's rule and its text
const ReportRuleMargin = 8   // above and below a horizontal rule
