//! Terminal grid metrics: how many cells fit a view, and the cell size herdr needs in device pixels.

use contr_herdr::endpoint::messages::{
    CellPx, GridSize, MAX_CELL_PX, MAX_GRID_CELLS, MAX_GRID_SIDE,
};

/// Cell size in logical pixels, and the padding kept on every side of the grid.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Metrics {
    pub cell_w: f32,
    pub cell_h: f32,
    pub pad: f32,
}

/// The grid that fits a view of `width_px` × `height_px` logical pixels once `pad` is kept on every
/// side. `None` when fewer than 2 columns or 2 rows fit, or when a cell has no size. The grid is
/// clamped to herdr's limits: at most `MAX_GRID_SIDE` per side and `MAX_GRID_CELLS` in all. Rows give
/// way first, so a very wide view keeps all its columns.
pub fn grid_for(width_px: f32, height_px: f32, m: &Metrics) -> Option<GridSize> {
    if !has_size(m.cell_w) || !has_size(m.cell_h) {
        return None;
    }
    let cols = ((width_px - 2. * m.pad) / m.cell_w).floor();
    let rows = ((height_px - 2. * m.pad) / m.cell_h).floor();
    if !(2.0..).contains(&cols) || !(2.0..).contains(&rows) {
        return None;
    }
    // Both are at least 2 here. Clamped to the side limit they are finite, and `cols` is at
    // most `MAX_GRID_SIDE`, so the cell limit leaves at least 244 rows.
    let cols = cols.min(f32::from(MAX_GRID_SIDE)) as u32;
    let rows = (rows.min(f32::from(MAX_GRID_SIDE)) as u32).min(MAX_GRID_CELLS / cols);
    Some(GridSize {
        cols: cols as u16,
        rows: rows as u16,
    })
}

/// The cell size in device pixels at `scale`, rounded to whole pixels. herdr takes this at resize.
/// Each side is at least 1 and at most `MAX_CELL_PX`.
pub fn cell_px(m: &Metrics, scale: f32) -> CellPx {
    CellPx {
        width: device_px(m.cell_w * scale),
        height: device_px(m.cell_h * scale),
    }
}

/// A side in device pixels: rounded, then kept within 1..=`MAX_CELL_PX`. A NaN becomes 1.
fn device_px(side: f32) -> u32 {
    (side.round() as u32).clamp(1, MAX_CELL_PX)
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
    fn the_grid_is_clamped_to_herdrs_limits() {
        let unit = Metrics {
            cell_w: 1.,
            cell_h: 1.,
            pad: 0.,
        };
        // Each side is capped at 4096; then 1,000,000 cells leave 244 rows.
        assert_eq!(
            grid_for(5000., 5000., &unit),
            Some(GridSize {
                cols: 4096,
                rows: 244
            })
        );
        // 3000 × 3000 is 9,000,000 cells: the rows give way to 1,000,000 / 3000 = 333.
        assert_eq!(
            grid_for(3000., 3000., &unit),
            Some(GridSize {
                cols: 3000,
                rows: 333
            })
        );
        // 1000 × 1500 is 1,500,000 cells: 1000 rows fit.
        assert_eq!(
            grid_for(1000., 1500., &unit),
            Some(GridSize {
                cols: 1000,
                rows: 1000
            })
        );
        // An unbounded view is clamped, not refused.
        assert_eq!(
            grid_for(f32::INFINITY, f32::INFINITY, &unit),
            Some(GridSize {
                cols: 4096,
                rows: 244
            })
        );
    }

    #[test]
    fn cell_px_is_at_least_one_device_pixel() {
        let tiny = Metrics {
            cell_w: 0.2,
            cell_h: 0.4,
            pad: 0.,
        };
        assert_eq!(
            cell_px(&tiny, 1.),
            CellPx {
                width: 1,
                height: 1
            }
        );
        let no_size = Metrics {
            cell_w: f32::NAN,
            ..CELL
        };
        assert_eq!(cell_px(&no_size, 1.).width, 1);
    }

    #[test]
    fn cell_px_is_at_most_herdrs_limit() {
        let huge = Metrics {
            cell_w: 1e9,
            cell_h: f32::INFINITY,
            pad: 0.,
        };
        assert_eq!(
            cell_px(&huge, 1.),
            CellPx {
                width: MAX_CELL_PX,
                height: MAX_CELL_PX
            }
        );
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
