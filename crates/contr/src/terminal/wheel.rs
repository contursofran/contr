// Adapted from Herdr UI (Apache-2.0); changes: cell and pane lookup on the surface grid, one
// event per notch carrying the notch's lines, horizontal motion only to programs that report the
// mouse, unit tests.

//! Wheel and trackpad motion to herdr input events. herdr keeps the scrollback: contr sends only
//! wheel steps, and the server decides whether they scroll its history or reach the program.
//!
//! Everything here works on plain `f32` and `u16`, so the mapping is unit tested without a
//! window. The exceptions are `ScrollDelta` and `Modifiers`, which GPUI gives the view.

use contr_herdr::endpoint::messages::{GridSize, InputEvent, WheelDir, mods};
use contr_herdr::endpoint::surface::{Surface, SurfacePane};
use gpui::{Modifiers, ScrollDelta};

use super::grid::Metrics;
use super::panes::holds;

/// Lines one wheel notch scrolls in herdr's history (D88). herdr turns a wheel event for a
/// program into one mouse report or one arrow key and ignores the count there.
pub const WHEEL_LINES: f32 = 3.;

/// Most lines one event may carry. herdr closes the connection when a batch asks for more than
/// 4096 lines in all, so this stays well below that.
pub const MAX_WHEEL_LINES: u16 = 512;

/// Most events one batch may carry for a program, which gets one event per notch. At
/// `WHEEL_LINES` each, these are 192 lines at most.
pub const MAX_WHEEL_EVENTS: usize = 64;

/// The lines a `ScrollDelta` asks for, up being positive. A trackpad reports pixels, one cell
/// height per line; a mouse wheel reports lines. `0` when the cell height is not a positive,
/// finite number.
pub fn delta_lines(delta: ScrollDelta, cell_h: f32) -> f32 {
    if !cell_h.is_finite() || cell_h <= 0. {
        return 0.;
    }
    let lines = match delta {
        ScrollDelta::Pixels(p) => f32::from(p.y) / cell_h,
        ScrollDelta::Lines(l) => l.y * WHEEL_LINES,
    };
    if lines.is_finite() { lines } else { 0. }
}

/// The columns a `ScrollDelta` asks for, left being positive, as `delta_lines` computes lines.
pub fn delta_columns(delta: ScrollDelta, cell_w: f32) -> f32 {
    let columns = match delta {
        ScrollDelta::Pixels(p) => f32::from(p.x) / cell_w,
        ScrollDelta::Lines(l) => l.x * WHEEL_LINES,
    };
    if columns.is_finite() && cell_w.is_finite() && cell_w > 0. {
        columns
    } else {
        0.
    }
}

/// Whether the event moves mostly sideways. GPUI turns Shift with a vertical wheel into
/// horizontal motion itself, so a sideways delta is a horizontal wheel and not just drift; a
/// mostly vertical swipe's sideways drift is not a horizontal step.
pub fn sideways(delta: ScrollDelta) -> bool {
    let (x, y) = match delta {
        ScrollDelta::Pixels(p) => (f32::from(p.x), f32::from(p.y)),
        ScrollDelta::Lines(l) => (l.x, l.y),
    };
    x.is_finite() && y.is_finite() && x.abs() > y.abs()
}

/// The sub-line motion kept between events, for one pane. A change of pane, or motion the other
/// way, drops the remainder first: a swipe that turns around is not a step.
#[derive(Default)]
pub struct WheelAccum {
    lines: f32,
    columns: f32,
    pane: Option<String>,
}

impl WheelAccum {
    /// Adds `lines` of motion, up being positive, for `pane`.
    pub fn add(&mut self, pane: &str, lines: f32) {
        self.retarget(pane);
        add_remainder(&mut self.lines, lines);
    }

    /// Adds `columns` of motion, left being positive, for `pane`.
    pub fn add_columns(&mut self, pane: &str, columns: f32) {
        self.retarget(pane);
        add_remainder(&mut self.columns, columns);
    }

    /// The whole `unit`s in the vertical remainder, toward zero. The rest is kept.
    pub fn take(&mut self, unit: f32) -> i32 {
        take_remainder(&mut self.lines, unit)
    }

    /// The whole `unit`s in the horizontal remainder, toward zero. The rest is kept.
    pub fn take_columns(&mut self, unit: f32) -> i32 {
        take_remainder(&mut self.columns, unit)
    }

    /// Drops the remainder, for when no pane takes the wheel.
    pub fn clear(&mut self) {
        *self = Self::default();
    }

