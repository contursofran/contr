// Adapted from Herdr UI (Apache-2.0); changes: one pane per selection, bounded by the pane's inner rect; no popups or scrollback; no hyperlink or URL words; trailing spaces trimmed on every line; no select-all unit; wide glyphs read from the wide-tail flag.

//! The selection model: the cells a pointer chooses in one pane, and the text a copy takes.
//!
//! Selection is client-local. herdr owns the terminal, and nothing here sends input to it. A
//! selection lies in one pane: its edges are surface cells, and a drag clamps to the pane's inner
//! rect, so it never leaves the pane where it started. Nothing here paints or copies. The view does
//! both, with the answers this model gives.

use std::hash::{DefaultHasher, Hash, Hasher};
use std::ops::Range;

use contr_herdr::endpoint::surface::{Cell, Rect, Surface};

use super::grid::Metrics;
use super::panes::pane_at;

/// The most text one copy takes, in bytes. A longer copy is refused, not cut short.
pub const MAX_SELECTION_BYTES: usize = 4 << 20;

/// herdr's HIDDEN style bit, as `colors.rs` and `paint.rs` read it.
const HIDDEN: u16 = 128;

/// Why a selection gives no text.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SelectionError {
    /// The pane is no longer on screen, or the surface lacks one of its rows.
    Stale,
    /// The text is longer than `MAX_SELECTION_BYTES`.
    TooLarge,
}

/// Which half of a cell the pointer sat in. Anchoring on the half, as terminal emulators do, lets a
/// single cell be selected, while a plain click, which never leaves its half, selects nothing.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
enum Side {
    Left,
    Right,
}

/// A pointer position on the cell grid. Ordered by reading order, so the two ends of a drag sort
/// into a start and an end whichever way it was made.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
struct Edge {
    row: u16,
    column: u16,
    side: Side,
}

/// The cells of one pane's inner rect, in surface coordinates. Never empty.
#[derive(Clone, Debug, PartialEq, Eq)]
struct Region {
    columns: Range<u16>,
    rows: Range<u16>,
}

impl Region {
    /// The region of `rect`, or `None` for a rect with no cells.
    fn of(rect: Rect) -> Option<Self> {
        if rect.width == 0 || rect.height == 0 {
            return None;
        }
        Some(Self {
            columns: rect.x..rect.x.saturating_add(rect.width),
            rows: rect.y..rect.y.saturating_add(rect.height),
        })
    }

    fn contains(&self, column: u16, row: u16) -> bool {
        self.columns.contains(&column) && self.rows.contains(&row)
    }

    fn last_row(&self) -> u16 {
        self.rows.end - 1
    }

    fn last_column(&self) -> u16 {
        self.columns.end - 1
    }

    /// The cell edge under a point, clamped into the region. A point above, below, left of, or
    /// right of it gives the edge on its boundary, so a drag out of the pane selects up to the
    /// pane's edge. `None` for a point that is not finite, or a cell with no size.
    fn edge(&self, x: f32, y: f32, m: Metrics) -> Option<Edge> {
        if !usable(x, y, m) {
            return None;
        }
        let row = (y / m.cell_h).floor();
        if row < f32::from(self.rows.start) {
            return Some(Edge {
                row: self.rows.start,
                column: self.columns.start,
                side: Side::Left,
            });
        }
        if row > f32::from(self.last_row()) {
            return Some(Edge {
                row: self.last_row(),
                column: self.last_column(),
                side: Side::Right,
            });
        }
        let row = row as u16;
        let column = x / m.cell_w;
        if column < f32::from(self.columns.start) {
            return Some(Edge {
                row,
                column: self.columns.start,
                side: Side::Left,
            });
        }
        if column >= f32::from(self.columns.end) {
            return Some(Edge {
                row,
                column: self.last_column(),
                side: Side::Right,
            });
        }
        Some(Edge {
            row,
            column: column.floor() as u16,
            side: if column.fract() < 0.5 {
                Side::Left
            } else {
                Side::Right
            },
        })
    }
}

/// True when a point and the cell size give a usable edge: a finite point and a positive size.
fn usable(x: f32, y: f32, m: Metrics) -> bool {
    x.is_finite()
        && y.is_finite()
        && m.cell_w.is_finite()
        && m.cell_w > 0.
        && m.cell_h.is_finite()
        && m.cell_h > 0.
}

/// How much one press takes: the half-cell under it for a single click, the word for a double
/// click, and the row of the pane for a triple click. A drag that follows grows the selection by
/// the same unit.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Unit {
    Cell,
    Word,
    Line,
}

