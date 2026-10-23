//! Paints a surface: cell backgrounds, box glyphs as quads, text and the cursor. Text is shaped per
//! row and kept while herdr sends the same row again (`Arc::ptr_eq`), so a patch that changes one
//! row shapes one row. The span, run and cursor rules are pure functions, so they are tested
//! without a window.

use std::sync::Arc;

use contr_herdr::endpoint::messages::GridSize;
use contr_herdr::endpoint::surface::{Cell, Cursor, Row, Surface};
use gpui::{
    App, BorderStyle, Bounds, FontWeight, Hsla, Pixels, Point, ShapedLine, StrikethroughStyle,
    TextAlign, TextRun, UnderlineStyle, Window, fill, font, outline, point, px, rgb, rgba, size,
};

use super::boxdraw::{self, BoxGlyph};
use super::colors::cell_colors;
use super::grid::Metrics;
use crate::theme::{BG_TERMINAL, FONT_FAMILY, TERMINAL_FONT_SIZE, TEXT};

/// herdr's modifier bits (`Cell::attrs`) that the painter reads.
const BOLD: u16 = 1;
const UNDERLINE: u16 = 8;
const HIDDEN: u16 = 128;
const STRIKETHROUGH: u16 = 256;
/// herdr keeps an underline style in the bits above the low byte. Any of them means underlined.
const UNDERLINE_STYLE: u16 = 0xF000;

/// The cursor is 50 % white (`docs/DESIGN.md` greys, and white only for what the owner acts on).
const CURSOR_RGBA: u32 = 0xFFFFFF80;
/// Thickness of a bar or underline cursor, in logical pixels.
const CURSOR_LINE: f32 = 2.;

/// The shaped text of one row: each line starts at its column.
pub(super) type RowCache = Vec<(Row, Vec<(usize, ShapedLine)>)>;

/// What the view adds to a paint: whether it has focus, and the text the IME is composing.
pub(super) struct Typing<'a> {
    pub focused: bool,
    pub marked: Option<&'a str>,
}

/// Brings `cache` to `rows`, one entry per row, and truncates it to the row count. A row that is
/// the same `Arc` as its cached entry keeps its lines. Any other row is shaped by `shape_row`.
pub(super) fn refresh_rows<L>(
    cache: &mut Vec<(Row, Vec<(usize, L)>)>,
    rows: &[Row],
    mut shape_row: impl FnMut(&[Cell]) -> Vec<(usize, L)>,
) {
    cache.truncate(rows.len());
    for (y, row) in rows.iter().enumerate() {
        if cache
            .get(y)
            .is_some_and(|(cached, _)| Arc::ptr_eq(cached, row))
        {
            continue;
        }
        let entry = (Arc::clone(row), shape_row(row));
        if y < cache.len() {
            cache[y] = entry;
        } else {
            cache.push(entry);
        }
    }
}

/// The look of a run of text. Two cells with equal styles join one span.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct Style {
    pub fg: u32,
    pub bold: bool,
    pub underline: bool,
    pub strike: bool,
}

/// Text on one row, from column `col`. An ASCII span has one column per character.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Span {
    pub col: usize,
    pub text: String,
    pub style: Style,
}

/// The box or block glyph a cell paints as quads, if any. A wide tail or a hidden cell paints
/// nothing.
fn quad_glyph(cell: &Cell) -> Option<BoxGlyph> {
    if cell.wide_tail || cell.attrs & HIDDEN != 0 {
        return None;
    }
    let mut chars = cell.text.chars();
    match (chars.next(), chars.next()) {
        (Some(c), None) => boxdraw::segments(c),
        _ => None,
    }
}

/// The spans of a row, in column order. Wide glyph tails, hidden cells and box glyphs make no
/// span: box glyphs are drawn as quads. A span of blanks makes none either, unless it is
/// underlined or struck through.
pub(super) fn spans(row: &[Cell]) -> Vec<Span> {
    let mut out: Vec<Span> = Vec::new();
    for (col, cell) in row.iter().enumerate() {
        if cell.wide_tail || cell.attrs & HIDDEN != 0 || quad_glyph(cell).is_some() {
            continue;
        }
        let style = style_of(cell);
        if is_ascii_char(&cell.text) {
            match out.last_mut() {
                // An ASCII span has one column per byte, so it ends at `col + len`.
                Some(last)
                    if last.style == style
                        && last.text.is_ascii()
                        && last.col + last.text.len() == col =>
                {
                    last.text.push_str(&cell.text);
                }
                _ => out.push(Span {
                    col,
                    text: cell.text.clone(),
                    style,
                }),
            }
        } else if !cell.text.is_empty() {
            out.push(Span {
                col,
                text: cell.text.clone(),
                style,
            });
        }
    }
    out.retain(|span| !is_blank(span));
    out
}

