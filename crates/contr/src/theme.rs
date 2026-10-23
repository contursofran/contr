//! Design tokens from docs/DESIGN.md. Greyscale only: white means the owner acts, greys mean everything else.
//! Views use these constants; no hex colours in views.

// Font
pub const FONT_FAMILY: &str = "JetBrains Mono";
pub const FONT_SIZE: f32 = 12.;
pub const LINE_HEIGHT: f32 = 18.;
pub const TERMINAL_FONT_SIZE: f32 = 13.;
pub const TERMINAL_LINE_HEIGHT: f32 = 20.;
pub const TERM_PADDING: f32 = 8.; // space between the terminal view's edge and its grid

// Backgrounds
pub const BG_WINDOW: u32 = 0x141414; // window, panel
pub const BG_BARS: u32 = 0x101010; // sidebar, title bar, status bar
pub const BG_TERMINAL: u32 = 0x121212;
pub const BG_CARD: u32 = 0x1C1C1C; // selected row
pub const CARD_BORDER: u32 = 0x2C2C2C; // 1px inset
pub const CARD_RADIUS: f32 = 7.;
pub const BG_ANSWER: u32 = 0x1A1A1A; // answer card
pub const ANSWER_BORDER: u32 = 0x262626;
pub const ANSWER_RADIUS: f32 = 10.;
pub const LINE: u32 = 0x222222; // dividers

// Text
pub const TEXT: u32 = 0xEDEDED; // primary
pub const TEXT_2: u32 = 0xBDBDBD; // secondary
pub const TEXT_DIM: u32 = 0x7A7A7A; // labels
pub const TEXT_DIM_2: u32 = 0x6A6A6A; // meta
pub const YOU_BG: u32 = 0xEDEDED; // anything waiting on the owner
pub const YOU_TEXT: u32 = 0x111111; // bold, on YOU_BG

// Status icons (16px viewBox)
pub const ICON_SIZE: f32 = 14.;
pub const ICON_SIZE_SMALL: f32 = 12.;
pub const ICON_YOU: u32 = YOU_BG; // owner acts: needs you, decide, do, blocked, working arc
pub const ICON_GREY: u32 = TEXT_DIM; // waiting, idle, done, fyi
pub const ICON_FAINT: u32 = 0x4A4A4A; // not started, working ring (owner-decided default)
pub const SPIN_PERIOD_MS: u64 = 1400; // one turn of "working"

// Window and frame sizes
pub const WINDOW_WIDTH: f32 = 1440.;
pub const WINDOW_HEIGHT: f32 = 900.;
pub const SIDEBAR_WIDTH: f32 = 280.;
pub const TITLE_BAR_HEIGHT: f32 = 40.;
pub const PANEL_WIDTH: f32 = 440.;
pub const STATUS_BAR_HEIGHT: f32 = 26.;

// Sidebar spaces (planner defaults; docs/DESIGN.md has no sizes for these)
pub const SIDEBAR_PAD_X: f32 = 12.; // left/right padding of sidebar content
pub const TRAFFIC_LIGHT_INSET: f32 = 78.; // room for the native window buttons (x 14, 3×12 + 2×8 gaps, + 12)
pub const SPACES_ROW_HEIGHT: f32 = 26.; // one-line row: section header, repo, collapsed host
pub const WORKTREE_ROW_HEIGHT: f32 = 42.; // two-line worktree row (18 + 18 + 6)
pub const ROW_INDENT: f32 = 14.; // worktree rows under their repo
pub const CARD_INSET_X: f32 = 4.; // selected-card margin (same 4 px as the agent rows)
pub const BADGE_PAD_X: f32 = 5.;
pub const BADGE_RADIUS: f32 = 4.;
pub const MEM_BAR_WIDTH: f32 = 60.;
pub const MEM_BAR_HEIGHT: f32 = 4.;
pub const SIDEBAR_TOGGLE_BOX: f32 = 16.; // clickable box around the toggle drawing
pub const SIDEBAR_TOGGLE_WIDTH: f32 = 14.; // toggle outline, border included
pub const SIDEBAR_TOGGLE_HEIGHT: f32 = 12.;
pub const SIDEBAR_TOGGLE_RADIUS: f32 = 2.;
pub const SIDEBAR_TOGGLE_FILL: f32 = 4.; // filled left part of the outline

// Agents list (planner defaults; docs/DESIGN.md has no sizes for these)
pub const AGENTS_HEADER_HEIGHT: f32 = 28.; // "Agents" and "N running"
pub const AGENT_ROW_HEIGHT: f32 = 28.; // D42: one line per agent
pub const AGENT_ROW_PAD_X: f32 = 8.;
pub const AGENT_GAP: f32 = 8.; // icon to name, name to detail, detail to link
pub const AGENTS_MAX_SHARE: f32 = 0.45; // share of the sidebar height (D46)