impl From<usize> for Unit {
    fn from(clicks: usize) -> Self {
        match clicks {
            0 | 1 => Self::Cell,
            2 => Self::Word,
            _ => Self::Line,
        }
    }
}

impl Unit {
    /// The first and last edge of the unit around `edge`, in `region`.
    fn span(self, surface: &Surface, region: &Region, edge: Edge) -> (Edge, Edge) {
        let columns = match self {
            Self::Cell => return (edge, edge),
            Self::Line => region.columns.clone(),
            Self::Word => word(surface, region, edge.row, edge.column)
                .unwrap_or(edge.column..edge.column.saturating_add(1)),
        };
        (
            Edge {
                row: edge.row,
                column: columns.start,
                side: Side::Left,
            },
            Edge {
                row: edge.row,
                column: columns.end - 1,
                side: Side::Right,
            },
        )
    }
}

/// The cells of surface row `row` in `columns`, or `None` when the surface does not hold them.
fn cells_of(surface: &Surface, row: u16, columns: Range<u16>) -> Option<&[Cell]> {
    surface
        .rows
        .get(usize::from(row))?
        .get(usize::from(columns.start)..usize::from(columns.end))
}

/// The columns of the unbroken run of cells around `hit` that `belongs` accepts. Cells are indexed
/// from `start`.
fn run(start: u16, hit: usize, len: usize, belongs: impl Fn(usize) -> bool) -> Range<u16> {
    let first = (0..hit)
        .rev()
        .take_while(|&i| belongs(i))
        .last()
        .unwrap_or(hit);
    let last = (hit + 1..len)
        .take_while(|&i| belongs(i))
        .last()
        .unwrap_or(hit);
    let at = |index: usize| start + index as u16;
    at(first)..at(last) + 1
}

/// Characters that end a word even though they are printed: quotes, brackets, and the separators
/// and borders that sit between words in terminal output.
fn separates(c: char) -> bool {
    c.is_whitespace()
        || c.is_control()
        || matches!(
            c,
            '"' | '\'' | '`' | '(' | ')' | '[' | ']' | '{' | '}' | '<' | '>' | '|' | ',' | ';'
        )
        || ('\u{2500}'..='\u{259f}').contains(&c)
}

/// What a cell reads as. A concealed cell reads as a blank, and so does an empty cell.
fn shown(cell: &Cell) -> &str {
    if cell.attrs & HIDDEN != 0 || cell.text.is_empty() {
        " "
    } else {
        &cell.text
    }
}

/// The text of one row span: each cell's text, with a wide glyph's second cell skipped and the
/// trailing spaces removed.
fn line_text(cells: &[Cell]) -> String {
    let mut line: String = cells
        .iter()
        .filter(|cell| !cell.wide_tail)
        .map(shown)
        .collect();
    let len = line.trim_end_matches(' ').len();
    line.truncate(len);
    line
}

/// The columns of the word under the cell (`row`, `column`), or `None` when that cell is a
/// separator. A word runs to the nearest separator, so `src/main.rs:12` comes out whole, without
/// the full stop that closes a sentence after it.
fn word(surface: &Surface, region: &Region, row: u16, column: u16) -> Option<Range<u16>> {
    if !region.contains(column, row) {
        return None;
    }
    let start = region.columns.start;
    let cells = cells_of(surface, row, region.columns.clone())?;
    let hit = usize::from(column - start);
    // A wide glyph's second cell reads as the glyph that drew it.
    let glyph = |index: usize| {
        (0..=index)
            .rev()
            .find(|&i| !cells[i].wide_tail)
            .unwrap_or(index)
    };
    let first_char = |index: usize| shown(&cells[glyph(index)]).chars().next().unwrap_or(' ');
    if separates(first_char(hit)) {
        return None;
    }
    let mut span = run(start, hit, cells.len(), |index| {
        !separates(first_char(index))
    });
    // Punctuation that ends a sentence is not part of the word before it, unless it was clicked.
    while span.end - 1 > column
        && matches!(
            first_char(usize::from(span.end - 1 - start)),
            '.' | ':' | '!' | '?'
        )
    {
        span.end -= 1;
    }
    Some(span)
}

