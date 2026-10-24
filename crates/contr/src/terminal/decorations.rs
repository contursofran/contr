// Adapted from Herdr UI (Apache-2.0); changes: all five underline styles as grid marks; dash phase kept on the grid.
//! Underlines: which cells a style covers and the marks it paints. The rules are pure functions,
//! so they are tested without a window.

use contr_herdr::endpoint::surface::Cell;

use super::colors::cell_colors;
use super::paint::{HIDDEN, UNDERLINE, UNDERLINE_STYLE, UNDERLINE_STYLE_SHIFT};

/// The length of a dash, and of the gap after it, for a dashed underline.
const DASH: f32 = 4.;
const DASH_GAP: f32 = 3.;
/// The length of a dot, and of the gap after it, for a dotted underline.
const DOT: f32 = 1.;
const DOT_GAP: f32 = 1.;

/// The style of an underline. `None` is no underline.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Underline {
    None,
    Single,
    Double,
    Curly,
    Dotted,
    Dashed,
}

/// The underline of a cell. herdr keeps the style in bits 12–15: 2 double, 3 curly, 4 dotted and 5
/// dashed. 0 and 1 are single, and so is any other value, as herdr sends none. A hidden cell is not
/// underlined.
pub(super) fn underline_of(cell: &Cell) -> Underline {
    if cell.attrs & HIDDEN != 0 || cell.attrs & (UNDERLINE | UNDERLINE_STYLE) == 0 {
        return Underline::None;
    }
    match (cell.attrs & UNDERLINE_STYLE) >> UNDERLINE_STYLE_SHIFT {
        2 => Underline::Double,
        3 => Underline::Curly,
        4 => Underline::Dotted,
        5 => Underline::Dashed,
        _ => Underline::Single,
    }
}

/// Cells in one row that share an underline style and colour: the first column, the number of
/// columns, the style and the colour (the foreground, with reverse and dim applied).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct UnderlineRun {
    pub col: usize,
    pub cols: usize,
    pub kind: Underline,
    pub colour: u32,
}

/// The underline runs of a row, in column order. A cell with no underline, or a hidden cell, ends a
/// run. Blank cells and the tail of a wide glyph are underlined like any other cell.
pub(super) fn underline_runs(row: &[Cell]) -> Vec<UnderlineRun> {
    let mut runs: Vec<UnderlineRun> = Vec::new();
    for (col, cell) in row.iter().enumerate() {
        let kind = underline_of(cell);
        if kind == Underline::None {
            continue;
        }
        let colour = cell_colors(cell).0;
        match runs.last_mut() {
            Some(run) if run.kind == kind && run.colour == colour && run.col + run.cols == col => {
                run.cols += 1;
            }
            _ => runs.push(UnderlineRun {
                col,
                cols: 1,
                kind,
                colour,
            }),
        }
    }
    runs
}

/// A line an underline paints. `x0` and `x1` are pixels from the grid's left edge. `y` is the top
/// of the mark, in pixels from the top of its row.
#[derive(Debug, Clone, Copy, PartialEq)]
pub(super) enum Mark {
    /// A straight bar, one pixel thick.
    Bar { x0: f32, x1: f32, y: f32 },
    /// A wave three pixels high, drawn by the window's wavy underline.
    Wave { x0: f32, x1: f32, y: f32 },
}

/// The marks `kind` paints from `x0` to `x1`, in a row `cell_h` pixels tall. Nothing paints for
/// `None` or for an empty span.
pub(super) fn marks(kind: Underline, x0: f32, x1: f32, cell_h: f32) -> Vec<Mark> {
    if x1 <= x0 {
        return Vec::new();
    }
    // The bottom bar is one pixel thick, ending one pixel above the row's bottom edge.
    let bottom = cell_h - 2.;
    let bottom_bars = |on: f32, gap: f32| -> Vec<Mark> {
        dashes(x0, x1, on, gap)
            .map(|(start, end)| Mark::Bar {
                x0: start,
                x1: end,
                y: bottom,
            })
            .collect()
    };
    match kind {
        Underline::None => Vec::new(),
        Underline::Single => vec![Mark::Bar { x0, x1, y: bottom }],
        Underline::Double => vec![
            Mark::Bar {
                x0,
                x1,
                y: bottom - 2.,
            },
            Mark::Bar { x0, x1, y: bottom },
        ],
        Underline::Curly => vec![Mark::Wave {
            x0,
            x1,
            y: bottom - 2.,
        }],
        Underline::Dotted => bottom_bars(DOT, DOT_GAP),
        Underline::Dashed => bottom_bars(DASH, DASH_GAP),
    }
}