/// Runs of one background colour in a row: (first column, columns, colour). Cells with the
/// default background make no run.
pub(super) fn background_runs(row: &[Cell]) -> Vec<(usize, usize, u32)> {
    let mut runs: Vec<(usize, usize, u32)> = Vec::new();
    for (col, cell) in row.iter().enumerate() {
        let Some(colour) = cell_colors(cell).1 else {
            continue;
        };
        match runs.last_mut() {
            Some((start, len, last)) if *last == colour && *start + *len == col => *len += 1,
            _ => runs.push((col, 1, colour)),
        }
    }
    runs
}

/// The cell the cursor is in, clamped to the grid. herdr may place it one past the last column.
pub(super) fn cursor_cell(cursor: Cursor, size: GridSize) -> (u16, u16) {
    (
        cursor.x.min(size.cols.saturating_sub(1)),
        cursor.y.min(size.rows.saturating_sub(1)),
    )
}

/// The part of a cell the cursor fills: the whole cell for a block (shapes 0–2), a bar at its
/// left edge for shapes 5–6, and a line at its bottom for shapes 3–4.
pub(super) fn cursor_rect(cell: Bounds<Pixels>, shape: u8) -> Bounds<Pixels> {
    match shape {
        3 | 4 => Bounds::new(
            point(
                cell.origin.x,
                cell.origin.y + cell.size.height - px(CURSOR_LINE),
            ),
            size(cell.size.width, px(CURSOR_LINE)),
        ),
        5 | 6 => Bounds::new(cell.origin, size(px(CURSOR_LINE), cell.size.height)),
        _ => cell,
    }
}

/// Paints `surface` with its top-left cell at `origin`. `typing.focused` draws the cursor solid;
/// without it the cursor is an outline.
pub(super) fn paint_surface(
    window: &mut Window,
    cx: &mut App,
    origin: Point<Pixels>,
    m: Metrics,
    surface: &Surface,
    typing: Typing<'_>,
    cache: &mut RowCache,
) {
    for (y, row) in surface.rows.iter().enumerate() {
        let top = origin.y + px(y as f32 * m.cell_h);
        for (start, len, colour) in background_runs(row) {
            let at = point(origin.x + px(start as f32 * m.cell_w), top);
            let run = Bounds::new(at, size(px(len as f32 * m.cell_w), px(m.cell_h)));
            window.paint_quad(fill(run, Hsla::from(rgb(colour))));
        }
    }
    paint_quad_glyphs(window, origin, m, surface);

    refresh_rows(cache, &surface.rows, |row| {
        spans(row)
            .iter()
            .map(|span| (span.col, shape(&span.text, span.style, window)))
            .collect()
    });
    for (y, (_, lines)) in cache.iter().enumerate() {
        let top = origin.y + px(y as f32 * m.cell_h);
        for (col, line) in lines {
            let at = point(origin.x + px(*col as f32 * m.cell_w), top);
            paint_line(line, at, m, window, cx);
        }
    }

    if let Some(cursor) = surface.cursor {
        if let Some(text) = typing.marked {
            paint_marked(window, cx, origin, m, cursor, surface.size, text);
        }
        if cursor.visible {
            paint_cursor(window, origin, m, cursor, surface.size, typing.focused);
        }
    }
}

/// Paints the box and block glyphs of the surface as quads, each in its own cell. Every edge is
/// computed from the cell's row or column index, so neighbouring cells share it exactly. Shades
/// are translucent. The quads paint before the text, which has no span for these cells.
fn paint_quad_glyphs(window: &mut Window, origin: Point<Pixels>, m: Metrics, surface: &Surface) {
    let scale = window.scale_factor();
    let (left, top) = (f32::from(origin.x), f32::from(origin.y));
    for (y, row) in surface.rows.iter().enumerate() {
        let (y0, y1) = (top + y as f32 * m.cell_h, top + (y + 1) as f32 * m.cell_h);
        for (col, cell) in row.iter().enumerate() {
            let Some(glyph) = quad_glyph(cell) else {
                continue;
            };
            let (x0, x1) = (
                left + col as f32 * m.cell_w,
                left + (col + 1) as f32 * m.cell_w,
            );
            let paint = Hsla {
                a: glyph.alpha(),
                ..Hsla::from(rgb(cell_colors(cell).0))
            };
            let edges = boxdraw::Edges { x0, y0, x1, y1 };
            boxdraw::quads(glyph, edges, scale, |q| {
                let at = point(px(q.x), px(q.y));
                window.paint_quad(fill(Bounds::new(at, size(px(q.w), px(q.h))), paint));
            });
        }
    }
}

