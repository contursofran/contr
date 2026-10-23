//! Pane surfaces of herdr's endpoint socket: the full surface of the focused
//! tab (`PaneSurface`, tag 13) and the row spans that change it
//! (`PaneSurfacePatch`, tag 19). Layouts follow herdr's wire types; see NOTICE.

use std::sync::Arc;

use super::codec::{CodecError, Dec};
use super::messages::GridSize;

/// One cell. `fg` and `bg` are herdr's packed colours: the top byte is the
/// kind (0 named, 1 indexed, 2 RGB) and the low bits are the value.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cell {
    pub text: String,
    pub fg: u32,
    pub bg: u32,
    /// Style bits: herdr's modifier mask (BOLD 1 … STRIKETHROUGH 256) and the underline style.
    pub attrs: u16,
    /// The second column of a wide glyph. The glyph is drawn by the cell before it.
    pub wide_tail: bool,
}

/// Cursor position and shape inside the surface.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Cursor {
    pub x: u16,
    pub y: u16,
    pub visible: bool,
    /// DECSCUSR parameter: 0–2 block, 3–4 underline, 5–6 bar.
    pub shape: u8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Rect {
    pub x: u16,
    pub y: u16,
    pub width: u16,
    pub height: u16,
}

/// One pane of the shown tab, in surface coordinates.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SurfacePane {
    pub pane_id: String,
    pub rect: Rect,
    pub inner_rect: Rect,
    pub focused: bool,
    pub alternate_screen: bool,
}

/// One row of cells. Rows are shared, so a patch that changes one row keeps the others.
pub type Row = Arc<[Cell]>;

/// The rendered surface of the focused tab: what the terminal view paints.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Surface {
    pub boot_id: String,
    pub projection_revision: u64,
    pub revision: u64,
    pub size: GridSize,
    pub rows: Vec<Row>,
    pub cursor: Option<Cursor>,
    pub panes: Vec<SurfacePane>,
}

impl Surface {
    pub fn focused_pane(&self) -> Option<&SurfacePane> {
        self.panes.iter().find(|pane| pane.focused)
    }

    /// Applies `p` when it continues this surface. On an error nothing changes.
    pub fn apply_patch(&mut self, p: SurfacePatch) -> Result<(), PatchError> {
        if p.boot_id != self.boot_id
            || p.projection_revision != self.projection_revision
            || p.base_revision != self.revision
        {
            return Err(PatchError::Stale);
        }
        // Check every span before the first write, so a bad patch changes nothing.
        let in_range = p.spans.iter().all(|(x, y, cells)| {
            self.rows
                .get(usize::from(*y))
                .is_some_and(|row| usize::from(*x) + cells.len() <= row.len())
        });
        if !in_range {
            return Err(PatchError::OutOfRange);
        }
        for (x, y, cells) in p.spans {
            if let Some(slot) = self.rows.get_mut(usize::from(y)) {
                let start = usize::from(x);
                let mut row = slot.to_vec();
                row.splice(start..start + cells.len(), cells);
                *slot = Arc::from(row);
            }
        }
        for pane in p.panes {
            match self
                .panes
                .iter()
                .position(|known| known.pane_id == pane.pane_id)
            {
                Some(i) => self.panes[i] = pane,
                None => self.panes.push(pane),
            }
        }
        self.cursor = p.cursor;
        self.revision = p.revision;
        Ok(())
    }
}

/// A decoded `PaneSurface` frame.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SurfaceFrame(pub Surface);

/// A decoded `PaneSurfacePatch`: the changes from `base_revision` to `revision`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SurfacePatch {
    pub boot_id: String,
    pub projection_revision: u64,
    pub base_revision: u64,
    pub revision: u64,
    /// `(x, y, cells)`: the cells start at column `x` of row `y`.
    pub spans: Vec<(u16, u16, Vec<Cell>)>,
    pub panes: Vec<SurfacePane>,
    pub cursor: Option<Cursor>,
}

#[derive(Debug, PartialEq, Eq)]
pub enum PatchError {
    /// The patch belongs to another boot, projection or base revision: the
    /// link has lost sync and the caller must reconnect.
    Stale,
    /// A span does not fit in the surface.
    OutOfRange,
}

