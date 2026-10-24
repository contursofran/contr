// Adapted from Herdr UI (Apache-2.0); changes: cell lookup over the pane's inner rect, not its outer rect; no popup rule; unit tests.

//! Which pane of the shown surface a click is over.

use contr_herdr::endpoint::surface::{Rect, Surface, SurfacePane};

use super::grid::Metrics;

/// The pane whose inner rect holds the cell under the point (`x`, `y`). Both are in pixels from the
/// top-left corner of cell (0, 0), so a negative value lies in the padding. A point on a pane's
/// border, or in a cell that no pane holds, gives `None`. So does a point outside the surface.
pub(super) fn pane_at(surface: &Surface, m: Metrics, x: f32, y: f32) -> Option<&SurfacePane> {
    let col = (x / m.cell_w).floor();
    let row = (y / m.cell_h).floor();
    let (cols, rows) = (f32::from(surface.size.cols), f32::from(surface.size.rows));
    // A NaN or infinite point, or a zero cell size, fails these checks too.
    if !(0. ..cols).contains(&col) || !(0. ..rows).contains(&row) {
        return None;
    }
    let (col, row) = (col as u16, row as u16);
    surface
        .panes
        .iter()
        .find(|pane| holds(pane.inner_rect, col, row))
}

/// True when cell (`col`, `row`) lies inside `rect`.
fn holds(rect: Rect, col: u16, row: u16) -> bool {
    let (col, row) = (u32::from(col), u32::from(row));
    col >= u32::from(rect.x)
        && col < u32::from(rect.x) + u32::from(rect.width)
        && row >= u32::from(rect.y)
        && row < u32::from(rect.y) + u32::from(rect.height)
}

#[cfg(test)]
mod tests {
    use contr_herdr::endpoint::messages::GridSize;

    use super::*;

    /// Cells are 10 × 20 pixels, with no padding: a point's cell is its pixels over the cell size.
    const CELL: Metrics = Metrics {
        cell_w: 10.,
        cell_h: 20.,
        pad: 0.,
    };

    /// Two panes side by side in a 12 × 4 surface. Each pane's rect is 6 cells wide and its inner
    /// rect leaves a one-cell border. `p1` holds cols 1–4 and `p2` holds cols 7–10, rows 1–2.
    fn two_panes() -> Surface {
        let pane = |id: &str, x: u16| SurfacePane {
            pane_id: id.to_owned(),
            rect: Rect {
                x,
                y: 0,
                width: 6,
                height: 4,
            },
            inner_rect: Rect {
                x: x + 1,
                y: 1,
                width: 4,
                height: 2,
            },
            focused: false,
            alternate_screen: false,
        };
        Surface {
            boot_id: "boot".to_owned(),
            projection_revision: 1,
            revision: 1,
            size: GridSize { cols: 12, rows: 4 },
            rows: Vec::new(),
            cursor: None,
            panes: vec![pane("p1", 0), pane("p2", 6)],
        }
    }

    /// The pane id at `(x, y)`, or `None` when no pane holds that point.
    fn id_at(surface: &Surface, x: f32, y: f32) -> Option<String> {
        pane_at(surface, CELL, x, y).map(|pane| pane.pane_id.clone())
    }

    #[test]
    fn a_point_inside_the_inner_rect_is_that_pane() {
        let surface = two_panes();
        // The first and the last inner cell of p1: cols 1 and 4, rows 1 and 2.
        assert_eq!(id_at(&surface, 15., 25.).as_deref(), Some("p1"));
        assert_eq!(id_at(&surface, 45., 45.).as_deref(), Some("p1"));
    }

    #[test]
    fn a_point_on_a_pane_border_is_no_pane() {
        let surface = two_panes();
        // The corner of p1's rect, its right edge (col 5) and its bottom edge (row 3), and the
        // left edge of p2 (col 6). Each is in a rect but not in an inner rect.
        assert_eq!(id_at(&surface, 5., 5.), None);
        assert_eq!(id_at(&surface, 55., 45.), None);
        assert_eq!(id_at(&surface, 15., 65.), None);
        assert_eq!(id_at(&surface, 65., 45.), None);
    }

    #[test]
    fn a_point_outside_every_pane_is_no_pane() {
        let mut surface = two_panes();
        // With p2 gone, cols 7–11 are in no pane.
        surface.panes.truncate(1);
        assert_eq!(id_at(&surface, 85., 25.), None);
        // Past the last column and the last row of the surface.
        assert_eq!(id_at(&surface, 120., 25.), None);
        assert_eq!(id_at(&surface, 15., 80.), None);
    }

    #[test]
    fn a_point_in_the_padding_is_no_pane() {
        let surface = two_panes();
        assert_eq!(id_at(&surface, -1., 25.), None);
        assert_eq!(id_at(&surface, 15., -1.), None);
    }

    #[test]
    fn two_panes_side_by_side_hold_their_own_cells() {
        let surface = two_panes();
        assert_eq!(id_at(&surface, 45., 25.).as_deref(), Some("p1"));
        assert_eq!(id_at(&surface, 75., 25.).as_deref(), Some("p2"));
        assert_eq!(id_at(&surface, 105., 45.).as_deref(), Some("p2"));
    }

    #[test]
    fn a_point_on_a_cell_edge_belongs_to_the_cell_after_it() {
        let surface = two_panes();
        // Col 7 starts at 70 px: 70 is p2's first inner cell, 69.9 is p2's border.
        assert_eq!(id_at(&surface, 70., 25.).as_deref(), Some("p2"));
        assert_eq!(id_at(&surface, 69.9, 25.), None);
    }

    #[test]
    fn a_non_finite_point_or_a_zero_cell_is_no_pane() {
        let surface = two_panes();
        assert_eq!(id_at(&surface, f32::NAN, 25.), None);
        assert_eq!(id_at(&surface, 15., f32::INFINITY), None);
        let no_cells = Metrics { cell_w: 0., ..CELL };
        assert!(pane_at(&surface, no_cells, 15., 25.).is_none());
    }
}