    /// Drops the remainder when `pane` is not the pane it belongs to.
    fn retarget(&mut self, pane: &str) {
        if self.pane.as_deref() != Some(pane) {
            self.lines = 0.;
            self.columns = 0.;
            self.pane = Some(pane.to_owned());
        }
    }
}

fn add_remainder(total: &mut f32, delta: f32) {
    if !delta.is_finite() {
        return;
    }
    if delta != 0. && delta.signum() != total.signum() {
        *total = 0.;
    }
    *total += delta;
}

fn take_remainder(total: &mut f32, unit: f32) -> i32 {
    if !total.is_finite() {
        *total = 0.;
        return 0;
    }
    if !unit.is_finite() || unit <= 0. {
        return 0;
    }
    let steps = (*total / unit).trunc();
    if !steps.is_finite() {
        *total = 0.;
        return 0;
    }
    *total -= steps * unit;
    steps as i32
}

/// The surface cell under `pos`, a window point, with `origin` the grid's top-left corner in the
/// same coordinates. `None` in the padding, outside the grid, or with a point or cell that has no
/// size.
pub fn cell_at(
    pos: (f32, f32),
    origin: (f32, f32),
    m: &Metrics,
    size: GridSize,
) -> Option<(u16, u16)> {
    if !m.cell_w.is_finite() || !m.cell_h.is_finite() || m.cell_w <= 0. || m.cell_h <= 0. {
        return None;
    }
    let x = pos.0 - origin.0 - m.pad;
    let y = pos.1 - origin.1 - m.pad;
    let col = (x / m.cell_w).floor();
    let row = (y / m.cell_h).floor();
    // A NaN or infinite point fails these checks too.
    if !(0. ..f32::from(size.cols)).contains(&col) || !(0. ..f32::from(size.rows)).contains(&row) {
        return None;
    }
    Some((col as u16, row as u16))
}

/// The pane whose rect holds `cell`. The rect has the pane's border and its scrollbar column, so
/// the wheel over the scrollbar scrolls the pane.
pub fn pane_at(surface: &Surface, cell: (u16, u16)) -> Option<&SurfacePane> {
    surface
        .panes
        .iter()
        .find(|pane| holds(pane.rect, cell.0, cell.1))
}

/// `cell` relative to the pane's inner rect, clamped into it. The wheel event carries this
/// position, which herdr reads as the cell under the pointer inside the pane.
pub fn pane_cell(pane: &SurfacePane, cell: (u16, u16)) -> (u16, u16) {
    let inside = |value: u16, start: u16, width: u16| {
        value.saturating_sub(start).min(width.saturating_sub(1))
    };
    (
        inside(cell.0, pane.inner_rect.x, pane.inner_rect.width),
        inside(cell.1, pane.inner_rect.y, pane.inner_rect.height),
    )
}

/// Whether the pane hands the wheel to its program: it reads the mouse, or it is on the
/// alternate screen, where herdr sends an arrow key. Otherwise the wheel scrolls herdr's history.
pub fn reports(pane: &SurfacePane) -> bool {
    pane.mouse_reporting || pane.alternate_screen
}

/// The wheel events for `steps` lines, up being positive. A program that takes the wheel gets one
/// event per notch, as herdr's own client sends; the history gets one event with the whole count.
pub fn wheel_events(pane: &SurfacePane, cell: (u16, u16), steps: i32, mods: u8) -> Vec<InputEvent> {
    axis_events(pane, cell, steps, mods, WheelDir::Up, WheelDir::Down)
}

/// The wheel events for `steps` columns, left being positive. herdr turns a horizontal wheel into
/// a mouse report only, so a pane that does not read the mouse gets nothing.
pub fn wheel_column_events(
    pane: &SurfacePane,
    cell: (u16, u16),
    steps: i32,
    mods: u8,
) -> Vec<InputEvent> {
    if !reports(pane) {
        return Vec::new();
    }
    axis_events(pane, cell, steps, mods, WheelDir::Left, WheelDir::Right)
}

fn axis_events(
    pane: &SurfacePane,
    cell: (u16, u16),
    steps: i32,
    mods: u8,
    positive: WheelDir,
    negative: WheelDir,
) -> Vec<InputEvent> {
    if steps == 0 {
        return Vec::new();
    }
    let dir = if steps > 0 { positive } else { negative };
    let count = steps.unsigned_abs();
    let (events, lines) = if reports(pane) {
        (
            count.min(MAX_WHEEL_EVENTS as u32) as usize,
            WHEEL_LINES as u16,
        )
    } else {
        (1, count.min(u32::from(MAX_WHEEL_LINES)) as u16)
    };
    std::iter::repeat_n(
        InputEvent::Wheel {
            dir,
            column: cell.0,
            row: cell.1,
            mods,
            lines,
        },
        events,
    )
    .collect()
}