/// Paints one shaped line. A line that cannot paint leaves its text out; the rest still paints.
fn paint_line(line: &ShapedLine, at: Point<Pixels>, m: Metrics, window: &mut Window, cx: &mut App) {
    if let Err(e) = line.paint(at, px(m.cell_h), TextAlign::Left, None, window, cx) {
        tracing::debug!("terminal text not painted: {e}");
    }
}

/// Paints the text the IME is composing, underlined, at the cursor cell. Its background is the
/// terminal's, so the glyphs under it do not show through.
fn paint_marked(
    window: &mut Window,
    cx: &mut App,
    origin: Point<Pixels>,
    m: Metrics,
    cursor: Cursor,
    grid: GridSize,
    text: &str,
) {
    let (x, y) = cursor_cell(cursor, grid);
    let at = point(
        origin.x + px(f32::from(x) * m.cell_w),
        origin.y + px(f32::from(y) * m.cell_h),
    );
    let style = Style {
        fg: TEXT,
        bold: false,
        underline: true,
        strike: false,
    };
    let line = shape(text, style, window);
    let cover = Bounds::new(at, size(line.width(), px(m.cell_h)));
    window.paint_quad(fill(cover, Hsla::from(rgb(BG_TERMINAL))));
    paint_line(&line, at, m, window, cx);
}

fn paint_cursor(
    window: &mut Window,
    origin: Point<Pixels>,
    m: Metrics,
    cursor: Cursor,
    grid: GridSize,
    focused: bool,
) {
    let (x, y) = cursor_cell(cursor, grid);
    let at = point(
        origin.x + px(f32::from(x) * m.cell_w),
        origin.y + px(f32::from(y) * m.cell_h),
    );
    let cell = Bounds::new(at, size(px(m.cell_w), px(m.cell_h)));
    let colour = Hsla::from(rgba(CURSOR_RGBA));
    if focused {
        window.paint_quad(fill(cursor_rect(cell, cursor.shape), colour));
    } else {
        window.paint_quad(outline(cell, colour, BorderStyle::default()));
    }
}

/// Shapes `text` in the terminal font, at the terminal size, in the look `style` gives.
fn shape(text: &str, style: Style, window: &Window) -> ShapedLine {
    let colour = Hsla::from(rgb(style.fg));
    let mut face = font(FONT_FAMILY);
    if style.bold {
        face.weight = FontWeight::BOLD;
    }
    let run = TextRun {
        len: text.len(),
        font: face,
        color: colour,
        background_color: None,
        underline: style.underline.then_some(UnderlineStyle {
            thickness: px(1.),
            color: Some(colour),
            wavy: false,
        }),
        strikethrough: style.strike.then_some(StrikethroughStyle {
            thickness: px(1.),
            color: Some(colour),
        }),
    };
    window
        .text_system()
        .shape_line(text.to_owned().into(), px(TERMINAL_FONT_SIZE), &[run], None)
}

fn style_of(cell: &Cell) -> Style {
    Style {
        fg: cell_colors(cell).0,
        bold: cell.attrs & BOLD != 0,
        underline: cell.attrs & (UNDERLINE | UNDERLINE_STYLE) != 0,
        strike: cell.attrs & STRIKETHROUGH != 0,
    }
}

/// True for one printable ASCII character (space included).
fn is_ascii_char(text: &str) -> bool {
    let mut chars = text.chars();
    matches!((chars.next(), chars.next()), (Some(c), None) if (' '..='~').contains(&c))
}

/// True when a span is only whitespace and has no underline or strikethrough to draw.
fn is_blank(span: &Span) -> bool {
    span.text.trim().is_empty() && !span.style.underline && !span.style.strike
}

#[cfg(test)]
mod tests {
    use std::cell::RefCell;

    use super::*;

    const RED: u32 = 0x02E0_6C6C;
    const GREEN: u32 = 0x0200_FF00;
    const PLAIN: u32 = 0;