// Status bar (planner defaults; docs/DESIGN.md has no sizes for these)
pub const STATUS_PAD_X: f32 = 12.; // left and right padding
pub const STATUS_GAP: f32 = 8.; // between label, bar and percent, and around the "·" separators
pub const USAGE_BAR_WIDTH: f32 = 36.; // plan usage track
pub const USAGE_BAR_HEIGHT: f32 = 4.;
pub const USAGE_BAR_RADIUS: f32 = 2.;
pub const USAGE_LOW_LEFT: f32 = 20.; // under this % left the fill and percent are white (owner acts)

// Title bar tabs and buttons (planner defaults; docs/DESIGN.md has no sizes for these)
pub const TAB_PADDING_X: f32 = 12.;
pub const TAB_GAP: f32 = 6.; // icon to label
pub const TAB_MAX_WIDTH: f32 = 180.;
/// Overflow policy (D62, settled): when the tabs do not fit,
/// each tab shrinks to this width and its label ends in an ellipsis, then the tabs row clips the
/// rest. A scroll strip is a change to this constant and to `title_bar::tabs_row` only.
pub const TAB_MIN_WIDTH: f32 = 72.;
pub const TAB_UNDERLINE: f32 = 2.; // active tab, bottom edge
pub const TITLE_BUTTON_SIZE: f32 = 28.; // "+", split, panel toggle: square hit areas
pub const TITLE_GLYPH_WIDTH: f32 = 14.; // split and panel glyphs, border included
pub const TITLE_GLYPH_HEIGHT: f32 = 12.;
pub const TITLE_GLYPH_FILL: f32 = 4.; // panel glyph: filled right part when the panel is open

// Board panel (planner defaults; docs/DESIGN.md has no sizes for these). The "you" chip reuses
// BADGE_PAD_X and BADGE_RADIUS.
pub const PANEL_PAD_X: f32 = 12.;
pub const PANEL_HEADER_HEIGHT: f32 = 36.; // Board | Report switch and the wrap label
pub const BOARD_LINE_HEIGHT: f32 = 28.; // summary, section heads, needs-you rows
pub const WORK_ROW_HEIGHT: f32 = 42.; // two text lines (18 + 18) + 6
pub const STAGE_COL_WIDTH: f32 = 44.; // DESIGN.md: five 44 px stage columns
pub const WORK_LABEL_WIDTH: f32 = 52.; // "#123456"
pub const SEGMENT_PAD_X: f32 = 8.; // Board | Report segment
pub const SEGMENT_PAD_Y: f32 = 4.;
pub const SEGMENT_RADIUS: f32 = 5.;

// Report tab (planner defaults; docs/DESIGN.md has no sizes for these)
pub const REPORT_GAP: f32 = 12.; // between "Open in editor" and "back"
pub const REPORT_INDENT: f32 = 14.; // per list depth (same as ROW_INDENT)
pub const REPORT_MARKER_WIDTH: f32 = 22.; // "•" or "10." column
pub const REPORT_BLOCK_GAP: f32 = 9.; // above level 1 and 2 headings
pub const REPORT_BLANK_HEIGHT: f32 = 9.; // half a line
pub const REPORT_BODY_PAD_TOP: f32 = 6.; // between the divider and the first line
pub const REPORT_CODE_PAD_X: f32 = 6.; // inside a code line's background
pub const REPORT_CODE_BG: u32 = BG_BARS; // grey
pub const REPORT_QUOTE_PAD: f32 = 8.; // between a quote's rule and its text
pub const REPORT_RULE_MARGIN: f32 = 8.; // above and below a horizontal rule

#[cfg(test)]
mod tests {
    use super::*;

    fn grey(c: u32) -> bool {
        let (r, g, b) = (c >> 16 & 0xFF, c >> 8 & 0xFF, c & 0xFF);
        r == g && g == b
    }

    #[test]
    fn tokens_are_greyscale() {
        let colours = [
            BG_WINDOW,
            BG_BARS,
            BG_TERMINAL,
            BG_CARD,
            CARD_BORDER,
            BG_ANSWER,
            ANSWER_BORDER,
            LINE,
            TEXT,
            TEXT_2,
            TEXT_DIM,
            TEXT_DIM_2,
            YOU_BG,
            YOU_TEXT,
            ICON_YOU,
            ICON_GREY,
            ICON_FAINT,
            REPORT_CODE_BG,
        ];
        for colour in colours {
            assert!(grey(colour), "{colour:#08X} is not grey");
        }
    }
}