/// The cells one pointer chose in one pane. The pane is looked up in the surface on every use, so
/// a pane that closed or shrank neither paints nor copies stale cells.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Selection {
    pane_id: String,
    unit: Unit,
    /// The unit the press chose. A drag never gives it up.
    origin: (Edge, Edge),
    anchor: Edge,
    head: Edge,
    dragging: bool,
    /// The hash of each selected row's text when the drag ended, sorted by row. See
    /// `remember_text`.
    seen: Vec<(u16, u64)>,
}

impl Selection {
    /// Starts a drag at the pointer, in the pane under it. `x` and `y` are in pixels from the
    /// top-left corner of cell (0, 0). `clicks` is the press's click count: a double click starts on
    /// the word under the pointer, and a triple click on its row. `None` when no pane holds the
    /// pointer, so a click on a border or in the padding selects nothing.
    pub fn begin(surface: &Surface, m: Metrics, x: f32, y: f32, clicks: usize) -> Option<Self> {
        let pane = pane_at(surface, m, x, y)?;
        let region = Region::of(pane.inner_rect)?;
        let edge = region.edge(x, y, m)?;
        let unit = Unit::from(clicks);
        let origin = unit.span(surface, &region, edge);
        Some(Self {
            pane_id: pane.pane_id.clone(),
            unit,
            origin,
            anchor: origin.0,
            head: origin.1,
            dragging: true,
            seen: Vec::new(),
        })
    }

    /// The pane the selection lies in.
    pub fn pane_id(&self) -> &str {
        &self.pane_id
    }

    /// True until the drag ends.
    pub fn dragging(&self) -> bool {
        self.dragging
    }

    /// Follows the pointer, clamped to the pane. Does nothing once the drag has ended. Returns
    /// `true` when the selected cells changed.
    pub fn extend(&mut self, surface: &Surface, m: Metrics, x: f32, y: f32) -> bool {
        if !self.dragging {
            return false;
        }
        let Some(region) = self.region(surface) else {
            return false;
        };
        let Some(edge) = region.edge(x, y, m) else {
            return false;
        };
        let (start, end) = self.unit.span(surface, &region, edge);
        // A drag back past the unit the press chose keeps that unit's far end, so the selection
        // never loses what the press took.
        let ends = if start < self.origin.0 {
            (self.origin.1, start)
        } else {
            (self.origin.0, end.max(self.origin.1))
        };
        let changed = (self.anchor, self.head) != ends;
        (self.anchor, self.head) = ends;
        changed
    }

    /// Ends the drag. Returns `true` when this call ended it, and `false` when it had already
    /// ended.
    pub fn release(&mut self) -> bool {
        std::mem::take(&mut self.dragging)
    }

    /// The selected columns of each row the pane shows, as surface rows. Empty for a click that did
    /// not leave its half-cell, and for a pane that is no longer on screen.
    pub fn rows(&self, surface: &Surface) -> Vec<(u16, Range<u16>)> {
        self.region(surface)
            .map(|region| self.spans(&region))
            .unwrap_or_default()
    }

    /// The selected text, one line per row, with the trailing spaces removed from each line. A wide
    /// glyph is written once. A concealed cell is written as a space. `Stale` when the pane or one
    /// of its rows is gone, and `TooLarge` past `MAX_SELECTION_BYTES`.
    pub fn text(&self, surface: &Surface) -> Result<String, SelectionError> {
        let region = self.region(surface).ok_or(SelectionError::Stale)?;
        let mut text = String::new();
        for (index, (row, columns)) in self.spans(&region).into_iter().enumerate() {
            let cells = cells_of(surface, row, columns).ok_or(SelectionError::Stale)?;
            let line = line_text(cells);
            let separator = usize::from(index > 0);
            if text.len() + separator + line.len() > MAX_SELECTION_BYTES {
                return Err(SelectionError::TooLarge);
            }
            if index > 0 {
                text.push('\n');
            }
            text.push_str(&line);
        }
        Ok(text)
    }

    /// Remembers the text of the selected rows, so `text_unchanged` can tell later whether the
    /// program has written over them.
    pub fn remember_text(&mut self, surface: &Surface) {
        self.seen = self.row_hashes(surface);
    }

    /// False once a remembered row shows other text: the program wrote over the selected rows, and
    /// the highlight would mark cells the user did not choose. A row that was not remembered, or
    /// that is no longer on screen, is no evidence either way.
    pub fn text_unchanged(&self, surface: &Surface) -> bool {
        self.row_hashes(surface).iter().all(|&(row, hash)| {
            self.seen
                .binary_search_by_key(&row, |&(seen, _)| seen)
                .ok()
                .is_none_or(|index| self.seen[index].1 == hash)
        })
    }