/// The on-spans of a dashed line from `left` to `right`. Each is `on` pixels long, and one starts
/// every `on + off` pixels from the grid's left edge. The phase is the grid's, not the run's, so the
/// dashes of neighbouring runs form one even line.
fn dashes(left: f32, right: f32, on: f32, off: f32) -> impl Iterator<Item = (f32, f32)> {
    let period = on + off;
    let first = (left / period).floor();
    (0u32..)
        .map(move |n| {
            let start = (first + n as f32) * period;
            (start.max(left), (start + on).min(right))
        })
        .take_while(move |(start, _)| *start < right)
        .filter(|(start, end)| start < end)
}

#[cfg(test)]
mod tests {
    use super::*;

    const RED: u32 = 0x02E0_6C6C;
    const GREEN: u32 = 0x0200_FF00;
    const RED_RGB: u32 = 0xE06C6C;
    const GREEN_RGB: u32 = 0x00FF00;
    /// Eight pixels a cell and twenty a row: a bar sits at y 18, its double at 16, a wave at 16.
    const CELL_W: f32 = 8.;
    const CELL_H: f32 = 20.;

    fn cell(text: &str, attrs: u16) -> Cell {
        Cell {
            text: text.to_owned(),
            fg: 0,
            bg: 0,
            attrs,
            wide_tail: false,
        }
    }

    fn coloured(text: &str, attrs: u16, fg: u32) -> Cell {
        Cell {
            fg,
            ..cell(text, attrs)
        }
    }

    /// The bars of `marks` as (x0, x1, y). A wave fails the test.
    fn bars(marks: Vec<Mark>) -> Vec<(f32, f32, f32)> {
        marks
            .into_iter()
            .map(|mark| match mark {
                Mark::Bar { x0, x1, y } => (x0, x1, y),
                Mark::Wave { .. } => panic!("expected a bar, got {mark:?}"),
            })
            .collect()
    }

    #[test]
    fn herdr_bits_name_the_underline_style() {
        let style = |attrs| underline_of(&cell("a", attrs));
        assert_eq!(style(0), Underline::None);
        assert_eq!(style(UNDERLINE), Underline::Single);
        // Style 1 is single too, and a style without the underline bit still underlines.
        assert_eq!(style(0x1000), Underline::Single);
        assert_eq!(style(UNDERLINE | 0x2000), Underline::Double);
        assert_eq!(style(UNDERLINE | 0x3000), Underline::Curly);
        assert_eq!(style(UNDERLINE | 0x4000), Underline::Dotted);
        assert_eq!(style(UNDERLINE | 0x5000), Underline::Dashed);
        // herdr sends no other style. Any other value paints as single.
        assert_eq!(style(UNDERLINE | 0x6000), Underline::Single);
    }

    #[test]
    fn a_hidden_cell_is_not_underlined() {
        assert_eq!(
            underline_of(&cell("a", HIDDEN | UNDERLINE)),
            Underline::None
        );
        assert_eq!(underline_of(&cell("a", HIDDEN | 0x3000)), Underline::None);
    }

    #[test]
    fn cells_with_one_style_and_colour_make_one_run() {
        let row = [
            coloured("a", UNDERLINE, RED),
            coloured("b", UNDERLINE, RED),
            coloured(" ", UNDERLINE, RED),
        ];
        assert_eq!(
            underline_runs(&row),
            vec![UnderlineRun {
                col: 0,
                cols: 3,
                kind: Underline::Single,
                colour: RED_RGB,
            }]
        );
    }

    #[test]
    fn a_change_of_style_or_colour_starts_a_run() {
        let row = [
            coloured("a", UNDERLINE, RED),
            coloured("b", UNDERLINE | 0x3000, RED),
            coloured("c", UNDERLINE | 0x3000, GREEN),
        ];
        assert_eq!(
            underline_runs(&row),
            vec![
                UnderlineRun {
                    col: 0,
                    cols: 1,
                    kind: Underline::Single,
                    colour: RED_RGB,
                },
                UnderlineRun {
                    col: 1,
                    cols: 1,
                    kind: Underline::Curly,
                    colour: RED_RGB,
                },
                UnderlineRun {
                    col: 2,
                    cols: 1,
                    kind: Underline::Curly,
                    colour: GREEN_RGB,
                },
            ]
        );
    }

    #[test]
    fn a_plain_or_hidden_cell_ends_a_run() {
        let plain = [cell("a", UNDERLINE), cell("b", 0), cell("c", UNDERLINE)];
        assert_eq!(
            underline_runs(&plain)
                .iter()
                .map(|run| (run.col, run.cols))
                .collect::<Vec<_>>(),
            vec![(0, 1), (2, 1)]
        );
        let hidden = [
            cell("a", UNDERLINE),
            cell("b", HIDDEN | UNDERLINE),
            cell("c", UNDERLINE),
        ];
        assert_eq!(
            underline_runs(&hidden)
                .iter()
                .map(|run| (run.col, run.cols))
                .collect::<Vec<_>>(),
            vec![(0, 1), (2, 1)]
        );
    }