    fn cell(text: &str, attrs: u16) -> Cell {
        Cell {
            text: text.to_owned(),
            fg: PLAIN,
            bg: PLAIN,
            attrs,
            wide_tail: false,
        }
    }

    fn coloured(text: &str, fg: u32) -> Cell {
        Cell {
            fg,
            ..cell(text, 0)
        }
    }

    fn tail() -> Cell {
        Cell {
            wide_tail: true,
            ..cell("", 0)
        }
    }

    fn texts(row: &[Cell]) -> Vec<(usize, String)> {
        spans(row)
            .into_iter()
            .map(|span| (span.col, span.text))
            .collect()
    }

    #[test]
    fn ascii_cells_with_one_style_make_one_span() {
        let row = [cell("a", 0), cell("b", 0), cell("c", 0)];
        assert_eq!(texts(&row), vec![(0, "abc".to_owned())]);
    }

    #[test]
    fn a_style_change_starts_a_new_span() {
        let row = [coloured("a", RED), coloured("b", GREEN)];
        assert_eq!(texts(&row), vec![(0, "a".to_owned()), (1, "b".to_owned())]);
    }

    #[test]
    fn spaces_inside_a_line_stay_in_the_span_so_columns_hold() {
        let row = [cell("a", 0), cell(" ", 0), cell("b", 0)];
        assert_eq!(texts(&row), vec![(0, "a b".to_owned())]);
    }

    #[test]
    fn a_blank_run_makes_no_span_but_an_underlined_one_does() {
        let plain = [cell(" ", 0), cell(" ", 0)];
        assert!(spans(&plain).is_empty());
        let underlined = [cell(" ", UNDERLINE)];
        assert_eq!(texts(&underlined), vec![(0, " ".to_owned())]);
    }

    #[test]
    fn a_non_ascii_cell_is_its_own_span_at_its_column() {
        let row = [cell("a", 0), cell("é", 0), cell("b", 0)];
        assert_eq!(
            texts(&row),
            vec![
                (0, "a".to_owned()),
                (1, "é".to_owned()),
                (2, "b".to_owned())
            ]
        );
    }

    #[test]
    fn an_ascii_cell_never_joins_a_non_ascii_span_across_a_gap() {
        // "é" is two bytes but one column, so byte length must not stand in for columns.
        let row = [cell("é", 0), cell("h", HIDDEN), cell("a", 0)];
        assert_eq!(texts(&row), vec![(0, "é".to_owned()), (2, "a".to_owned())]);
    }

    #[test]
    fn a_wide_glyph_skips_its_tail_and_the_next_text_keeps_its_column() {
        let row = [cell("界", 0), tail(), cell("x", 0)];
        assert_eq!(texts(&row), vec![(0, "界".to_owned()), (2, "x".to_owned())]);
    }

    #[test]
    fn hidden_cells_make_no_span_so_nothing_is_shaped_for_them() {
        let row = [cell("a", HIDDEN), cell("b", 0)];
        assert_eq!(texts(&row), vec![(1, "b".to_owned())]);
        let only_hidden = [cell("a", HIDDEN), cell("é", HIDDEN)];
        assert!(spans(&only_hidden).is_empty());
    }

    #[test]
    fn bold_strike_and_any_underline_style_are_read_from_attrs() {
        let style = |attrs| style_of(&cell("a", attrs));
        assert!(style(BOLD).bold);
        assert!(style(UNDERLINE).underline);
        // herdr's underline style lives above the low byte.
        assert!(style(0x1000).underline);
        assert!(style(STRIKETHROUGH).strike);
        assert!(!style(0).bold && !style(0).underline && !style(0).strike);
    }

    #[test]
    fn background_runs_merge_equal_colours_and_skip_the_default() {
        let bg = |colour| Cell {
            bg: colour,
            ..cell(" ", 0)
        };
        let row = [
            bg(0x0200_FF00),
            bg(0x0200_FF00),
            bg(PLAIN),
            bg(0x0200_FF00),
            bg(0x0200_0001),
        ];
        assert_eq!(
            background_runs(&row),
            vec![(0, 2, 0x00FF00), (3, 1, 0x00FF00), (4, 1, 0x000001)]
        );
    }

    #[test]
    fn the_cursor_is_clamped_to_the_grid() {
        let grid = GridSize { cols: 80, rows: 24 };
        let at = |x, y| Cursor {
            x,
            y,
            visible: true,
            shape: 0,
        };
        assert_eq!(cursor_cell(at(10, 5), grid), (10, 5));
        // herdr may put it one past the last column or row.
        assert_eq!(cursor_cell(at(80, 24), grid), (79, 23));
        assert_eq!(cursor_cell(at(900, 900), grid), (79, 23));
    }