/// Reads the body of a `PaneSurface` (tag 13, already read). The fields after
/// `panes` (splits, popup, graphics) are not read: the frame length bounds
/// the payload and contr does not use them yet.
pub(super) fn decode_frame(d: &mut Dec<'_>) -> Result<SurfaceFrame, CodecError> {
    let boot_id = d.str()?;
    let projection_revision = d.vu()?;
    let revision = d.vu()?;
    let body = frame(d)?;
    if body.cells.len() != usize::from(body.width) * usize::from(body.height) {
        return Err(CodecError::Invalid("frame size"));
    }
    let panes = pane_list(d)?;
    Ok(SurfaceFrame(Surface {
        boot_id,
        projection_revision,
        revision,
        size: GridSize {
            cols: body.width,
            rows: body.height,
        },
        rows: into_rows(body.cells, body.width, body.height),
        cursor: body.cursor,
        panes,
    }))
}

/// Reads the body of a `PaneSurfacePatch` (tag 19, already read).
pub(super) fn decode_patch(d: &mut Dec<'_>) -> Result<SurfacePatch, CodecError> {
    let boot_id = d.str()?;
    let projection_revision = d.vu()?;
    let base_revision = d.vu()?;
    let revision = d.vu()?;
    let mut spans = Vec::new();
    for _ in 0..d.len()? {
        let x = d.vu16()?;
        let y = d.vu16()?;
        spans.push((x, y, cells(d)?));
    }
    let panes = pane_list(d)?;
    let cursor_state = cursor(d)?;
    Ok(SurfacePatch {
        boot_id,
        projection_revision,
        base_revision,
        revision,
        spans,
        panes,
        cursor: cursor_state,
    })
}

/// A `FrameData` without its hyperlinks and graphics, which are read past.
struct Frame {
    cells: Vec<Cell>,
    width: u16,
    height: u16,
    cursor: Option<Cursor>,
}

fn frame(d: &mut Dec<'_>) -> Result<Frame, CodecError> {
    let cells = cells(d)?;
    let width = d.vu16()?;
    let height = d.vu16()?;
    let cursor_state = cursor(d)?;
    for _ in 0..d.len()? {
        d.skip_bytes()?; // hyperlink URI
    }
    d.skip_bytes()?; // graphics bytes
    Ok(Frame {
        cells,
        width,
        height,
        cursor: cursor_state,
    })
}

fn cells(d: &mut Dec<'_>) -> Result<Vec<Cell>, CodecError> {
    let n = d.len()?;
    let mut out = Vec::new();
    for _ in 0..n {
        out.push(cell(d)?);
    }
    Ok(out)
}

fn cell(d: &mut Dec<'_>) -> Result<Cell, CodecError> {
    let text = d.str()?;
    let fg = d.vu32()?;
    let bg = d.vu32()?;
    let attrs = d.vu16()?;
    let wide_tail = d.bool()?;
    if d.option()? {
        d.vu32()?; // hyperlink index: the hyperlink list is not kept
    }
    Ok(Cell {
        text,
        fg,
        bg,
        attrs,
        wide_tail,
    })
}

fn cursor(d: &mut Dec<'_>) -> Result<Option<Cursor>, CodecError> {
    if !d.option()? {
        return Ok(None);
    }
    let x = d.vu16()?;
    let y = d.vu16()?;
    let visible = d.bool()?;
    let shape = d.u8()?;
    Ok(Some(Cursor {
        x,
        y,
        visible,
        shape,
    }))
}

fn rect(d: &mut Dec<'_>) -> Result<Rect, CodecError> {
    let x = d.vu16()?;
    let y = d.vu16()?;
    let width = d.vu16()?;
    let height = d.vu16()?;
    Ok(Rect {
        x,
        y,
        width,
        height,
    })
}