    /// A hash of the text in each selected row, by row. Text only: a recolour, or the cursor passing,
    /// leaves the choice valid.
    fn row_hashes(&self, surface: &Surface) -> Vec<(u16, u64)> {
        let Some(region) = self.region(surface) else {
            return Vec::new();
        };
        self.spans(&region)
            .into_iter()
            .filter_map(|(row, columns)| {
                let line = line_text(cells_of(surface, row, columns)?);
                let mut hasher = DefaultHasher::new();
                line.hash(&mut hasher);
                Some((row, hasher.finish()))
            })
            .collect()
    }

    /// The pane's region, or `None` once the surface no longer shows the pane.
    fn region(&self, surface: &Surface) -> Option<Region> {
        let pane = surface
            .panes
            .iter()
            .find(|pane| pane.pane_id == self.pane_id)?;
        Region::of(pane.inner_rect)
    }

    /// The first and last edge, in reading order.
    fn ends(&self) -> (Edge, Edge) {
        if self.anchor <= self.head {
            (self.anchor, self.head)
        } else {
            (self.head, self.anchor)
        }
    }

    /// The selected columns of each row in `region`. The two ends cut their own rows at the
    /// pointer. The rows between them run the full width of the region.
    fn spans(&self, region: &Region) -> Vec<(u16, Range<u16>)> {
        let (start, end) = self.ends();
        let bound = |edge: Edge| match edge.side {
            Side::Left => edge.column,
            Side::Right => edge.column.saturating_add(1),
        };
        let columns = region.columns.clone();
        let first_row = start.row.max(region.rows.start);
        let last_row = end.row.min(region.last_row());
        (first_row..=last_row)
            .filter_map(|row| {
                let first = if row == start.row {
                    bound(start).max(columns.start)
                } else {
                    columns.start
                };
                let stop = if row == end.row {
                    bound(end).min(columns.end)
                } else {
                    columns.end
                };
                (first < stop).then_some((row, first..stop))
            })
            .collect()
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::sync::Arc;

    use contr_herdr::endpoint::messages::GridSize;
    use contr_herdr::endpoint::surface::{Row, SurfacePane};

    use super::*;

    /// Cells of 10 by 20 pixels, with no padding: cell (c, r) spans x from 10c to 10c + 10.
    const M: Metrics = Metrics {
        cell_w: 10.,
        cell_h: 20.,
        pad: 0.,
    };

    fn cell(text: &str) -> Cell {
        Cell {
            text: text.to_owned(),
            fg: 0,
            bg: 0,
            attrs: 0,
            wide_tail: false,
        }
    }

    /// One cell per character.
    fn cells(text: &str) -> Vec<Cell> {
        text.chars().map(|c| cell(&c.to_string())).collect()
    }

    /// A wide glyph: the cell that draws it, then the cell it covers.
    fn wide(glyph: &str) -> Vec<Cell> {
        vec![
            cell(glyph),
            Cell {
                wide_tail: true,
                ..cell("")
            },
        ]
    }

    /// A row of cells of the given byte lengths, each one text.
    fn row_of(lengths: &[usize]) -> Vec<Cell> {
        lengths.iter().map(|&len| cell(&"x".repeat(len))).collect()
    }

    fn pane(id: &str, x: u16, y: u16, width: u16, height: u16) -> SurfacePane {
        let inner_rect = Rect {
            x,
            y,
            width,
            height,
        };
        SurfacePane {
            pane_id: id.to_owned(),
            rect: inner_rect,
            inner_rect,
            focused: true,
            alternate_screen: false,
        }
    }

    /// A surface of `rows`, each padded with blanks to the widest row.
    fn screen(rows: Vec<Vec<Cell>>, panes: Vec<SurfacePane>) -> Surface {
        let cols = rows.iter().map(Vec::len).max().unwrap_or(0);
        let rows: Vec<Row> = rows
            .into_iter()
            .map(|mut row| {
                row.resize(cols, cell(" "));
                Arc::from(row)
            })
            .collect();
        Surface {
            boot_id: "boot".to_owned(),
            projection_revision: 1,
            revision: 1,
            size: GridSize {
                cols: cols as u16,
                rows: rows.len() as u16,
            },
            rows,
            cursor: None,
            panes,
        }
    }

    /// The pointer a fraction of the way across cell (`column`, `row`), at mid height.
    fn at(column: u16, row: u16, fraction: f32) -> (f32, f32) {
        (
            f32::from(column) * M.cell_w + fraction * M.cell_w,
            f32::from(row) * M.cell_h + M.cell_h / 2.,
        )
    }

    /// The left half of a cell.
    fn left(column: u16, row: u16) -> (f32, f32) {
        at(column, row, 0.2)
    }

    /// The right half of a cell.
    fn right(column: u16, row: u16) -> (f32, f32) {
        at(column, row, 0.8)
    }

    /// Presses at `from`, drags to `to`, and releases.
    fn drag(surface: &Surface, from: (f32, f32), to: (f32, f32)) -> Selection {
        let mut selection =
            Selection::begin(surface, M, from.0, from.1, 1).expect("a press on a pane");
        selection.extend(surface, M, to.0, to.1);
        selection.release();
        selection
    }

    /// Presses `clicks` times at `at`, with no drag.
    fn press(surface: &Surface, at: (f32, f32), clicks: usize) -> Selection {
        Selection::begin(surface, M, at.0, at.1, clicks).expect("a press on a pane")
    }

    fn copied(selection: &Selection, surface: &Surface) -> String {
        selection.text(surface).expect("a copy")
    }

    fn region(x: u16, y: u16, width: u16, height: u16) -> Region {
        Region::of(Rect {
            x,
            y,
            width,
            height,
        })
        .expect("a region with cells")
    }

    #[test]
    fn an_edge_is_the_half_of_the_cell_under_the_point() {
        let region = region(2, 1, 5, 2);
        assert_eq!(
            region.edge(left(2, 1).0, left(2, 1).1, M),
            Some(Edge {
                row: 1,
                column: 2,
                side: Side::Left
            })
        );
        assert_eq!(
            region.edge(right(2, 1).0, right(2, 1).1, M),
            Some(Edge {
                row: 1,
                column: 2,
                side: Side::Right
            })
        );
        assert_eq!(
            region.edge(right(6, 2).0, right(6, 2).1, M),
            Some(Edge {
                row: 2,
                column: 6,
                side: Side::Right
            })
        );
    }

    #[test]
    fn an_edge_outside_the_region_is_clamped_to_its_boundary() {
        let region = region(2, 1, 5, 2);
        // Above the region: its first row, at its first column.
        assert_eq!(
            region.edge(25., 0., M),
            Some(Edge {
                row: 1,
                column: 2,
                side: Side::Left
            })
        );
        // Below the region: its last row, at its last column.
        assert_eq!(
            region.edge(25., 100., M),
            Some(Edge {
                row: 2,
                column: 6,
                side: Side::Right
            })
        );
        // Left of the region: its first column, on the row of the point.
        assert_eq!(
            region.edge(-5., 30., M),
            Some(Edge {
                row: 1,
                column: 2,
                side: Side::Left
            })
        );
        // Right of the region: its last column, on the row of the point.
        assert_eq!(
            region.edge(500., 30., M),
            Some(Edge {
                row: 1,
                column: 6,
                side: Side::Right
            })
        );
    }

    #[test]
    fn an_edge_needs_a_finite_point_and_a_cell_with_a_size() {
        let region = region(0, 0, 5, 1);
        assert_eq!(region.edge(f32::NAN, 5., M), None);
        assert_eq!(region.edge(5., f32::INFINITY, M), None);
        assert_eq!(region.edge(5., 5., Metrics { cell_w: 0., ..M }), None);
        assert_eq!(
            region.edge(
                5.,
                5.,
                Metrics {
                    cell_h: f32::NAN,
                    ..M
                }
            ),
            None
        );
    }

    #[test]
    fn a_press_selects_in_the_pane_under_the_pointer_only() {
        let surface = screen(
            vec![cells("abcdefghijkl")],
            vec![pane("left", 0, 0, 5, 1), pane("right", 7, 0, 5, 1)],
        );
        assert_eq!(press(&surface, left(1, 0), 1).pane_id(), "left");
        assert_eq!(press(&surface, right(8, 0), 1).pane_id(), "right");
        // Columns 5 and 6 are the gap between the panes.
        assert!(Selection::begin(&surface, M, at(5, 0, 0.5).0, at(5, 0, 0.5).1, 1).is_none());
        assert!(Selection::begin(&surface, M, at(6, 0, 0.5).0, at(6, 0, 0.5).1, 1).is_none());
        // Negative positions lie in the padding.
        assert!(Selection::begin(&surface, M, -1., 5., 1).is_none());
    }

    #[test]
    fn a_pane_with_no_cells_takes_no_press() {
        let surface = screen(vec![cells("abc")], vec![pane("empty", 0, 0, 0, 1)]);
        assert!(Selection::begin(&surface, M, 5., 5., 1).is_none());
    }

    #[test]
    fn a_click_without_a_drag_selects_no_cell() {
        let surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = press(&surface, right(3, 0), 1);
        assert!(selection.dragging());
        assert!(selection.rows(&surface).is_empty());
        assert_eq!(copied(&selection, &surface), "");
        assert!(selection.release());
        assert!(!selection.dragging());
        assert!(!selection.release());
    }

    #[test]
    fn a_drag_selects_from_the_first_cell_to_the_one_under_the_pointer() {
        let surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let selection = drag(&surface, left(1, 0), right(3, 0));
        assert_eq!(selection.rows(&surface), vec![(0, 1..4)]);
        assert_eq!(copied(&selection, &surface), "bcd");
    }

    #[test]
    fn a_drag_back_to_the_left_selects_the_same_cells() {
        let surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let selection = drag(&surface, right(3, 0), left(1, 0));
        assert_eq!(copied(&selection, &surface), "bcd");
    }

    #[test]
    fn a_drag_over_several_rows_cuts_the_first_and_last_rows_and_keeps_the_middle_full() {
        let surface = screen(
            vec![cells("abcd"), cells("efgh"), cells("ijkl")],
            vec![pane("p", 0, 0, 4, 3)],
        );
        let selection = drag(&surface, left(2, 0), right(1, 2));
        assert_eq!(
            selection.rows(&surface),
            vec![(0, 2..4), (1, 0..4), (2, 0..2)]
        );
        assert_eq!(copied(&selection, &surface), "cd\nefgh\nij");
    }

    #[test]
    fn each_line_loses_its_trailing_spaces_and_a_blank_row_stays_a_line() {
        let surface = screen(
            vec![cells("ab  "), cells("    "), cells("cd  ")],
            vec![pane("p", 0, 0, 4, 3)],
        );
        let selection = drag(&surface, left(0, 0), right(3, 2));
        assert_eq!(copied(&selection, &surface), "ab\n\ncd");
    }

    #[test]
    fn a_wide_glyph_is_written_once_from_its_first_cell() {
        let row = [vec![cell("a")], wide("漢"), cells("b")].concat();
        let surface = screen(vec![row], vec![pane("p", 0, 0, 4, 1)]);
        let selection = drag(&surface, left(0, 0), right(3, 0));
        assert_eq!(copied(&selection, &surface), "a漢b");
        let first_cell = drag(&surface, left(0, 0), right(1, 0));
        assert_eq!(copied(&first_cell, &surface), "a漢");
    }

    #[test]
    fn a_wide_glyph_whose_first_cell_is_before_the_selection_is_left_out() {
        let row = [vec![cell("a")], wide("漢"), cells("b")].concat();
        let surface = screen(vec![row], vec![pane("p", 0, 0, 4, 1)]);
        // The drag starts on the glyph's second cell, so the glyph is not in the selection.
        let selection = drag(&surface, left(2, 0), right(3, 0));
        assert_eq!(copied(&selection, &surface), "b");
    }

    #[test]
    fn a_concealed_cell_is_copied_as_a_blank() {
        let row = [
            cells("ab"),
            vec![
                Cell {
                    attrs: HIDDEN,
                    ..cell("c")
                },
                Cell {
                    attrs: HIDDEN,
                    ..cell("d")
                },
            ],
            cells("ef"),
        ]
        .concat();
        let surface = screen(vec![row], vec![pane("p", 0, 0, 6, 1)]);
        let selection = drag(&surface, left(0, 0), right(5, 0));
        assert_eq!(copied(&selection, &surface), "ab  ef");
    }

    #[test]
    fn an_empty_cell_is_copied_as_a_blank() {
        let row = vec![cell("a"), cell(""), cell("b")];
        let surface = screen(vec![row], vec![pane("p", 0, 0, 3, 1)]);
        let selection = drag(&surface, left(0, 0), right(2, 0));
        assert_eq!(copied(&selection, &surface), "a b");
    }

    #[test]
    fn a_double_click_selects_the_word_under_the_pointer() {
        // The word `src/main.rs:12` has no separator in it.
        let surface = screen(
            vec![cells("ls -la src/main.rs:12 ok")],
            vec![pane("p", 0, 0, 24, 1)],
        );
        let selection = press(&surface, right(12, 0), 2);
        assert_eq!(selection.rows(&surface), vec![(0, 7..21)]);
        assert_eq!(copied(&selection, &surface), "src/main.rs:12");
    }

    #[test]
    fn a_word_ends_before_a_full_stop_that_closes_a_sentence() {
        let surface = screen(vec![cells("see foo.bar. ok")], vec![pane("p", 0, 0, 15, 1)]);
        assert_eq!(
            copied(&press(&surface, right(5, 0), 2), &surface),
            "foo.bar"
        );
        // A click on the full stop keeps it.
        assert_eq!(
            copied(&press(&surface, right(11, 0), 2), &surface),
            "foo.bar."
        );
    }

    #[test]
    fn a_word_stops_at_quotes_and_brackets() {
        let surface = screen(vec![cells("call(\"x\")")], vec![pane("p", 0, 0, 9, 1)]);
        assert_eq!(copied(&press(&surface, right(6, 0), 2), &surface), "x");
    }

    #[test]
    fn a_double_click_on_a_separator_selects_that_one_cell() {
        let surface = screen(vec![cells("ab cd")], vec![pane("p", 0, 0, 5, 1)]);
        let selection = press(&surface, right(2, 0), 2);
        assert_eq!(selection.rows(&surface), vec![(0, 2..3)]);
    }

    #[test]
    fn a_wide_glyph_belongs_to_the_word_it_is_in() {
        let row = [wide("漢"), wide("字"), cells(" ok")].concat();
        let surface = screen(vec![row], vec![pane("p", 0, 0, 7, 1)]);
        // Column 3 is the second cell of 字.
        let selection = press(&surface, right(3, 0), 2);
        assert_eq!(copied(&selection, &surface), "漢字");
    }

    #[test]
    fn a_triple_click_selects_the_whole_row_and_a_drag_takes_whole_rows() {
        let surface = screen(
            vec![cells("ab  cd"), cells("xyz")],
            vec![pane("p", 0, 0, 6, 2)],
        );
        let mut selection = press(&surface, right(1, 0), 3);
        assert_eq!(selection.rows(&surface), vec![(0, 0..6)]);
        assert_eq!(copied(&selection, &surface), "ab  cd");
        selection.extend(&surface, M, right(2, 1).0, right(2, 1).1);
        selection.release();
        assert_eq!(copied(&selection, &surface), "ab  cd\nxyz");
    }

    #[test]
    fn a_word_drag_takes_whole_words_forwards() {
        let surface = screen(vec![cells("one two three")], vec![pane("p", 0, 0, 13, 1)]);
        let mut selection = press(&surface, right(0, 0), 2);
        selection.extend(&surface, M, right(9, 0).0, right(9, 0).1);
        assert_eq!(copied(&selection, &surface), "one two three");
    }

    #[test]
    fn a_word_drag_back_keeps_the_far_end_of_the_word_the_press_took() {
        let surface = screen(vec![cells("one two three")], vec![pane("p", 0, 0, 13, 1)]);
        let mut selection = press(&surface, right(5, 0), 2);
        selection.extend(&surface, M, left(0, 0).0, left(0, 0).1);
        selection.release();
        assert_eq!(copied(&selection, &surface), "one two");
    }

    #[test]
    fn extend_reports_only_a_change_in_the_selected_cells() {
        let surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = press(&surface, left(1, 0), 1);
        assert!(selection.extend(&surface, M, right(3, 0).0, right(3, 0).1));
        assert!(!selection.extend(&surface, M, right(3, 0).0, right(3, 0).1));
        assert!(selection.extend(&surface, M, right(4, 0).0, right(4, 0).1));
    }

    #[test]
    fn a_drag_after_release_does_not_move_the_selection() {
        let surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = press(&surface, left(1, 0), 1);
        selection.extend(&surface, M, right(3, 0).0, right(3, 0).1);
        selection.release();
        assert!(!selection.extend(&surface, M, right(5, 0).0, right(5, 0).1));
        assert_eq!(selection.rows(&surface), vec![(0, 1..4)]);
    }

    #[test]
    fn a_drag_stays_inside_the_pane_where_it_started() {
        let surface = screen(
            vec![cells("abcdXefgh")],
            vec![pane("a", 0, 0, 4, 1), pane("b", 5, 0, 4, 1)],
        );
        let mut selection = press(&surface, left(1, 0), 1);
        selection.extend(&surface, M, right(7, 0).0, right(7, 0).1);
        selection.release();
        assert_eq!(selection.pane_id(), "a");
        assert_eq!(copied(&selection, &surface), "bcd");
    }

    #[test]
    fn a_selection_gives_no_text_once_its_pane_is_gone() {
        let mut surface = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let selection = drag(&surface, left(1, 0), right(3, 0));
        surface.panes.clear();
        assert!(selection.rows(&surface).is_empty());
        assert_eq!(selection.text(&surface), Err(SelectionError::Stale));
    }

    #[test]
    fn a_selection_gives_no_text_when_the_surface_lacks_one_of_its_rows() {
        let surface = screen(vec![cells("abc")], vec![pane("p", 0, 0, 3, 3)]);
        let selection = drag(&surface, left(0, 0), right(0, 2));
        assert_eq!(selection.text(&surface), Err(SelectionError::Stale));
    }

    #[test]
    fn a_copy_of_four_mebibytes_is_taken_and_one_byte_more_is_refused() {
        let mib = 1 << 20;
        let at_limit = screen(
            vec![row_of(&[mib, mib, mib, mib])],
            vec![pane("p", 0, 0, 4, 1)],
        );
        let taken = drag(&at_limit, left(0, 0), right(3, 0));
        assert_eq!(copied(&taken, &at_limit).len(), MAX_SELECTION_BYTES);

        let over = screen(
            vec![row_of(&[mib, mib, mib, mib + 1])],
            vec![pane("p", 0, 0, 4, 1)],
        );
        let refused = drag(&over, left(0, 0), right(3, 0));
        assert_eq!(refused.text(&over), Err(SelectionError::TooLarge));
    }

    #[test]
    fn the_line_break_between_rows_counts_toward_the_limit() {
        let mib = 1 << 20;
        let fits = screen(
            vec![row_of(&[mib, mib]), row_of(&[mib, mib - 1])],
            vec![pane("p", 0, 0, 2, 2)],
        );
        let taken = drag(&fits, left(0, 0), right(1, 1));
        assert_eq!(copied(&taken, &fits).len(), MAX_SELECTION_BYTES);

        let over = screen(
            vec![row_of(&[mib, mib]), row_of(&[mib, mib])],
            vec![pane("p", 0, 0, 2, 2)],
        );
        let refused = drag(&over, left(0, 0), right(1, 1));
        assert_eq!(refused.text(&over), Err(SelectionError::TooLarge));
    }

    #[test]
    fn text_unchanged_holds_while_the_selected_text_reads_the_same() {
        let before = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = drag(&before, left(1, 0), right(3, 0));
        selection.remember_text(&before);

        let mut recoloured = cells("abcdef");
        recoloured.iter_mut().for_each(|cell| cell.fg = 1);
        let same_text = screen(vec![recoloured], vec![pane("p", 0, 0, 6, 1)]);
        assert!(selection.text_unchanged(&same_text));
    }

    #[test]
    fn text_unchanged_fails_once_a_selected_row_is_written_over() {
        let before = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = drag(&before, left(1, 0), right(3, 0));
        selection.remember_text(&before);

        let written = screen(vec![cells("axcdef")], vec![pane("p", 0, 0, 6, 1)]);
        assert!(!selection.text_unchanged(&written));
    }

    #[test]
    fn text_unchanged_ignores_a_change_outside_the_selected_cells() {
        let before = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let mut selection = drag(&before, left(1, 0), right(3, 0));
        selection.remember_text(&before);

        let written = screen(vec![cells("abcdez")], vec![pane("p", 0, 0, 6, 1)]);
        assert!(selection.text_unchanged(&written));
    }

    #[test]
    fn text_unchanged_is_true_when_nothing_was_remembered() {
        let before = screen(vec![cells("abcdef")], vec![pane("p", 0, 0, 6, 1)]);
        let selection = drag(&before, left(1, 0), right(3, 0));

        let written = screen(vec![cells("xxxxxx")], vec![pane("p", 0, 0, 6, 1)]);
        assert!(selection.text_unchanged(&written));
    }

    #[test]
    fn a_unit_follows_the_click_count() {
        assert_eq!(Unit::from(0), Unit::Cell);
        assert_eq!(Unit::from(1), Unit::Cell);
        assert_eq!(Unit::from(2), Unit::Word);
        assert_eq!(Unit::from(3), Unit::Line);
        assert_eq!(Unit::from(4), Unit::Line);
    }
}
