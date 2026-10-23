//! Box drawing and block characters as rectangles. The terminal font leaves a gap between rows
//! when the line height is larger than the glyph, so a column of `│` breaks. These characters are
//! drawn as quads instead. Every edge lands on a device pixel, so rows and columns meet with no
//! gap. Rounded corners (`╭ ╮ ╯ ╰`) and every other character stay text.
//!
//! The geometry is pure: logical pixels in, logical pixels out. The tests need no window.

/// The weight of one line: one logical pixel (light), two (heavy), or two parallel lines one
/// logical pixel wide, two logical pixels apart centre to centre (double).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Weight {
    Light,
    Heavy,
    Double,
}

/// The arms of a line glyph: one for each side that has a line running to the cell edge.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Arms {
    pub up: Option<Weight>,
    pub down: Option<Weight>,
    pub left: Option<Weight>,
    pub right: Option<Weight>,
}

/// A block glyph: the part of the cell it fills, as fractions of the cell (0 to 1), at `alpha`.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Block {
    pub x0: f32,
    pub y0: f32,
    pub x1: f32,
    pub y1: f32,
    pub alpha: f32,
}

/// What a character paints as quads.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BoxGlyph {
    Lines(Arms),
    Block(Block),
}

impl BoxGlyph {
    /// The opacity of the quads. Shades are translucent; lines and solid blocks are opaque.
    pub fn alpha(self) -> f32 {
        match self {
            BoxGlyph::Lines(_) => 1.,
            BoxGlyph::Block(block) => block.alpha,
        }
    }
}

/// A rectangle in logical pixels, with its top-left corner at (`x`, `y`).
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

/// The glyph that paints `c` as quads, or `None` when the font draws `c`.
pub fn segments(c: char) -> Option<BoxGlyph> {
    const L: Option<Weight> = Some(Weight::Light);
    const H: Option<Weight> = Some(Weight::Heavy);
    const D: Option<Weight> = Some(Weight::Double);
    const N: Option<Weight> = None;
    // Arms in the order: up, down, left, right.
    let lines = |up, down, left, right| {
        Some(BoxGlyph::Lines(Arms {
            up,
            down,
            left,
            right,
        }))
    };
    let block = |x0, y0, x1, y1, alpha| {
        Some(BoxGlyph::Block(Block {
            x0,
            y0,
            x1,
            y1,
            alpha,
        }))
    };
    match c {
        '─' => lines(N, N, L, L),
        '│' => lines(L, L, N, N),
        '┌' => lines(N, L, N, L),
        '┐' => lines(N, L, L, N),
        '└' => lines(L, N, N, L),
        '┘' => lines(L, N, L, N),
        '├' => lines(L, L, N, L),
        '┤' => lines(L, L, L, N),
        '┬' => lines(N, L, L, L),
        '┴' => lines(L, N, L, L),
        '┼' => lines(L, L, L, L),
        '╴' => lines(N, N, L, N),
        '╵' => lines(L, N, N, N),
        '╶' => lines(N, N, N, L),
        '╷' => lines(N, L, N, N),
        '━' => lines(N, N, H, H),
        '┃' => lines(H, H, N, N),
        '┏' => lines(N, H, N, H),
        '┓' => lines(N, H, H, N),
        '┗' => lines(H, N, N, H),
        '┛' => lines(H, N, H, N),
        '┣' => lines(H, H, N, H),
        '┫' => lines(H, H, H, N),
        '┳' => lines(N, H, H, H),
        '┻' => lines(H, N, H, H),
        '╋' => lines(H, H, H, H),
        '═' => lines(N, N, D, D),
        '║' => lines(D, D, N, N),
        '╔' => lines(N, D, N, D),
        '╗' => lines(N, D, D, N),
        '╚' => lines(D, N, N, D),
        '╝' => lines(D, N, D, N),
        '╠' => lines(D, D, N, D),
        '╣' => lines(D, D, D, N),
        '╦' => lines(N, D, D, D),
        '╩' => lines(D, N, D, D),
        '╬' => lines(D, D, D, D),
        '█' => block(0., 0., 1., 1., 1.),
        '▀' => block(0., 0., 1., 0.5, 1.),
        '▄' => block(0., 0.5, 1., 1., 1.),
        '▌' => block(0., 0., 0.5, 1., 1.),
        '▐' => block(0.5, 0., 1., 1., 1.),
        '░' => block(0., 0., 1., 1., 0.25),
        '▒' => block(0., 0., 1., 1., 0.5),
        '▓' => block(0., 0., 1., 1., 0.75),
        _ => None,
    }
}