/// A `PaneSurfacePane`. Its scroll, mouse and pixel fields are read past.
fn pane(d: &mut Dec<'_>) -> Result<SurfacePane, CodecError> {
    let pane_id = d.str()?;
    d.vu()?; // content_revision
    let outer = rect(d)?;
    let inner = rect(d)?;
    if d.option()? {
        rect(d)?; // scrollbar_rect
    }
    if d.option()? {
        // scroll: offset_from_bottom, max_offset_from_bottom, viewport_rows
        d.vu()?;
        d.vu()?;
        d.vu()?;
    }
    let focused = d.bool()?;
    d.bool()?; // mouse_reporting
    d.bool()?; // sgr_pixel_mouse
    let alternate_screen = d.bool()?;
    d.vu32()?; // pixel_width
    d.vu32()?; // pixel_height
    Ok(SurfacePane {
        pane_id,
        rect: outer,
        inner_rect: inner,
        focused,
        alternate_screen,
    })
}

fn pane_list(d: &mut Dec<'_>) -> Result<Vec<SurfacePane>, CodecError> {
    let n = d.len()?;
    let mut out = Vec::new();
    for _ in 0..n {
        out.push(pane(d)?);
    }
    Ok(out)
}

/// Cuts the row-major cells into `height` rows of `width` cells. The caller has
/// checked that the count is `width * height`.
fn into_rows(cells: Vec<Cell>, width: u16, height: u16) -> Vec<Row> {
    let cols = usize::from(width);
    let mut rest = cells.into_iter();
    (0..height)
        .map(|_| rest.by_ref().take(cols).collect::<Vec<Cell>>().into())
        .collect()
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::endpoint::codec::Enc;
    use crate::endpoint::messages::{S_PATCH, S_SURFACE, ServerMsg, decode_server};

    /// A cell with no colour, no style and no wide tail.
    fn plain(text: &str) -> Cell {
        Cell {
            text: text.into(),
            fg: 0,
            bg: 0,
            attrs: 0,
            wide_tail: false,
        }
    }

    /// The second column of a wide glyph.
    fn tail() -> Cell {
        Cell {
            text: String::new(),
            fg: 0,
            bg: 0,
            attrs: 0,
            wide_tail: true,
        }
    }

    fn pane(id: &str, focused: bool) -> SurfacePane {
        SurfacePane {
            pane_id: id.into(),
            rect: Rect {
                x: 0,
                y: 0,
                width: 3,
                height: 1,
            },
            inner_rect: Rect {
                x: 0,
                y: 0,
                width: 2,
                height: 1,
            },
            focused,
            alternate_screen: false,
        }
    }

    /// A `cols` × `rows` surface of blank cells, boot "b1", projection 7, revision 9.
    fn blank(cols: u16, rows: u16) -> Surface {
        Surface {
            boot_id: "b1".into(),
            projection_revision: 7,
            revision: 9,
            size: GridSize { cols, rows },
            rows: (0..rows)
                .map(|_| Arc::from(vec![plain(" "); usize::from(cols)]))
                .collect(),
            cursor: None,
            panes: Vec::new(),
        }
    }

    fn patch(base: u64, revision: u64, spans: Vec<(u16, u16, Vec<Cell>)>) -> SurfacePatch {
        SurfacePatch {
            boot_id: "b1".into(),
            projection_revision: 7,
            base_revision: base,
            revision,
            spans,
            panes: Vec::new(),
            cursor: None,
        }
    }

    fn texts(row: &Row) -> Vec<String> {
        row.iter().map(|c| c.text.clone()).collect()
    }

    fn put_cell(e: &mut Enc, c: &Cell) {
        e.str(&c.text);
        e.vu(u64::from(c.fg));
        e.vu(u64::from(c.bg));
        e.vu(u64::from(c.attrs));
        e.bool(c.wide_tail);
        e.none(); // hyperlink
    }

    fn put_rect(e: &mut Enc, r: Rect) {
        e.vu(u64::from(r.x));
        e.vu(u64::from(r.y));
        e.vu(u64::from(r.width));
        e.vu(u64::from(r.height));
    }

    fn put_pane(e: &mut Enc, p: &SurfacePane) {
        e.str(&p.pane_id);
        e.vu(0); // content_revision
        put_rect(e, p.rect);
        put_rect(e, p.inner_rect);
        e.none(); // scrollbar_rect
        e.none(); // scroll
        e.bool(p.focused);
        e.bool(false); // mouse_reporting
        e.bool(false); // sgr_pixel_mouse
        e.bool(p.alternate_screen);
        e.vu(0); // pixel_width
        e.vu(0); // pixel_height
    }

    /// Writes a `PaneSurface` payload the way herdr lays it out: the cells of
    /// all rows in order, one hyperlink, three graphics bytes, then the tail
    /// (splits, popup, graphics scene) that decoding skips.
    fn enc_surface(s: &Surface) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_SURFACE));
        e.str(&s.boot_id);
        e.vu(s.projection_revision);
        e.vu(s.revision);
        let cells: Vec<&Cell> = s.rows.iter().flat_map(|row| row.iter()).collect();
        e.vu(cells.len() as u64);
        for c in cells {
            put_cell(&mut e, c);
        }
        e.vu(u64::from(s.size.cols));
        e.vu(u64::from(s.size.rows));
        match s.cursor {
            Some(c) => {
                e.some();
                e.vu(u64::from(c.x));
                e.vu(u64::from(c.y));
                e.bool(c.visible);
                e.u8(c.shape);
            }
            None => e.none(),
        }
        e.vu(1); // hyperlinks
        e.str("h");
        e.vu(3); // graphics
        e.buf.extend_from_slice(&[1, 2, 3]);
        e.vu(s.panes.len() as u64);
        for p in &s.panes {
            put_pane(&mut e, p);
        }
        // Tail: no splits, no popup, empty graphics scene (assets, placements, retained).
        e.vu(0);
        e.none();
        e.vu(0);
        e.vu(0);
        e.vu(0);
        e.buf
    }

    /// A `PaneSurface` payload, byte by byte, from the layout above. Surface
    /// 3×1, boot "b1", projection 7, revision 9. Cells: "a" (fg indexed 1, bold),
    /// "界" (fg RGB 0x112233, italic, hyperlink 0), wide tail. Cursor (2,0) bar.
    /// Pane "w1:p1", focused, 300 px wide, 40 px high. The last five bytes are the tail.
    #[rustfmt::skip]
    const FRAME_GOLDEN: &[u8] = &[
        13,                                   // tag: PaneSurface
        2, b'b', b'1',                        // boot_id
        7,                                    // projection_revision
        9,                                    // surface_revision
        3,                                    // cells: 3
        1, b'a', 252, 1, 0, 0, 1, 0, 1, 0, 0, // "a", fg 0x01000001, bg 0, attrs 1, skip false, no hyperlink
        3, 0xE7, 0x95, 0x8C, 252, 0x33, 0x22, 0x11, 0x02, 0, 4, 0, 1, 0, // "界", fg 0x02112233, attrs 4, hyperlink 0
        0, 0, 0, 0, 1, 0,                     // "" wide tail, skip true
        3, 1,                                 // width 3, height 1
        1, 2, 0, 1, 5,                        // cursor: some, x 2, y 0, visible, shape 5
        1, 1, b'h',                           // hyperlinks: one URI "h"
        0,                                    // graphics: no bytes
        1,                                    // panes: 1
        5, b'w', b'1', b':', b'p', b'1',      // pane_id "w1:p1"
        4,                                    // content_revision
        0, 0, 3, 1,                           // rect
        0, 0, 2, 1,                           // inner_rect
        0, 0,                                 // scrollbar_rect none, scroll none
        1, 0, 0, 0,                           // focused, mouse, sgr pixel, alternate screen
        251, 44, 1, 40,                       // pixel_width 300, pixel_height 40
        0, 0, 0, 0, 0,                        // tail: splits 0, popup none, graphics 0/0/0
    ];

    /// The same surface as `FRAME_GOLDEN` decodes to.
    fn golden_surface() -> Surface {
        Surface {
            boot_id: "b1".into(),
            projection_revision: 7,
            revision: 9,
            size: GridSize { cols: 3, rows: 1 },
            rows: vec![Arc::from(vec![
                Cell {
                    text: "a".into(),
                    fg: 0x0100_0001,
                    bg: 0,
                    attrs: 1,
                    wide_tail: false,
                },
                Cell {
                    text: "界".into(),
                    fg: 0x0211_2233,
                    bg: 0,
                    attrs: 4,
                    wide_tail: false,
                },
                tail(),
            ])],
            cursor: Some(Cursor {
                x: 2,
                y: 0,
                visible: true,
                shape: 5,
            }),
            panes: vec![SurfacePane {
                pane_id: "w1:p1".into(),
                rect: Rect {
                    x: 0,
                    y: 0,
                    width: 3,
                    height: 1,
                },
                inner_rect: Rect {
                    x: 0,
                    y: 0,
                    width: 2,
                    height: 1,
                },
                focused: true,
                alternate_screen: false,
            }],
        }
    }

    /// A `PaneSurfacePatch` payload, byte by byte: boot "b1", projection 7,
    /// base 9 → revision 10. One span at x 1, y 1: "z" (fg indexed 2, dim) and a
    /// wide tail. One pane "w1:p1" (alternate screen on). Cursor (1,1), hidden.
    #[rustfmt::skip]
    const PATCH_GOLDEN: &[u8] = &[
        19,                                   // tag: PaneSurfacePatch
        2, b'b', b'1',                        // boot_id
        7,                                    // projection_revision
        9,                                    // base_surface_revision
        10,                                   // surface_revision
        1,                                    // spans: 1
        1, 1, 2,                              // x 1, y 1, cells: 2
        1, b'z', 252, 2, 0, 0, 1, 0, 2, 0, 0, // "z", fg 0x01000002, attrs 2, skip false, no hyperlink
        0, 0, 0, 0, 1, 0,                     // "" wide tail
        1,                                    // panes: 1
        5, b'w', b'1', b':', b'p', b'1',      // pane_id "w1:p1"
        4,                                    // content_revision
        0, 0, 3, 1,                           // rect
        0, 0, 2, 1,                           // inner_rect
        0, 0,                                 // scrollbar_rect none, scroll none
        0, 0, 0, 1,                           // focused, mouse, sgr pixel, alternate screen
        251, 44, 1, 40,                       // pixel_width 300, pixel_height 40
        1, 1, 1, 0, 0,                        // cursor: some, x 1, y 1, hidden, shape 0
    ];

    #[test]
    fn frame_golden_bytes_decode_to_the_surface() {
        assert_eq!(
            decode_server(FRAME_GOLDEN).unwrap(),
            ServerMsg::Surface(SurfaceFrame(golden_surface()))
        );
    }

    #[test]
    fn frame_skips_the_tail_after_the_panes() {
        let mut payload = FRAME_GOLDEN.to_vec();
        let n = payload.len();
        payload[n - 5..].copy_from_slice(&[0xFF; 5]); // would be a bad varint if read
        assert_eq!(
            decode_server(&payload).unwrap(),
            ServerMsg::Surface(SurfaceFrame(golden_surface()))
        );
    }

    #[test]
    fn frame_cut_before_its_tail_is_an_error() {
        let tail_start = FRAME_GOLDEN.len() - 5;
        for n in 0..tail_start {
            assert!(
                decode_server(&FRAME_GOLDEN[..n]).is_err(),
                "a frame cut at {n} bytes decoded"
            );
        }
    }

    #[test]
    fn frame_round_trips_through_the_layout() {
        let mut wide = blank(3, 2);
        wide.rows = vec![
            Arc::from(vec![plain("a"), plain("界"), tail()]),
            Arc::from(vec![plain("x"), plain(" "), plain("é")]),
        ];
        wide.cursor = Some(Cursor {
            x: 1,
            y: 1,
            visible: false,
            shape: 0,
        });
        wide.panes = vec![pane("w1:p1", true), pane("w1:p2", false)];
        let empty = Surface {
            size: GridSize { cols: 0, rows: 0 },
            rows: Vec::new(),
            ..blank(1, 1)
        };
        let zero_wide = Surface {
            size: GridSize { cols: 0, rows: 2 },
            rows: vec![Arc::from(Vec::new()), Arc::from(Vec::new())],
            ..blank(1, 1)
        };
        for s in [golden_surface(), wide, empty, zero_wide] {
            let payload = enc_surface(&s);
            assert_eq!(
                decode_server(&payload).unwrap(),
                ServerMsg::Surface(SurfaceFrame(s.clone())),
                "{s:?}"
            );
        }
    }

    #[test]
    fn frame_with_a_wrong_cell_count_is_invalid() {
        let mut s = blank(2, 2);
        s.rows.pop(); // two cells left for a 2×2 surface
        assert!(matches!(
            decode_server(&enc_surface(&s)),
            Err(CodecError::Invalid("frame size"))
        ));
    }

    #[test]
    fn patch_golden_bytes_decode() {
        assert_eq!(
            decode_server(PATCH_GOLDEN).unwrap(),
            ServerMsg::Patch(SurfacePatch {
                boot_id: "b1".into(),
                projection_revision: 7,
                base_revision: 9,
                revision: 10,
                spans: vec![(
                    1,
                    1,
                    vec![
                        Cell {
                            text: "z".into(),
                            fg: 0x0100_0002,
                            bg: 0,
                            attrs: 2,
                            wide_tail: false,
                        },
                        tail(),
                    ]
                )],
                panes: vec![SurfacePane {
                    pane_id: "w1:p1".into(),
                    rect: Rect {
                        x: 0,
                        y: 0,
                        width: 3,
                        height: 1,
                    },
                    inner_rect: Rect {
                        x: 0,
                        y: 0,
                        width: 2,
                        height: 1,
                    },
                    focused: false,
                    alternate_screen: true,
                }],
                cursor: Some(Cursor {
                    x: 1,
                    y: 1,
                    visible: false,
                    shape: 0,
                }),
            })
        );
    }

    #[test]
    fn pane_scrollbar_and_scroll_metrics_are_read_past() {
        // The optional scrollbar rect and scroll metrics are present here, so the
        // fields after them only line up if both are skipped in full.
        let mut e = Enc::default();
        e.vu(u64::from(S_PATCH));
        e.str("b1");
        e.vu(7);
        e.vu(9);
        e.vu(10);
        e.vu(0); // spans
        e.vu(1); // panes
        e.str("w1:p1");
        e.vu(4); // content_revision
        put_rect(
            &mut e,
            Rect {
                x: 0,
                y: 0,
                width: 3,
                height: 1,
            },
        );
        put_rect(
            &mut e,
            Rect {
                x: 0,
                y: 0,
                width: 2,
                height: 1,
            },
        );
        e.some();
        put_rect(
            &mut e,
            Rect {
                x: 2,
                y: 0,
                width: 1,
                height: 1,
            },
        ); // scrollbar_rect
        e.some();
        e.vu(5); // offset_from_bottom
        e.vu(300); // max_offset_from_bottom
        e.vu(2); // viewport_rows
        e.bool(true); // focused
        e.bool(true); // mouse_reporting
        e.bool(true); // sgr_pixel_mouse
        e.bool(false); // alternate_screen_active
        e.vu(0); // pixel_width
        e.vu(0); // pixel_height
        e.none(); // cursor
        assert_eq!(
            decode_server(&e.buf).unwrap(),
            ServerMsg::Patch(SurfacePatch {
                panes: vec![pane("w1:p1", true)],
                ..patch(9, 10, Vec::new())
            })
        );
    }

    #[test]
    fn patch_cut_anywhere_is_an_error() {
        for n in 0..PATCH_GOLDEN.len() {
            assert!(
                decode_server(&PATCH_GOLDEN[..n]).is_err(),
                "a patch cut at {n} bytes decoded"
            );
        }
    }

    #[test]
    fn patch_changes_only_the_row_it_names() {
        let mut s = blank(3, 2);
        let row0 = Arc::clone(&s.rows[0]);
        s.apply_patch(patch(9, 10, vec![(0, 1, vec![plain("x"), plain("y")])]))
            .unwrap();
        assert!(Arc::ptr_eq(&row0, &s.rows[0]), "row 0 was copied");
        assert_eq!(texts(&s.rows[1]), ["x", "y", " "]);
        assert_eq!(s.revision, 10);
    }

    #[test]
    fn patch_span_starts_at_its_column() {
        let mut s = blank(3, 1);
        s.apply_patch(patch(9, 10, vec![(1, 0, vec![plain("x")])]))
            .unwrap();
        assert_eq!(texts(&s.rows[0]), [" ", "x", " "]);
    }

    #[test]
    fn patch_with_no_spans_moves_only_the_revision_and_cursor() {
        let mut s = blank(3, 1);
        let before = s.rows.clone();
        let mut p = patch(9, 10, Vec::new());
        p.cursor = Some(Cursor {
            x: 0,
            y: 0,
            visible: true,
            shape: 6,
        });
        s.apply_patch(p).unwrap();
        assert_eq!(s.revision, 10);
        assert_eq!(s.cursor.map(|c| c.shape), Some(6));
        assert!(s.rows.iter().zip(&before).all(|(a, b)| Arc::ptr_eq(a, b)));
    }

    #[test]
    fn stale_base_revision_is_refused_and_nothing_changes() {
        let mut s = blank(3, 2);
        let before = s.clone();
        let err = s
            .apply_patch(patch(8, 10, vec![(0, 0, vec![plain("x")])]))
            .unwrap_err();
        assert_eq!(err, PatchError::Stale);
        assert_eq!(s, before);
    }

    #[test]
    fn other_boot_is_stale() {
        let mut s = blank(3, 2);
        let mut p = patch(9, 10, Vec::new());
        p.boot_id = "b2".into();
        assert_eq!(s.apply_patch(p), Err(PatchError::Stale));
    }

    #[test]
    fn other_projection_is_stale() {
        let mut s = blank(3, 2);
        let mut p = patch(9, 10, Vec::new());
        p.projection_revision = 8;
        assert_eq!(s.apply_patch(p), Err(PatchError::Stale));
    }

    #[test]
    fn span_past_the_width_is_out_of_range_and_nothing_applies() {
        let mut s = blank(3, 2);
        let before = s.clone();
        let err = s
            .apply_patch(patch(
                9,
                10,
                vec![
                    (0, 0, vec![plain("x")]),
                    (2, 1, vec![plain("y"), plain("z")]),
                ],
            ))
            .unwrap_err();
        assert_eq!(err, PatchError::OutOfRange);
        assert_eq!(s, before, "the first span must not be applied");
    }

    #[test]
    fn span_below_the_last_row_is_out_of_range() {
        let mut s = blank(3, 2);
        let err = s
            .apply_patch(patch(9, 10, vec![(0, 2, vec![plain("x")])]))
            .unwrap_err();
        assert_eq!(err, PatchError::OutOfRange);
    }

    #[test]
    fn patch_cursor_none_clears_the_cursor() {
        let mut s = blank(3, 1);
        s.cursor = Some(Cursor {
            x: 0,
            y: 0,
            visible: true,
            shape: 1,
        });
        s.apply_patch(patch(9, 10, Vec::new())).unwrap();
        assert_eq!(s.cursor, None);
    }

    #[test]
    fn patch_replaces_a_pane_by_id_and_adds_an_unknown_one() {
        let mut s = blank(3, 1);
        s.panes = vec![pane("w1:p1", true)];
        let mut p = patch(9, 10, Vec::new());
        p.panes = vec![pane("w1:p1", false), pane("w1:p2", true)];
        s.apply_patch(p).unwrap();
        assert_eq!(s.panes.len(), 2);
        assert!(!s.panes[0].focused);
        assert_eq!(s.focused_pane().map(|p| p.pane_id.as_str()), Some("w1:p2"));
    }

    #[test]
    fn focused_pane_is_none_when_no_pane_has_focus() {
        let mut s = blank(3, 1);
        s.panes = vec![pane("w1:p1", false)];
        assert!(s.focused_pane().is_none());
    }
}