    #[test]
    fn cursor_shapes_fill_the_block_the_bar_or_the_underline() {
        let cell = Bounds::new(point(px(10.), px(20.)), size(px(8.), px(20.)));
        let block = cursor_rect(cell, 2);
        assert_eq!((block.origin, block.size), (cell.origin, cell.size));
        let bar = cursor_rect(cell, 5);
        assert_eq!(f32::from(bar.size.width), 2.);
        assert_eq!(f32::from(bar.size.height), 20.);
        assert_eq!(f32::from(bar.origin.x), 10.);
        let underline = cursor_rect(cell, 3);
        assert_eq!(f32::from(underline.size.width), 8.);
        assert_eq!(f32::from(underline.size.height), 2.);
        assert_eq!(f32::from(underline.origin.y), 38.);
    }

    #[test]
    fn a_row_herdr_sends_again_is_shaped_only_when_it_is_a_new_row() {
        let first: Row = Arc::from(vec![cell("a", 0)]);
        let second: Row = Arc::from(vec![cell("b", 0)]);
        let mut cache: Vec<(Row, Vec<(usize, String)>)> = Vec::new();
        let shaped: RefCell<Vec<String>> = RefCell::new(Vec::new());
        let shape_row = |row: &[Cell]| {
            shaped.borrow_mut().push(row[0].text.clone());
            vec![(0, row[0].text.clone())]
        };

        refresh_rows(
            &mut cache,
            &[Arc::clone(&first), Arc::clone(&second)],
            shape_row,
        );
        assert_eq!(*shaped.borrow(), ["a", "b"]);

        // The same rows again, as herdr sends them with a patch that leaves them alone: no shaping.
        refresh_rows(
            &mut cache,
            &[Arc::clone(&first), Arc::clone(&second)],
            shape_row,
        );
        assert_eq!(*shaped.borrow(), ["a", "b"]);

        // A patch that replaces the second row: only that row is shaped again.
        let patched: Row = Arc::from(vec![cell("c", 0)]);
        refresh_rows(&mut cache, &[first.clone(), patched], shape_row);
        assert_eq!(*shaped.borrow(), ["a", "b", "c"]);

        // A shorter surface drops the rows below it: nothing is shaped.
        refresh_rows(&mut cache, std::slice::from_ref(&first), shape_row);
        assert_eq!(*shaped.borrow(), ["a", "b", "c"]);
        assert_eq!(cache.len(), 1);
    }

    #[test]
    fn a_non_ascii_or_empty_text_is_not_a_plain_char() {
        assert!(is_ascii_char("a"));
        assert!(is_ascii_char(" "));
        assert!(!is_ascii_char(""));
        assert!(!is_ascii_char("ab"));
        assert!(!is_ascii_char("é"));
        assert!(!is_ascii_char("\t"));
    }

    #[test]
    fn a_box_glyph_makes_no_text_span_so_it_is_not_shaped() {
        let row = [cell("a", 0), cell("│", 0), cell("b", 0)];
        assert_eq!(texts(&row), vec![(0, "a".to_owned()), (2, "b".to_owned())]);
    }

    #[test]
    fn a_box_glyph_cell_is_a_line_glyph_in_its_colour() {
        let line = coloured("─", RED);
        assert!(matches!(quad_glyph(&line), Some(BoxGlyph::Lines(_))));
        assert_eq!(cell_colors(&line).0, 0xE06C6C);
        assert_eq!(quad_glyph(&cell("a", 0)), None);
    }

    #[test]
    fn a_hidden_box_glyph_paints_nothing_and_a_wide_tail_is_not_a_glyph() {
        let hidden = cell("│", HIDDEN);
        assert_eq!(quad_glyph(&hidden), None);
        assert!(spans(&[hidden]).is_empty());
        let wide_tail = Cell {
            wide_tail: true,
            ..cell("│", 0)
        };
        assert_eq!(quad_glyph(&wide_tail), None);
    }

    #[test]
    fn a_text_of_more_than_one_char_is_not_a_box_glyph() {
        let row = cell("│a", 0);
        assert_eq!(quad_glyph(&row), None);
        assert_eq!(texts(&[row]), vec![(0, "│a".to_owned())]);
    }
}