/// The quads that paint `glyph` in `cell`, when the window has `scale` device pixels to each
/// logical pixel. Cells that touch share their edge pixels, so they meet with no gap.
pub fn quads(glyph: BoxGlyph, cell: Rect, scale: f32) -> Vec<Rect> {
    let mut out = Vec::new();
    match glyph {
        BoxGlyph::Lines(arms) => lines(arms, cell, scale, &mut out),
        BoxGlyph::Block(block) => block_quad(block, cell, scale, &mut out),
    }
    out
}

/// A logical edge, moved to the device pixel grid.
fn edge(v: f32, scale: f32) -> f32 {
    (v * scale).round()
}

/// Adds `[xa, xb) × [ya, yb)` in device pixels as a logical rectangle. An empty one adds nothing.
fn push(out: &mut Vec<Rect>, (xa, xb): (f32, f32), (ya, yb): (f32, f32), scale: f32) {
    if xb > xa && yb > ya {
        out.push(Rect {
            x: xa / scale,
            y: ya / scale,
            w: (xb - xa) / scale,
            h: (yb - ya) / scale,
        });
    }
}

fn block_quad(block: Block, cell: Rect, scale: f32, out: &mut Vec<Rect>) {
    let x = |f: f32| edge(cell.x + f * cell.w, scale);
    let y = |f: f32| edge(cell.y + f * cell.h, scale);
    push(
        out,
        (x(block.x0), x(block.x1)),
        (y(block.y0), y(block.y1)),
        scale,
    );
}

/// The strokes of one axis of a line, in device pixels: (start, end) of each, across the axis.
/// Light and heavy lines are one stroke centred on `centre`. A double line is two light strokes,
/// centred one logical pixel either side of it.
fn strokes(weight: Weight, centre: f32, scale: f32) -> Vec<(f32, f32)> {
    let light = scale.round().max(1.);
    let (thick, offsets) = match weight {
        Weight::Light => (light, vec![0.]),
        Weight::Heavy => ((2. * scale).round().max(1.), vec![0.]),
        Weight::Double => (light, vec![-scale, scale]),
    };
    offsets
        .into_iter()
        .map(|o| {
            let start = (centre + o - thick / 2.).round();
            (start, start + thick)
        })
        .collect()
}

/// The stroke at `i`, or the last one when `i` is past the end. Strokes are never empty.
fn band(strokes: &[(f32, f32)], i: usize) -> (f32, f32) {
    strokes
        .get(i)
        .or(strokes.last())
        .copied()
        .unwrap_or_default()
}

fn last(strokes: &[(f32, f32)]) -> (f32, f32) {
    strokes.last().copied().unwrap_or_default()
}

/// Index `k` counted from the far end of `n` strokes: the first stroke is the last one.
fn from_end(n: usize, k: usize) -> usize {
    n.saturating_sub(1 + k)
}

