//! Terminal grid metrics: how many cells fit a view, and the cell size herdr needs in device pixels.

use contr_herdr::endpoint::messages::{CellPx, GridSize};

/// Cell size in logical pixels, and the padding kept on every side of the grid.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Metrics {
    pub cell_w: f32,
    pub cell_h: f32,
    pub pad: f32,
}

/// The grid that fits a view of `width_px` × `height_px` logical pixels once `pad` is kept on every
/// side. `None` when fewer than 2 columns or 2 rows fit, or when a cell has no size.
pub fn grid_for(width_px: f32, height_px: f32, m: &Metrics) -> Option<GridSize> {
    if !has_size(m.cell_w) || !has_size(m.cell_h) {
        return None;
    }
    let cols = ((width_px - 2. * m.pad) / m.cell_w).floor();
    let rows = ((height_px - 2. * m.pad) / m.cell_h).floor();
    if !(2.0..).contains(&cols) || !(2.0..).contains(&rows) {
        return None;
    }
    Some(GridSize {
        cols: cols as u16,
        rows: rows as u16,
    })
}

/// The cell size in device pixels at `scale`, rounded to whole pixels. herdr takes this at resize.
pub fn cell_px(m: &Metrics, scale: f32) -> CellPx {
    CellPx {
        width: (m.cell_w * scale).round() as u32,
        height: (m.cell_h * scale).round() as u32,
    }
}

/// True for a finite, positive size. NaN and infinity count as no size.
fn has_size(size: f32) -> bool {
    size.is_finite() && size > 0.
}

#[cfg(test)]
mod tests {
    use super::*;

    const CELL: Metrics = Metrics {
        cell_w: 7.8,
        cell_h: 20.,
        pad: 8.,
    };

    #[test]
    fn grid_fits_the_view_minus_the_padding() {
        // (720 - 16) / 7.8 = 90.25 columns, (834 - 16) / 20 = 40.9 rows.
        assert_eq!(
            grid_for(720., 834., &CELL),
            Some(GridSize { cols: 90, rows: 40 })
        );
    }

    #[test]
    fn two_by_two_is_the_smallest_grid() {
        let m = Metrics {
            cell_w: 8.,
            cell_h: 20.,
            pad: 8.,
        };
        assert_eq!(grid_for(32., 56., &m), Some(GridSize { cols: 2, rows: 2 }));
    }

    #[test]
    fn a_view_with_one_column_or_one_row_has_no_grid() {
        let m = Metrics {
            cell_w: 8.,
            cell_h: 20.,
            pad: 8.,
        };
        assert_eq!(grid_for(31., 56., &m), None); // one column
        assert_eq!(grid_for(32., 55., &m), None); // one row
        assert_eq!(grid_for(10., 10., &CELL), None); // smaller than the padding
    }

    #[test]
    fn a_nan_or_negative_view_has_no_grid() {
        assert_eq!(grid_for(f32::NAN, 834., &CELL), None);
        assert_eq!(grid_for(720., -50., &CELL), None);
    }

    #[test]
    fn a_cell_without_a_size_has_no_grid() {
        let zero_width = Metrics { cell_w: 0., ..CELL };
        let nan_height = Metrics {
            cell_h: f32::NAN,
            ..CELL
        };
        assert_eq!(grid_for(720., 834., &zero_width), None);
        assert_eq!(grid_for(720., 834., &nan_height), None);
    }

    #[test]
    fn cell_px_rounds_the_cell_size_at_the_scale() {
        assert_eq!(
            cell_px(&CELL, 2.),
            CellPx {
                width: 16,
                height: 40
            }
        );
        // 7.8 × 1.5 = 11.7 rounds to 12; 20 × 1.5 = 30.
        assert_eq!(
            cell_px(&CELL, 1.5),
            CellPx {
                width: 12,
                height: 30
            }
        );
        assert_eq!(
            cell_px(&CELL, 1.),
            CellPx {
                width: 8,
                height: 20
            }
        );
    }
}