/// The modifier bits of a wheel event, as herdr reads them.
pub fn wheel_mods(m: &Modifiers) -> u8 {
    let mut flags = 0;
    if m.shift {
        flags |= mods::SHIFT;
    }
    if m.control {
        flags |= mods::CONTROL;
    }
    if m.alt {
        flags |= mods::ALT;
    }
    flags
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use contr_herdr::endpoint::surface::Rect;
    use gpui::{point, px};

    use super::*;

    const CELL: Metrics = Metrics {
        cell_w: 10.,
        cell_h: 20.,
        pad: 8.,
    };

    fn pane(id: &str, mouse_reporting: bool, alternate_screen: bool) -> SurfacePane {
        SurfacePane {
            pane_id: id.to_owned(),
            rect: Rect {
                x: 0,
                y: 0,
                width: 6,
                height: 4,
            },
            inner_rect: Rect {
                x: 1,
                y: 1,
                width: 4,
                height: 2,
            },
            mouse_reporting,
            alternate_screen,
            ..Default::default()
        }
    }

    fn surface() -> Surface {
        Surface {
            boot_id: "boot".to_owned(),
            projection_revision: 1,
            revision: 1,
            size: GridSize { cols: 12, rows: 4 },
            rows: Vec::new(),
            cursor: None,
            panes: vec![pane("p1", false, false), {
                let mut p = pane("p2", false, false);
                p.rect.x = 6;
                p.inner_rect.x = 7;
                p
            }],
        }
    }

    fn event(dir: WheelDir, column: u16, row: u16, lines: u16) -> InputEvent {
        InputEvent::Wheel {
            dir,
            column,
            row,
            mods: 0,
            lines,
        }
    }

    fn close(a: f32, b: f32) -> bool {
        (a - b).abs() < 1e-5
    }

    #[test]
    fn delta_lines_reads_pixels_and_lines() {
        // A trackpad: one cell height of pixels is one line.
        assert!(close(
            delta_lines(ScrollDelta::Pixels(point(px(0.), px(40.))), 20.),
            2.
        ));
        assert!(close(
            delta_lines(ScrollDelta::Pixels(point(px(0.), px(-10.))), 20.),
            -0.5
        ));
        // A wheel: one notch is three lines.
        assert!(close(
            delta_lines(ScrollDelta::Lines(point(0., 1.)), 20.),
            3.
        ));
        assert!(close(
            delta_lines(ScrollDelta::Lines(point(0., -2.)), 20.),
            -6.
        ));
    }

    #[test]
    fn delta_lines_without_a_cell_height_is_zero() {
        let event = ScrollDelta::Pixels(point(px(0.), px(40.)));
        for cell_h in [0., -20., f32::NAN, f32::INFINITY] {
            assert_eq!(delta_lines(event, cell_h), 0., "{cell_h}");
        }
        // A non-finite delta is not motion either.
        let broken = ScrollDelta::Pixels(point(px(f32::NAN), px(f32::INFINITY)));
        assert_eq!(delta_lines(broken, 20.), 0.);
    }

    #[test]
    fn delta_columns_reads_pixels_and_lines() {
        assert!(close(
            delta_columns(ScrollDelta::Pixels(point(px(30.), px(0.))), 10.),
            3.
        ));
        assert!(close(
            delta_columns(ScrollDelta::Lines(point(-1., 0.)), 10.),
            -3.
        ));
        assert_eq!(delta_columns(ScrollDelta::Lines(point(1., 0.)), 0.), 0.);
    }

    #[test]
    fn a_sideways_event_is_mostly_horizontal() {
        assert!(sideways(ScrollDelta::Lines(point(2., 1.))));
        assert!(!sideways(ScrollDelta::Lines(point(1., 2.))));
        assert!(!sideways(ScrollDelta::Lines(point(1., 1.))));
        assert!(sideways(ScrollDelta::Pixels(point(px(-30.), px(10.)))));
        assert!(!sideways(ScrollDelta::Pixels(point(px(f32::NAN), px(0.)))));
    }

    #[test]
    fn a_trackpad_remainder_adds_up_to_whole_lines() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 0.6);
        assert_eq!(wheel.take(1.), 0);
        assert!(close(wheel.lines, 0.6));
        wheel.add("p1", 0.6);
        assert_eq!(wheel.take(1.), 1);
        assert!(close(wheel.lines, 0.2));
        // The kept fifth of a line finishes the next one.
        wheel.add("p1", 0.8);
        assert_eq!(wheel.take(1.), 1);
        assert!(close(wheel.lines, 0.));
    }

    #[test]
    fn a_change_of_direction_drops_the_remainder() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 0.9);
        wheel.add("p1", -0.4);
        // Only the motion after the turn counts.
        assert!(close(wheel.lines, -0.4));
        assert_eq!(wheel.take(1.), 0);
        wheel.add("p1", -0.6);
        assert_eq!(wheel.take(1.), -1);
    }

    #[test]
    fn a_change_of_pane_drops_the_remainder() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 0.9);
        wheel.add("p2", 0.4);
        assert_eq!(wheel.take(1.), 0);
        assert!(close(wheel.lines, 0.4));
    }

    #[test]
    fn take_counts_whole_units_toward_zero() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 7.);
        assert_eq!(wheel.take(3.), 2);
        assert!(close(wheel.lines, 1.));
        assert_eq!(wheel.take(3.), 0);

        wheel.add("p1", -7.);
        assert_eq!(wheel.take(3.), -2);
        assert!(close(wheel.lines, -1.));
    }

    #[test]
    fn take_without_a_unit_keeps_the_remainder() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 2.);
        for unit in [0., -1., f32::NAN] {
            assert_eq!(wheel.take(unit), 0, "{unit}");
            assert!(close(wheel.lines, 2.));
        }
        wheel.clear();
        assert_eq!(wheel.take(1.), 0);
    }

    #[test]
    fn columns_are_kept_apart_from_lines() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 1.5);
        wheel.add_columns("p1", 1.5);
        assert_eq!(wheel.take(3.), 0);
        assert_eq!(wheel.take_columns(3.), 0);
        wheel.add_columns("p1", 1.5);
        assert_eq!(wheel.take_columns(3.), 1);
        assert!(close(wheel.lines, 1.5));
    }

    #[test]
    fn clear_drops_both_remainders() {
        let mut wheel = WheelAccum::default();
        wheel.add("p1", 2.5);
        wheel.add_columns("p1", 2.5);
        wheel.clear();
        assert_eq!(wheel.take(1.), 0);
        assert_eq!(wheel.take_columns(1.), 0);
        wheel.add("p1", 0.5);
        assert_eq!(wheel.take(1.), 0);
    }

    #[test]
    fn cell_at_takes_the_padding_off_the_point() {
        // The grid starts at (100, 50); the padding is 8 pixels.
        let at = |x, y| cell_at((x, y), (100., 50.), &CELL, GridSize { cols: 12, rows: 4 });
        assert_eq!(at(108., 58.), Some((0, 0)));
        // The last pixel of a cell belongs to it; the next one starts the cell after.
        assert_eq!(at(117.9, 77.9), Some((0, 0)));
        assert_eq!(at(118., 78.), Some((1, 1)));
        assert_eq!(at(108. + 110., 58. + 20.), Some((11, 1)));
    }

    #[test]
    fn cell_at_is_none_in_the_padding_and_outside_the_grid() {
        let at = |x, y| cell_at((x, y), (100., 50.), &CELL, GridSize { cols: 12, rows: 4 });
        // In the padding, past the last cell, and non-finite.
        assert_eq!(at(100., 58.), None);
        assert_eq!(at(107.9, 58.), None);
        assert_eq!(at(108. + 120., 58.), None);
        assert_eq!(at(108., 58. + 80.), None);
        assert_eq!(at(f32::NAN, 58.), None);
        assert_eq!(at(108., f32::INFINITY), None);
    }

    #[test]
    fn cell_at_without_a_cell_size_is_none() {
        let size = GridSize { cols: 12, rows: 4 };
        let no_width = Metrics { cell_w: 0., ..CELL };
        let nan_height = Metrics {
            cell_h: f32::NAN,
            ..CELL
        };
        assert_eq!(cell_at((108., 58.), (100., 50.), &no_width, size), None);
        assert_eq!(cell_at((108., 58.), (100., 50.), &nan_height, size), None);
    }

    #[test]
    fn pane_at_holds_the_scrollbar_column() {
        let surface = surface();
        // p1's last rect cell is column 5, its last inner column 4.
        assert_eq!(
            pane_at(&surface, (5, 2)).map(|pane| pane.pane_id.as_str()),
            Some("p1")
        );
        assert_eq!(
            pane_at(&surface, (4, 2)).map(|pane| pane.pane_id.as_str()),
            Some("p1")
        );
        // Column 6 is p2's rect, column 7 its first inner column.
        assert_eq!(
            pane_at(&surface, (6, 2)).map(|pane| pane.pane_id.as_str()),
            Some("p2")
        );
        assert_eq!(
            pane_at(&surface, (7, 2)).map(|pane| pane.pane_id.as_str()),
            Some("p2")
        );
        // Below the panes.
        assert_eq!(pane_at(&surface, (2, 4)), None);
    }

    #[test]
    fn pane_cell_is_relative_to_the_inner_rect_and_clamped() {
        let surface = surface();
        let p1 = &surface.panes[0];
        assert_eq!(pane_cell(p1, (1, 1)), (0, 0));
        assert_eq!(pane_cell(p1, (4, 2)), (3, 1));
        // The border and the scrollbar column clamp to the first and last inner cells.
        assert_eq!(pane_cell(p1, (0, 0)), (0, 0));
        assert_eq!(pane_cell(p1, (5, 3)), (3, 1));
        assert_eq!(pane_cell(p1, (u16::MAX, u16::MAX)), (3, 1));
    }

    #[test]
    fn a_history_pane_gets_one_event_with_the_whole_count() {
        let pane = pane("p1", false, false);
        assert_eq!(
            wheel_events(&pane, (2, 3), 4, mods::SHIFT),
            vec![InputEvent::Wheel {
                dir: WheelDir::Up,
                column: 2,
                row: 3,
                mods: mods::SHIFT,
                lines: 4,
            }]
        );
        // The event is capped well below herdr's batch limit; the excess is dropped.
        assert_eq!(
            wheel_events(&pane, (0, 0), 700, 0),
            vec![event(WheelDir::Up, 0, 0, MAX_WHEEL_LINES)]
        );
        assert_eq!(
            wheel_events(&pane, (0, 0), -4, 0),
            vec![event(WheelDir::Down, 0, 0, 4)]
        );
        assert!(wheel_events(&pane, (0, 0), 0, 0).is_empty());
    }

    #[test]
    fn a_reporting_pane_gets_one_event_per_notch() {
        let notch = InputEvent::Wheel {
            dir: WheelDir::Up,
            column: 2,
            row: 3,
            mods: mods::CONTROL,
            lines: 3,
        };
        for pane in [pane("p1", true, false), pane("p1", false, true)] {
            assert_eq!(
                wheel_events(&pane, (2, 3), 2, mods::CONTROL),
                vec![notch.clone(), notch.clone()]
            );
            assert_eq!(
                wheel_events(&pane, (0, 0), -1, 0),
                vec![event(WheelDir::Down, 0, 0, 3)]
            );
            // One batch at most, whatever the count.
            assert_eq!(wheel_events(&pane, (0, 0), 100, 0).len(), MAX_WHEEL_EVENTS);
        }
    }

    #[test]
    fn a_horizontal_wheel_needs_a_pane_that_reports_the_mouse() {
        let reporting = pane("p1", true, false);
        assert_eq!(
            wheel_column_events(&reporting, (2, 3), 2, 0),
            vec![
                event(WheelDir::Left, 2, 3, 3),
                event(WheelDir::Left, 2, 3, 3)
            ]
        );
        assert_eq!(
            wheel_column_events(&reporting, (0, 0), -2, 0),
            vec![
                event(WheelDir::Right, 0, 0, 3),
                event(WheelDir::Right, 0, 0, 3)
            ]
        );
        assert!(wheel_column_events(&reporting, (0, 0), 0, 0).is_empty());
        let history = pane("p1", false, false);
        assert!(wheel_column_events(&history, (0, 0), 2, 0).is_empty());
    }

    #[test]
    fn wheel_mods_carries_shift_control_and_alt() {
        assert_eq!(wheel_mods(&Modifiers::none()), 0);
        assert_eq!(
            wheel_mods(&Modifiers {
                shift: true,
                ..Default::default()
            }),
            mods::SHIFT
        );
        assert_eq!(
            wheel_mods(&Modifiers {
                shift: true,
                control: true,
                alt: true,
                ..Default::default()
            }),
            mods::SHIFT | mods::CONTROL | mods::ALT
        );
    }
}