/// The rectangles of a line glyph. Each arm runs from its cell edge to the centre. Where it meets
/// a perpendicular arm, it runs on to that arm's far edge, so the corner has no notch:
/// - an arm with the opposite arm on its axis runs through the whole cell;
/// - a T or a corner joins the stroke that the other arm continues from (the inner one for a
///   double T, and the outer one for a double corner);
/// - a lone arm stops at the cell centre.
fn lines(arms: Arms, cell: Rect, scale: f32, out: &mut Vec<Rect>) {
    let (x0, x1) = (edge(cell.x, scale), edge(cell.x + cell.w, scale));
    let (y0, y1) = (edge(cell.y, scale), edge(cell.y + cell.h, scale));
    let (fx, fy) = (
        (cell.x + cell.w / 2.) * scale,
        (cell.y + cell.h / 2.) * scale,
    );
    let (cx, cy) = (fx.round(), fy.round());
    // Horizontal arms are drawn as rows, vertical arms as columns. Each axis uses the weight of
    // its arms; both arms of one axis share it in every glyph of the table.
    let rows = arms
        .left
        .or(arms.right)
        .map(|w| strokes(w, fy, scale))
        .unwrap_or_default();
    let cols = arms
        .up
        .or(arms.down)
        .map(|w| strokes(w, fx, scale))
        .unwrap_or_default();
    let (up, down) = (arms.up.is_some(), arms.down.is_some());
    let (left, right) = (arms.left.is_some(), arms.right.is_some());
    let (n_rows, n_cols) = (rows.len(), cols.len());

    for (k, &(top, bottom)) in rows.iter().enumerate() {
        if left {
            let end = match (right, up, down) {
                (true, _, _) => x1,
                (false, true, true) => band(&cols, 0).1,
                (false, false, true) => band(&cols, from_end(n_cols, k)).1,
                (false, true, false) => band(&cols, k).1,
                (false, false, false) => cx,
            };
            push(out, (x0, end), (top, bottom), scale);
        }
        if right {
            let start = match (left, up, down) {
                (true, _, _) => x0,
                (false, true, true) => last(&cols).0,
                (false, false, true) => band(&cols, k).0,
                (false, true, false) => band(&cols, from_end(n_cols, k)).0,
                (false, false, false) => cx,
            };
            push(out, (start, x1), (top, bottom), scale);
        }
    }

    for (k, &(lft, rgt)) in cols.iter().enumerate() {
        if up {
            let end = match (down, left, right) {
                (true, _, _) => y1,
                (false, true, true) => band(&rows, 0).1,
                (false, false, true) => band(&rows, from_end(n_rows, k)).1,
                (false, true, false) => band(&rows, k).1,
                (false, false, false) => cy,
            };
            push(out, (lft, rgt), (y0, end), scale);
        }
        if down {
            let start = match (up, left, right) {
                (true, _, _) => y0,
                (false, true, true) => last(&rows).0,
                (false, false, true) => band(&rows, k).0,
                (false, true, false) => band(&rows, from_end(n_rows, k)).0,
                (false, false, false) => cy,
            };
            push(out, (lft, rgt), (start, y1), scale);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const LIGHT: Option<Weight> = Some(Weight::Light);
    const HEAVY: Option<Weight> = Some(Weight::Heavy);
    const DOUBLE: Option<Weight> = Some(Weight::Double);
    const NONE: Option<Weight> = None;

    /// Every character the table draws with lines.
    const LINE_GLYPHS: &str = "─│┌┐└┘├┤┬┴┼╴╵╶╷━┃┏┓┗┛┣┫┳┻╋═║╔╗╚╝╠╣╦╩╬";
    /// An 8 × 20 logical pixel cell at the origin.
    const CELL: Rect = Rect {
        x: 0.,
        y: 0.,
        w: 8.,
        h: 20.,
    };

    /// True when the point (`x`, `y`), in logical pixels, is inside a quad.
    fn covered(quads: &[Rect], x: f32, y: f32) -> bool {
        quads
            .iter()
            .any(|q| (q.x..q.x + q.w).contains(&x) && (q.y..q.y + q.h).contains(&y))
    }

    fn arms_of(c: char) -> Arms {
        let Some(BoxGlyph::Lines(arms)) = segments(c) else {
            panic!("{c} should be a line glyph");
        };
        arms
    }

    fn lines_quads(c: char, cell: Rect, scale: f32) -> Vec<Rect> {
        let Some(glyph) = segments(c) else {
            panic!("{c} should have a glyph");
        };
        quads(glyph, cell, scale)
    }

    #[test]
    fn the_table_gives_the_arms_of_each_kind_of_line() {
        let light_cross = Arms {
            up: LIGHT,
            down: LIGHT,
            left: LIGHT,
            right: LIGHT,
        };
        assert_eq!(arms_of('┼'), light_cross);
        assert_eq!(
            arms_of('╋'),
            Arms {
                up: HEAVY,
                down: HEAVY,
                left: HEAVY,
                right: HEAVY,
            }
        );
        assert_eq!(
            arms_of('═'),
            Arms {
                up: NONE,
                down: NONE,
                left: DOUBLE,
                right: DOUBLE,
            }
        );
        assert_eq!(
            arms_of('│'),
            Arms {
                up: LIGHT,
                down: LIGHT,
                left: NONE,
                right: NONE,
            }
        );
        assert_eq!(
            arms_of('╴'),
            Arms {
                up: NONE,
                down: NONE,
                left: LIGHT,
                right: NONE,
            }
        );
    }

    #[test]
    fn every_listed_glyph_is_drawn_and_each_axis_has_one_weight() {
        for c in LINE_GLYPHS.chars() {
            let arms = arms_of(c);
            if let (Some(a), Some(b)) = (arms.left, arms.right) {
                assert_eq!(a, b, "{c}: both horizontal arms share a weight");
            }
            if let (Some(a), Some(b)) = (arms.up, arms.down) {
                assert_eq!(a, b, "{c}: both vertical arms share a weight");
            }
        }
    }

    #[test]
    fn rounded_corners_and_other_text_are_not_drawn_as_quads() {
        for c in ['╭', '╮', '╯', '╰', '+', 'a', ' ', 'é', '界'] {
            assert_eq!(segments(c), None, "{c} stays text");
        }
    }

    #[test]
    fn the_block_glyphs_fill_their_part_of_the_cell() {
        let Some(BoxGlyph::Block(full)) = segments('█') else {
            panic!("█ is a block");
        };
        assert_eq!(
            (full.x0, full.y0, full.x1, full.y1, full.alpha),
            (0., 0., 1., 1., 1.)
        );
        let upper = lines_quads('▀', CELL, 1.);
        assert_eq!(
            upper,
            vec![Rect {
                x: 0.,
                y: 0.,
                w: 8.,
                h: 10.
            }]
        );
        let shade = segments('░').map(BoxGlyph::alpha);
        assert_eq!(shade, Some(0.25));
        assert_eq!(segments('▓').map(BoxGlyph::alpha), Some(0.75));
    }

    #[test]
    fn a_block_meets_the_cell_edges_at_scale_1_and_2() {
        for scale in [1., 2.] {
            assert_eq!(
                lines_quads('█', CELL, scale),
                vec![CELL],
                "scale {scale}: full"
            );
            let lower = Rect {
                x: 0.,
                y: 10.,
                w: 8.,
                h: 10.,
            };
            assert_eq!(
                lines_quads('▄', CELL, scale),
                vec![lower],
                "scale {scale}: lower half"
            );
        }
    }

    #[test]
    fn a_cell_with_no_width_draws_nothing() {
        let flat = Rect { w: 0., ..CELL };
        assert!(lines_quads('─', flat, 1.).is_empty());
        assert!(lines_quads('█', flat, 1.).is_empty());
    }

    #[test]
    fn each_arm_reaches_its_cell_edge_and_no_other_edge() {
        for scale in [1., 2.] {
            let quads_of = |c| lines_quads(c, CELL, scale);
            let dev_cols = (CELL.w * scale) as usize;
            let dev_rows = (CELL.h * scale) as usize;
            // The middle of the first and last device pixel on each side.
            let inset = 0.5 / scale;
            for c in LINE_GLYPHS.chars() {
                let quads = quads_of(c);
                let arms = arms_of(c);
                // Sample points: the centre of each device pixel along the x and y axes.
                let xs: Vec<f32> = (0..dev_cols).map(|i| (i as f32 + 0.5) / scale).collect();
                let ys: Vec<f32> = (0..dev_rows).map(|j| (j as f32 + 0.5) / scale).collect();
                let hit_at_y = |y: f32| xs.iter().any(|&x| covered(&quads, x, y));
                let hit_at_x = |x: f32| ys.iter().any(|&y| covered(&quads, x, y));
                let reaches = (
                    hit_at_y(inset),
                    hit_at_x(CELL.w - inset),
                    hit_at_y(CELL.h - inset),
                    hit_at_x(inset),
                );
                assert_eq!(
                    reaches,
                    (
                        arms.up.is_some(),
                        arms.right.is_some(),
                        arms.down.is_some(),
                        arms.left.is_some()
                    ),
                    "{c} at scale {scale}: (top, right, bottom, left)"
                );
            }
        }
    }

    #[test]
    fn a_vertical_line_spans_the_cell_height_with_no_gap_at_scale_1_and_2() {
        for scale in [1., 2.] {
            let cell = Rect {
                x: 10.,
                y: 20.,
                w: 8.,
                h: 20.,
            };
            let quads = lines_quads('│', cell, scale);
            let top = quads.iter().map(|q| q.y).fold(f32::MAX, f32::min);
            let bottom = quads.iter().map(|q| q.y + q.h).fold(f32::MIN, f32::max);
            assert_eq!((top, bottom), (cell.y, cell.y + cell.h), "scale {scale}");
            // Two cells one under the other: every device row of both is inside a quad.
            let below = Rect {
                y: cell.y + cell.h,
                ..cell
            };
            let mut both = quads.clone();
            both.extend(lines_quads('│', below, scale));
            let x = cell.x + cell.w / 2.;
            let device_rows = (2. * cell.h * scale) as usize;
            for row in 0..device_rows {
                let y = cell.y + (row as f32 + 0.5) / scale;
                assert!(
                    covered(&both, x, y),
                    "scale {scale}: device row {row} has a gap"
                );
            }
        }
    }

    #[test]
    fn a_light_line_is_one_logical_pixel_and_a_heavy_line_two_at_any_scale() {
        for scale in [1., 2.] {
            let light = lines_quads('─', CELL, scale);
            assert!(light.iter().all(|q| q.h == 1.), "scale {scale}: light");
            let heavy = lines_quads('━', CELL, scale);
            assert!(heavy.iter().all(|q| q.h == 2.), "scale {scale}: heavy");
        }
    }

    #[test]
    fn a_double_line_is_two_one_pixel_lines_two_pixels_apart() {
        let quads = lines_quads('═', CELL, 1.);
        // Rows 9 and 11 are the two lines; row 10 between them is open.
        assert!(covered(&quads, 4.5, 9.5));
        assert!(covered(&quads, 4.5, 11.5));
        assert!(!covered(&quads, 4.5, 10.5));
    }

    #[test]
    fn a_double_corner_closes_its_outer_corner_and_leaves_its_inner_channel_open() {
        let quads = lines_quads('╔', CELL, 1.);
        // The outer corner is closed.
        assert!(covered(&quads, 3.5, 9.5));
        // The outer top line closes the gap above the channel.
        assert!(covered(&quads, 4.5, 9.5));
        // The inner corner is closed, and the channel under the outer top line is open.
        assert!(covered(&quads, 5.5, 11.5));
        assert!(!covered(&quads, 4.5, 11.5));
    }

    #[test]
    fn a_mirrored_double_corner_closes_its_outer_corner_and_leaves_its_channel_open() {
        let quads = lines_quads('╗', CELL, 1.);
        // The outer top line runs left to the outer right stem, so the top corner is closed.
        assert!(covered(&quads, 5.5, 9.5));
        assert!(covered(&quads, 4.5, 9.5));
        // The inner stem joins the inner bar, and the gap between the two stems stays open below it.
        assert!(covered(&quads, 3.5, 11.5));
        assert!(!covered(&quads, 4.5, 11.5));
    }

    #[test]
    fn a_double_t_keeps_the_channel_of_its_stem_open_where_its_bars_join() {
        // `╠`: the stem is full height, and both bars join its inner line only.
        let quads = lines_quads('╠', CELL, 1.);
        assert!(covered(&quads, 3.5, 0.5));
        assert!(covered(&quads, 3.5, 19.5));
        assert!(covered(&quads, 5.5, 9.5));
        assert!(covered(&quads, 5.5, 11.5));
        assert!(!covered(&quads, 4.5, 9.5));
    }

    #[test]
    fn a_double_bar_with_a_stem_below_hangs_the_stem_from_its_inner_line() {
        // `╦`: the bar runs the full width, and the stem hangs from the inner (lower) line.
        let quads = lines_quads('╦', CELL, 1.);
        assert!(covered(&quads, 0.5, 9.5));
        assert!(!covered(&quads, 0.5, 10.5));
        assert!(covered(&quads, 3.5, 19.5));
        assert!(!covered(&quads, 3.5, 10.5));
    }

    #[test]
    fn a_t_junction_joins_its_stem_and_its_bar_at_the_centre() {
        let quads = lines_quads('├', CELL, 1.);
        // The stem is the device column 4, the bar is row 10. The stem runs the full height, the
        // bar reaches the right edge, and the centre is covered.
        assert!(covered(&quads, 4.5, 0.5));
        assert!(covered(&quads, 4.5, 19.5));
        assert!(covered(&quads, 7.5, 10.5));
        assert!(covered(&quads, 4.5, 10.5));
        assert!(!covered(&quads, 0.5, 10.5));
    }

    #[test]
    fn a_corner_has_no_notch_where_its_two_arms_meet() {
        // `┘`: the vertical arm ends at the bottom of the bar, and the bar ends at the right
        // of the stem. The pixel where they meet is covered.
        let quads = lines_quads('┘', CELL, 1.);
        assert!(covered(&quads, 4.5, 10.5));
        assert!(covered(&quads, 4.5, 0.5));
        assert!(covered(&quads, 0.5, 10.5));
    }
}