    #[test]
    fn a_blank_cell_and_the_tail_of_a_wide_glyph_are_underlined() {
        let tail = Cell {
            wide_tail: true,
            ..cell("", UNDERLINE)
        };
        let row = [cell(" ", UNDERLINE), cell("界", UNDERLINE), tail];
        assert_eq!(
            underline_runs(&row),
            vec![UnderlineRun {
                col: 0,
                cols: 3,
                kind: Underline::Single,
                colour: cell_colors(&row[0]).0,
            }]
        );
    }

    #[test]
    fn single_is_one_bar_two_pixels_above_the_bottom() {
        assert_eq!(
            marks(Underline::Single, 0., 16., CELL_H),
            vec![Mark::Bar {
                x0: 0.,
                x1: 16.,
                y: 18.,
            }]
        );
    }

    #[test]
    fn double_is_two_bars_two_and_four_pixels_above_the_bottom() {
        assert_eq!(
            marks(Underline::Double, 0., 16., CELL_H),
            vec![
                Mark::Bar {
                    x0: 0.,
                    x1: 16.,
                    y: 16.,
                },
                Mark::Bar {
                    x0: 0.,
                    x1: 16.,
                    y: 18.,
                },
            ]
        );
    }

    #[test]
    fn curly_is_one_wave_that_starts_four_pixels_above_the_bottom() {
        assert_eq!(
            marks(Underline::Curly, 0., 16., CELL_H),
            vec![Mark::Wave {
                x0: 0.,
                x1: 16.,
                y: 16.,
            }]
        );
    }

    #[test]
    fn dotted_is_one_pixel_dots_every_two_pixels_from_the_grid_origin() {
        // The second cell: dots start at 8, 10, 12 and 14.
        assert_eq!(
            bars(marks(Underline::Dotted, CELL_W, 2. * CELL_W, CELL_H)),
            vec![
                (8., 9., 18.),
                (10., 11., 18.),
                (12., 13., 18.),
                (14., 15., 18.),
            ]
        );
    }

    #[test]
    fn dashed_is_four_pixel_dashes_every_seven_pixels_from_the_grid_origin() {
        // Dashes start at 0, 7, 14, 21 and so on. The second and third cells hold 8 to 24.
        assert_eq!(
            bars(marks(Underline::Dashed, CELL_W, 3. * CELL_W, CELL_H)),
            vec![(8., 11., 18.), (14., 18., 18.), (21., 24., 18.)]
        );
    }

    #[test]
    fn a_dash_across_a_cell_edge_is_two_pieces_that_meet_there() {
        // The dash at 7 to 11 is 7 to 8 in the first cell and 8 to 11 in the second.
        let first = bars(marks(Underline::Dashed, 0., CELL_W, CELL_H));
        let second = bars(marks(Underline::Dashed, CELL_W, 2. * CELL_W, CELL_H));
        assert_eq!(first, vec![(0., 4., 18.), (7., 8., 18.)]);
        assert_eq!(second, vec![(8., 11., 18.), (14., 16., 18.)]);
    }

    #[test]
    fn each_style_paints_inside_its_cells() {
        // The render check: every style paints marks, and each mark stays in its span and row.
        let styles = [
            Underline::Single,
            Underline::Double,
            Underline::Curly,
            Underline::Dotted,
            Underline::Dashed,
        ];
        for kind in styles {
            let found = marks(kind, CELL_W, 5. * CELL_W, CELL_H);
            assert!(!found.is_empty(), "{kind:?} paints nothing");
            for mark in found {
                let (x0, x1, y, height) = match mark {
                    Mark::Bar { x0, x1, y } => (x0, x1, y, 1.),
                    Mark::Wave { x0, x1, y } => (x0, x1, y, 3.),
                };
                assert!(
                    x0 >= CELL_W && x1 <= 5. * CELL_W && x0 < x1,
                    "{kind:?}: {mark:?}"
                );
                assert!(y >= 0. && y + height <= CELL_H, "{kind:?}: {mark:?}");
            }
        }
    }

    #[test]
    fn no_underline_or_empty_span_paints_nothing() {
        assert!(marks(Underline::None, 0., 16., CELL_H).is_empty());
        assert!(marks(Underline::Single, CELL_W, CELL_W, CELL_H).is_empty());
        assert!(marks(Underline::Dashed, 2. * CELL_W, CELL_W, CELL_H).is_empty());
    }
}
