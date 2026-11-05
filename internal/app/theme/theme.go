// Package theme holds the design tokens of docs/DESIGN.md. The UI is
// greyscale: white means the owner acts, greys mean everything else. Views
// use these constants and have no hex colors of their own.
//
// Ported from the Rust contr theme module. Sizes are in DIPs and colors are
// 0xRRGGBB values; Color turns one into a MyGo color.
//
// OLD (v0.2): the constants in this file are the old tokens of docs/DESIGN.md.
// The from-zero redesign (D118, D119) replaces them with the palettes in
// palette.go, the scales in scale.go and the theme wiring in follow.go. Each
// view keeps its old constants until its own issue of tracker #333 moves it
// to the new tokens; then the constants it used are deleted.
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

// TerminalLineHeightScale is the row height of the terminal as a multiple of
// the font's own line height: TerminalLineHeight over JetBrains Mono's 1.32
// em at TerminalFontSize.
const TerminalLineHeightScale = 1.165

// Backgrounds
const BgWindow = 0x141414   // window, panel
const BgBars = 0x101010     // sidebar, title bar, status bar
const BgCard = 0x1C1C1C     // selected row
const CardBorder = 0x2C2C2C // 1px inset
const CardRadius = 7
const Line = 0x222222 // dividers
const LineStrong = 0x3A3A3A

// Segmented control (DESIGN.md, PR viewer tabs): a track and the selected
// segment.
const SegmentTrack = 0x1A1A1A
const SegmentSelected = 0x2A2A2A
const SegmentRadius = 7
const SegmentPad = 2

// State badge of a PR (DESIGN.md, PR viewer header line 3).
const BadgeApprovedBorder = 0x5A5A5A

// Diff colors: changes in a code view (D34). The only colors outside the
// greyscale, because a diff needs them; from the approved Review design.
const DiffAdded = 0xA6D189
const DiffDeleted = 0xF07178

// Failed is the red of failed and blocked (D76). Settings shows update errors in it (D115);
// the clone error uses it too.
const Failed = 0xF07178

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
const SidebarWidth = SizeSidebarWidth
const TitleBarHeight = SizeTitlebarHeight
const PanelWidth = SizeFilesWidth

// Limits of the widths the owner can drag (D111).
const (
	SidebarMinWidth = 200
	SidebarMaxWidth = 480
	PanelMinWidth   = 220
	PanelMaxWidth   = 720
)

// Window rules (docs/spec/Window.md). TerminalMinWidth is the least the
// terminal keeps. Below FilesHideBelow window width the files panel hides
// (O6); the owner's toggle keeps its state. PanelGap is the space between
// panels and the window edge.
const (
	TerminalMinWidth = 480
	FilesHideBelow   = 1100
	PanelGap         = Space10
	// SidebarIconsBelow is the window width under which the sidebar is an
	// icon column (D128); SidebarIconWidth is the width of that column. The
	// stored sidebar width (D111) does not change.
	SidebarIconsBelow = 800
	SidebarIconWidth  = 56
)

// PR pane (D59)
const PRPaneWidth = 560
const PRPanePad = 16
const PRWhoWidth = 74 // timeline: who column

// Sidebar spaces (planner defaults; docs/DESIGN.md has no sizes for these)
const SidebarPadX = 12       // left/right padding of sidebar content
const TrafficLightInset = 78 // room for the native window buttons (x 14, 3×12 + 2×8 gaps, + 12)
const CardInsetX = 4         // selected-card margin (same 4 px as the agent rows)
const BadgeRadius = 4
const SpacesGap = 8 // icon to text, name to branch, between a row's parts
const MemBarWidth = 60
const SidebarToggleRadius = 2

// Title bar tabs and buttons (planner defaults; docs/DESIGN.md has no sizes for these)

const TitleButtonSize = 28 // "+", split, panel toggle: square hit areas
const TitleGlyphWidth = 14 // split and panel glyphs, border included
const TitleGlyphHeight = 12
const TitleBarPadEnd = Space14 // title bar: room right of the files toggle
const TitleGlyphFill = 4       // panel glyph: filled right part when the panel is open

// Board panel (planner defaults; docs/DESIGN.md has no sizes for these). The "you" chip reuses
// BADGE_PAD_X and BADGE_RADIUS.
const PanelPadX = 12
const PanelHeaderHeight = 36 // worker panel header
const BoardLineHeight = 28   // summary, section heads, needs-you rows
const NeedKeyWidth = 29      // four characters of JetBrains Mono 12 px
const NeedGap = 8            // needs-you head and rows: between their parts
const WorkRowHeight = 42     // two text lines (18 + 18) + 6
const StageColWidth = 44     // DESIGN.md: five 44 px stage columns
const WorkLabelWidth = 52    // "#123456"

// Answer card (DESIGN.md fixes the height at 168; the rest are planner defaults)
const AnswerButtonHeight = 24
const AnswerButtonPadX = 10
const AnswerButtonRadius = 6
const AnswerButtonGap = 8 // between two buttons

// PRFieldBorder is the border of the PR footer comment field.
const PRFieldBorder = 0x2E2E2E

// Projects modal (DESIGN.md, D110)
const BgOverlay = 0x161616
const OverlayBorder = 0x2E2E2E
const ModalWidth = 580
const ModalTop = 110
const ModalRadius = 12
const ModalPadX = 18
const ModalRowSub = 0x5A5A5A
const ModalTile = 28
const ModalTileBg = 0x1F1F1F
const ModalTileRadius = 7
const ModalGrip = 0x3A3A3A
const ModalListMax = 360
const ModalRowHeight = 46
