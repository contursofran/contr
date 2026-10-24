//! Messages of herdr's endpoint socket, generation 1: the ones contr sends
//! (hello, resize, input with keys, text, paste and wheel, focus, detach) and
//! the server ones it reads. Surface
//! frames and patches are decoded in `surface`. Written from herdr's wire
//! layout; see NOTICE.

use serde::{Deserialize, Serialize};

use super::codec::{CodecError, Dec, Enc};
use super::surface::{self, SurfaceFrame, SurfacePatch};

pub const ENDPOINT_GENERATION: u32 = 1;
pub const HELLO_KIND: &str = "endpoint.hello.v1";
pub const WELCOME_KIND: &str = "endpoint.welcome.v1";
pub const SNAPSHOT_KIND: &str = "shell.snapshot.v1";
pub const SNAPSHOT_CODEC: &str = "shell.snapshot.v1";
pub const SURFACE_CODEC: &str = "shell.surface.v1";
pub const INPUT_CODEC: &str = "shell.input.semantic.v1";
pub const BLOB_CODEC: &str = "shell.blob.v1";
/// Largest frame either side accepts (herdr allows 32 MiB with graphics).
pub const MAX_FRAME: usize = 32 * 1024 * 1024;
/// Limits herdr puts on a resize: columns and rows each in 1..=`MAX_GRID_SIDE`, their product at
/// most `MAX_GRID_CELLS`, and a cell side at most `MAX_CELL_PX` device pixels.
pub const MAX_GRID_SIDE: u16 = 4096;
pub const MAX_GRID_CELLS: u32 = 1_000_000;
pub const MAX_CELL_PX: u32 = 4096;
/// herdr versions whose wire layout was checked against source (v0.9.3 live, v0.9.4/v0.9.5 by source diff).
pub const TESTED_HERDR_VERSIONS: &[&str] = &["0.9.3", "0.9.4", "0.9.5"];

// ClientMessage tags we send.
pub(crate) const C_DETACH: u32 = 4;
pub(crate) const C_RESIZE: u32 = 12;
pub(crate) const C_PANE_INPUT: u32 = 13;
pub(crate) const C_FOCUS: u32 = 18;
pub(crate) const C_CONTROL: u32 = 20;

// ServerMessage tags we read. Every other tag is `ServerMsg::Other`.
pub(crate) const S_WELCOME_LEGACY: u32 = 0;
pub(crate) const S_SHUTDOWN: u32 = 3;
pub(crate) const S_SHELL_ERROR: u32 = 15;
pub(crate) const S_CONTROL: u32 = 20;
pub(crate) const S_SURFACE: u32 = 13;
pub(crate) const S_PATCH: u32 = 19;

/// Grid size in cells.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct GridSize {
    pub cols: u16,
    pub rows: u16,
}

/// One cell in pixels.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CellPx {
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KeyCode {
    Backspace,
    Enter,
    Left,
    Right,
    Up,
    Down,
    Home,
    End,
    PageUp,
    PageDown,
    Tab,
    BackTab,
    Delete,
    Insert,
    Esc,
    Char(char),
    F(u8),
}

/// Key modifier bits, as herdr reads them.
pub mod mods {
    pub const SHIFT: u8 = 1;
    pub const CONTROL: u8 = 2;
    pub const ALT: u8 = 4;
}

/// The direction of one wheel step. `Left` and `Right` are horizontal.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WheelDir {
    Up,
    Down,
    Left,
    Right,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InputEvent {
    Key {
        code: KeyCode,
        mods: u8,
        repeat: bool,
    },
    Text(String),
    Paste(String),
    /// A wheel step at a cell of the pane, relative to its inner rect. herdr scrolls its own
    /// history by `lines`, or sends one wheel report or arrow key to the program.
    Wheel {
        dir: WheelDir,
        column: u16,
        row: u16,
        mods: u8,
        lines: u16,
    },
}

/// What contr sends to herdr.
#[derive(Debug)]
pub enum ClientMsg<'a> {
    Hello {
        size: GridSize,
        cell: CellPx,
    },
    Resize {
        size: GridSize,
        cell: CellPx,
    },
    Input {
        pane_id: &'a str,
        events: &'a [InputEvent],
    },
    Focus(bool),
    Detach,
}

impl ClientMsg<'_> {
    /// Payload only. The caller adds the length prefix with `codec::write_frame`.
    pub fn encode(&self) -> Vec<u8> {
        let mut e = Enc::default();
        match self {
            Self::Hello { size, cell } => {
                e.vu(u64::from(C_CONTROL));
                e.str(HELLO_KIND);
                e.str(&hello_json(*size, *cell));
            }
            Self::Resize { size, cell } => {
                e.vu(u64::from(C_RESIZE));
                e.vu(u64::from(cell.width));
                e.vu(u64::from(cell.height));
                e.vu(u64::from(size.cols));
                e.vu(u64::from(size.rows));
                e.bool(false); // pixel_mouse
            }
            Self::Input { pane_id, events } => {
                e.vu(u64::from(C_PANE_INPUT));
                e.str(pane_id);
                // The number of events is a usize; it is far below u64::MAX.
                e.vu(events.len() as u64);
                for event in events.iter() {
                    put_event(&mut e, event);
                }
            }
            Self::Focus(focused) => {
                e.vu(u64::from(C_FOCUS));
                e.bool(*focused);
            }
            Self::Detach => e.vu(u64::from(C_DETACH)),
        }
        e.buf
    }
}

#[derive(Serialize)]
struct HelloJson {
    generation: u32,
    cell_width_px: u32,
    cell_height_px: u32,
    surface_size: SizeJson,
    pixel_mouse: bool,
    direct_graphics: bool,
    endpoint_keybindings: bool,
    mouse_capture: bool,
    surface_active: bool,
    surface_reuse: bool,
    surface_delta: bool,
    surface_scroll: bool,
    snapshot_codecs: &'static [&'static str],
    surface_codecs: &'static [&'static str],
    input_codecs: &'static [&'static str],
    blob_codecs: &'static [&'static str],
}

#[derive(Serialize)]
struct SizeJson {
    cols: u16,
    rows: u16,
}

fn hello_json(size: GridSize, cell: CellPx) -> String {
    let hello = HelloJson {
        generation: ENDPOINT_GENERATION,
        cell_width_px: cell.width,
        cell_height_px: cell.height,
        surface_size: SizeJson {
            cols: size.cols,
            rows: size.rows,
        },
        pixel_mouse: false,
        direct_graphics: false,
        endpoint_keybindings: false,
        mouse_capture: false,
        surface_active: true,
        surface_reuse: false,
        surface_delta: false,
        surface_scroll: false,
        snapshot_codecs: &[SNAPSHOT_CODEC],
        surface_codecs: &[SURFACE_CODEC],
        input_codecs: &[INPUT_CODEC],
        blob_codecs: &[BLOB_CODEC],
    };
    // A struct of numbers, bools and static strings: serialising it cannot fail.
    serde_json::to_string(&hello).unwrap_or_default()
}

/// One `ClientPaneInputEvent`.
fn put_event(e: &mut Enc, event: &InputEvent) {
    match event {
        InputEvent::Key { code, mods, repeat } => {
            e.vu(0); // Key
            put_code(e, *code);
            e.u8(*mods);
            e.vu(u64::from(*repeat)); // ClientKeyKind: Press 0, Repeat 1
            e.vu(1); // repeat_count
            e.none(); // shifted_codepoint
            e.none(); // generated_text
            e.bool(false); // tracks_release
            e.none(); // physical_key_id
            e.none(); // windows_record
        }
        InputEvent::Text(text) => {
            e.vu(1); // TextCommit
            e.str(text);
        }
        InputEvent::Paste(text) => {
            e.vu(3); // Paste
            e.str(text);
        }
        InputEvent::Wheel {
            dir,
            column,
            row,
            mods,
            lines,
        } => {
            e.vu(2); // Mouse
            e.vu(match dir {
                WheelDir::Up => 4,    // ScrollUp
                WheelDir::Down => 5,  // ScrollDown
                WheelDir::Left => 6,  // ScrollLeft
                WheelDir::Right => 7, // ScrollRight
            });
            e.vu(0); // position: Cell
            e.vu(u64::from(*column));
            e.vu(u64::from(*row));
            e.none(); // geometry: none, as cells are sent, not pixels
            e.u8(*mods);
            e.vu(u64::from(*lines));
        }
    }
}

/// One `ClientKeyCode`: the variant tag, then a char or an F-number when it has one.
fn put_code(e: &mut Enc, code: KeyCode) {
    let tag: u64 = match code {
        KeyCode::Backspace => 0,
        KeyCode::Enter => 1,
        KeyCode::Left => 2,
        KeyCode::Right => 3,
        KeyCode::Up => 4,
        KeyCode::Down => 5,
        KeyCode::Home => 6,
        KeyCode::End => 7,
        KeyCode::PageUp => 8,
        KeyCode::PageDown => 9,
        KeyCode::Tab => 10,
        KeyCode::BackTab => 11,
        KeyCode::Delete => 12,
        KeyCode::Insert => 13,
        KeyCode::Esc => 14,
        KeyCode::Char(_) => 15,
        KeyCode::F(_) => 16,
    };
    e.vu(tag);
    match code {
        KeyCode::Char(c) => e.char(c),
        KeyCode::F(n) => e.u8(n),
        _ => {}
    }
}

/// Welcome reply to our hello (the JSON in an `endpoint.welcome.v1` control frame).
#[derive(Debug, Clone, Deserialize)]
pub struct Welcome {
    pub generation: u32,
    pub server_version: String,
    pub snapshot_codec: String,
    pub surface_codec: String,
    pub input_codec: String,
    pub blob_codec: String,
    #[serde(default)]
    pub error: Option<WelcomeError>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct WelcomeError {
    pub code: String,
    pub message: String,
}

/// The `shell.snapshot.v1` control frame: the shell's view, without surface data.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct ShellSnapshot {
    pub boot_id: String,
    pub revision: u64,
    #[serde(default)]
    pub focused_pane_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VersionCheck {
    /// The server version is in `TESTED_HERDR_VERSIONS`.
    Ok,
    /// A server version contr has not been tested with: warn and connect.
    Untested(String),
}

/// Refuses only a welcome with an `error`, a generation other than
/// `ENDPOINT_GENERATION`, or a codec name other than the v1 one. Any other
/// server version connects as `Untested`.
pub fn check_welcome(w: &Welcome) -> Result<VersionCheck, String> {
    if let Some(err) = &w.error {
        return Err(format!(
            "herdr refused the endpoint: {}: {}",
            err.code, err.message
        ));
    }
    if w.generation != ENDPOINT_GENERATION {
        return Err(format!(
            "herdr speaks endpoint generation {}, contr speaks {ENDPOINT_GENERATION}",
            w.generation
        ));
    }
    let codecs = [
        ("snapshot", w.snapshot_codec.as_str(), SNAPSHOT_CODEC),
        ("surface", w.surface_codec.as_str(), SURFACE_CODEC),
        ("input", w.input_codec.as_str(), INPUT_CODEC),
        ("blob", w.blob_codec.as_str(), BLOB_CODEC),
    ];
    for (what, offered, wanted) in codecs {
        if offered != wanted {
            return Err(format!(
                "herdr offers {what} codec {offered}, contr needs {wanted}"
            ));
        }
    }
    if TESTED_HERDR_VERSIONS.contains(&w.server_version.as_str()) {
        Ok(VersionCheck::Ok)
    } else {
        Ok(VersionCheck::Untested(w.server_version.clone()))
    }
}

/// A server message contr decodes. Other tags come back as `Other(tag)`
/// without their body being read.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ServerMsg {
    /// An `EndpointControl` frame: `kind` names it, `data` is JSON.
    Control {
        kind: String,
        data: String,
    },
    Shutdown(Option<String>),
    ShellError(String),
    /// A welcome for a first message herdr does not know (pre-generation-1 servers).
    LegacyWelcome {
        error: Option<String>,
    },
    /// The full surface of the focused tab (`PaneSurface`).
    Surface(SurfaceFrame),
    /// Row spans that change the last surface (`PaneSurfacePatch`).
    Patch(SurfacePatch),
    Other(u32),
}

/// Decodes one server payload (the bytes after the length prefix).
pub fn decode_server(payload: &[u8]) -> Result<ServerMsg, CodecError> {
    let mut d = Dec::new(payload);
    let tag = d.vu32()?;
    Ok(match tag {
        S_WELCOME_LEGACY => {
            let _version = d.vu32()?;
            // RenderEncoding has two variants; any other index is not a layout we know.
            match d.vu32()? {
                0 | 1 => {}
                other => {
                    return Err(CodecError::BadTag {
                        what: "render encoding",
                        tag: other,
                    });
                }
            }
            ServerMsg::LegacyWelcome {
                error: opt_str(&mut d)?,
            }
        }
        S_SHUTDOWN => ServerMsg::Shutdown(opt_str(&mut d)?),
        S_SHELL_ERROR => ServerMsg::ShellError(d.str()?),
        S_CONTROL => ServerMsg::Control {
            kind: d.str()?,
            data: d.str()?,
        },
        S_SURFACE => ServerMsg::Surface(surface::decode_frame(&mut d)?),
        S_PATCH => ServerMsg::Patch(surface::decode_patch(&mut d)?),
        other => ServerMsg::Other(other),
    })
}

fn opt_str(d: &mut Dec<'_>) -> Result<Option<String>, CodecError> {
    if d.option()? {
        Ok(Some(d.str()?))
    } else {
        Ok(None)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use serde_json::{Value, json};

    use super::*;

    const SIZE: GridSize = GridSize { cols: 80, rows: 24 };
    const CELL: CellPx = CellPx {
        width: 8,
        height: 16,
    };

    fn welcome_json(extra: &str) -> String {
        format!(
            r#"{{"generation":1,"server_version":"0.9.3","snapshot_codec":"shell.snapshot.v1","surface_codec":"shell.surface.v1","input_codec":"shell.input.semantic.v1","blob_codec":"shell.blob.v1"{extra}}}"#
        )
    }

    fn welcome(json: &str) -> Welcome {
        serde_json::from_str(json).unwrap()
    }

    /// Reads back the payload of a `C_CONTROL` message we encoded.
    fn hello_data(payload: &[u8]) -> Value {
        let mut d = Dec::new(payload);
        assert_eq!(d.vu().unwrap(), u64::from(C_CONTROL));
        assert_eq!(d.str().unwrap(), HELLO_KIND);
        serde_json::from_str(&d.str().unwrap()).unwrap()
    }

    #[test]
    fn resize_golden_bytes() {
        let msg = ClientMsg::Resize {
            size: SIZE,
            cell: CELL,
        };
        assert_eq!(msg.encode(), [12, 8, 16, 80, 24, 0]);
    }

    #[test]
    fn input_golden_bytes() {
        let events = [
            InputEvent::Text("ls".into()),
            InputEvent::Key {
                code: KeyCode::Enter,
                mods: 0,
                repeat: false,
            },
            InputEvent::Key {
                code: KeyCode::Char('c'),
                mods: mods::CONTROL,
                repeat: false,
            },
        ];
        let msg = ClientMsg::Input {
            pane_id: "w1:p1",
            events: &events,
        };
        // Tag 13, pane id (len 5), 3 events.
        // Text: tag 1, len 2, "ls".
        // Key Enter: tag 0, code 1, mods 0, kind 0 (Press), repeat_count 1,
        //   then six empty option/bool/option fields (shifted, generated, tracks, physical, windows).
        // Key Char('c') with CONTROL: tag 0, code 15, 'c', mods 2, kind 0, repeat_count 1, then the same tail.
        #[rustfmt::skip]
        let expected = [
            13, 5, b'w', b'1', b':', b'p', b'1',
            3,
            1, 2, b'l', b's',
            0, 1, 0, 0, 1, 0, 0, 0, 0, 0,
            0, 15, b'c', 2, 0, 1, 0, 0, 0, 0, 0,
        ];
        assert_eq!(msg.encode(), expected);
    }

    #[test]
    fn every_key_code_has_its_tag() {
        let codes: [(KeyCode, &[u8]); 17] = [
            (KeyCode::Backspace, &[0]),
            (KeyCode::Enter, &[1]),
            (KeyCode::Left, &[2]),
            (KeyCode::Right, &[3]),
            (KeyCode::Up, &[4]),
            (KeyCode::Down, &[5]),
            (KeyCode::Home, &[6]),
            (KeyCode::End, &[7]),
            (KeyCode::PageUp, &[8]),
            (KeyCode::PageDown, &[9]),
            (KeyCode::Tab, &[10]),
            (KeyCode::BackTab, &[11]),
            (KeyCode::Delete, &[12]),
            (KeyCode::Insert, &[13]),
            (KeyCode::Esc, &[14]),
            (KeyCode::Char('é'), &[15, 0xC3, 0xA9]),
            (KeyCode::F(5), &[16, 5]),
        ];
        for (code, bytes) in codes {
            let mut e = Enc::default();
            put_code(&mut e, code);
            assert_eq!(e.buf, bytes, "{code:?}");
        }
    }

    #[test]
    fn key_codes_and_events_encode_as_specified() {
        let mut e = Enc::default();
        put_code(&mut e, KeyCode::F(5));
        assert_eq!(e.buf, [16, 5]);

        let mut e = Enc::default();
        put_event(&mut e, &InputEvent::Paste("x".into()));
        assert_eq!(e.buf, [3, 1, b'x']);

        let mut e = Enc::default();
        put_event(
            &mut e,
            &InputEvent::Key {
                code: KeyCode::Enter,
                mods: 0,
                repeat: true,
            },
        );
        assert_eq!(e.buf, [0, 1, 0, 1, 1, 0, 0, 0, 0, 0], "repeat is kind 1");
    }

    #[test]
    fn wheel_golden_bytes() {
        let events = [
            InputEvent::Wheel {
                dir: WheelDir::Up,
                column: 3,
                row: 200,
                mods: mods::SHIFT,
                lines: 3,
            },
            InputEvent::Wheel {
                dir: WheelDir::Down,
                column: 0,
                row: 0,
                mods: 0,
                lines: 300,
            },
        ];
        let msg = ClientMsg::Input {
            pane_id: "w1:p1",
            events: &events,
        };
        // Tag 13, pane id (len 5), 2 events.
        // Wheel Up: tag 2 (Mouse), kind 4 (ScrollUp), position tag 0 (Cell), column 3, row 200,
        //   geometry none, mods 1 (SHIFT), lines 3.
        // Wheel Down: tag 2, kind 5 (ScrollDown), Cell 0, column 0, row 0, none, mods 0,
        //   lines 300 (marker 251, then u16 little-endian: 44, 1).
        #[rustfmt::skip]
        let expected = [
            13, 5, b'w', b'1', b':', b'p', b'1',
            2,
            2, 4, 0, 3, 200, 0, 1, 3,
            2, 5, 0, 0, 0, 0, 0, 251, 44, 1,
        ];
        assert_eq!(msg.encode(), expected);
    }

    #[test]
    fn every_wheel_direction_has_its_kind() {
        let cases: [(WheelDir, u8); 4] = [
            (WheelDir::Up, 4),
            (WheelDir::Down, 5),
            (WheelDir::Left, 6),
            (WheelDir::Right, 7),
        ];
        for (dir, kind) in cases {
            let event = InputEvent::Wheel {
                dir,
                column: 0,
                row: 0,
                mods: 0,
                lines: 1,
            };
            let mut e = Enc::default();
            put_event(&mut e, &event);
            // Mouse tag 2, the kind, Cell 0, column 0, row 0, geometry none, mods 0, lines 1.
            assert_eq!(e.buf, [2, kind, 0, 0, 0, 0, 0, 1], "{dir:?}");
        }
    }

    #[test]
    fn focus_and_detach_golden_bytes() {
        assert_eq!(ClientMsg::Focus(true).encode(), [18, 1]);
        assert_eq!(ClientMsg::Focus(false).encode(), [18, 0]);
        assert_eq!(ClientMsg::Detach.encode(), [4]);
    }

    #[test]
    fn hello_carries_every_field_of_the_contract() {
        let payload = ClientMsg::Hello {
            size: SIZE,
            cell: CELL,
        }
        .encode();
        assert_eq!(
            hello_data(&payload),
            json!({
                "generation": 1,
                "cell_width_px": 8,
                "cell_height_px": 16,
                "surface_size": {"cols": 80, "rows": 24},
                "pixel_mouse": false,
                "direct_graphics": false,
                "endpoint_keybindings": false,
                "mouse_capture": false,
                "surface_active": true,
                "surface_reuse": false,
                "surface_delta": false,
                "surface_scroll": false,
                "snapshot_codecs": ["shell.snapshot.v1"],
                "surface_codecs": ["shell.surface.v1"],
                "input_codecs": ["shell.input.semantic.v1"],
                "blob_codecs": ["shell.blob.v1"]
            })
        );
    }

    #[test]
    fn welcome_check_accepts_a_tested_version() {
        let w = welcome(&welcome_json(""));
        assert_eq!(check_welcome(&w), Ok(VersionCheck::Ok));
    }

    #[test]
    fn welcome_check_warns_on_an_untested_version() {
        let w = welcome(&welcome_json("").replace("0.9.3", "0.10.0"));
        assert_eq!(
            check_welcome(&w),
            Ok(VersionCheck::Untested("0.10.0".into()))
        );
    }

    #[test]
    fn welcome_check_refuses_another_generation() {
        let w = welcome(&welcome_json("").replace("\"generation\":1", "\"generation\":2"));
        let err = check_welcome(&w).unwrap_err();
        assert!(err.contains("generation 2"), "{err}");
    }

    #[test]
    fn welcome_check_refuses_a_server_error_with_its_message() {
        let w = welcome(&welcome_json(
            r#","error":{"code":"no_common_core","message":"no shared core"}"#,
        ));
        let err = check_welcome(&w).unwrap_err();
        assert!(
            err.contains("no_common_core") && err.contains("no shared core"),
            "{err}"
        );
    }

    #[test]
    fn welcome_check_refuses_another_surface_codec() {
        let w = welcome(&welcome_json("").replace("shell.surface.v1", "shell.surface.v2"));
        let err = check_welcome(&w).unwrap_err();
        assert!(
            err.contains("surface") && err.contains("shell.surface.v2"),
            "{err}"
        );
    }

    #[test]
    fn welcome_parses_with_unknown_fields_and_without_error() {
        let w = welcome(&welcome_json(
            r#","future_field":[1,2],"another":{"x":true}"#,
        ));
        assert!(w.error.is_none());
        assert_eq!(check_welcome(&w), Ok(VersionCheck::Ok));
    }

    #[test]
    fn snapshot_reads_the_focused_pane_when_present() {
        let snap: ShellSnapshot = serde_json::from_str(
            r#"{"boot_id":"b1","revision":7,"focused_pane_id":"w1:p1","extra":1}"#,
        )
        .unwrap();
        assert_eq!(snap.boot_id, "b1");
        assert_eq!(snap.revision, 7);
        assert_eq!(snap.focused_pane_id.as_deref(), Some("w1:p1"));
        let bare: ShellSnapshot = serde_json::from_str(r#"{"boot_id":"b2","revision":1}"#).unwrap();
        assert_eq!(bare.focused_pane_id, None);
    }

    /// Builds a server payload with the encoder, as herdr would send it.
    fn server_payload(build: impl FnOnce(&mut Enc)) -> Vec<u8> {
        let mut e = Enc::default();
        build(&mut e);
        e.buf
    }

    #[test]
    fn decodes_control_frames() {
        let payload = server_payload(|e| {
            e.vu(u64::from(S_CONTROL));
            e.str("shell.snapshot.v1");
            e.str("{}");
        });
        assert_eq!(
            decode_server(&payload).unwrap(),
            ServerMsg::Control {
                kind: "shell.snapshot.v1".into(),
                data: "{}".into(),
            }
        );
    }

    #[test]
    fn decodes_shutdown_with_and_without_reason() {
        let none = server_payload(|e| {
            e.vu(u64::from(S_SHUTDOWN));
            e.none();
        });
        assert_eq!(decode_server(&none).unwrap(), ServerMsg::Shutdown(None));

        let some = server_payload(|e| {
            e.vu(u64::from(S_SHUTDOWN));
            e.some();
            e.str("bye");
        });
        assert_eq!(
            decode_server(&some).unwrap(),
            ServerMsg::Shutdown(Some("bye".into()))
        );
    }

    #[test]
    fn decodes_shell_error() {
        let payload = server_payload(|e| {
            e.vu(u64::from(S_SHELL_ERROR));
            e.str("hi");
        });
        assert_eq!(
            decode_server(&payload).unwrap(),
            ServerMsg::ShellError("hi".into())
        );
    }

    #[test]
    fn decodes_legacy_welcome_with_and_without_error() {
        let refused = server_payload(|e| {
            e.vu(u64::from(S_WELCOME_LEGACY));
            e.vu(1); // version
            e.vu(0); // encoding
            e.some();
            e.str("nope");
        });
        assert_eq!(
            decode_server(&refused).unwrap(),
            ServerMsg::LegacyWelcome {
                error: Some("nope".into())
            }
        );

        let ok = server_payload(|e| {
            e.vu(u64::from(S_WELCOME_LEGACY));
            e.vu(1);
            e.vu(0);
            e.none();
        });
        assert_eq!(
            decode_server(&ok).unwrap(),
            ServerMsg::LegacyWelcome { error: None }
        );
    }

    #[test]
    fn legacy_welcome_checks_the_render_encoding_index() {
        for encoding in [0, 1] {
            let payload = server_payload(|e| {
                e.vu(u64::from(S_WELCOME_LEGACY));
                e.vu(1);
                e.vu(encoding);
                e.none();
            });
            assert_eq!(
                decode_server(&payload).unwrap(),
                ServerMsg::LegacyWelcome { error: None },
                "encoding {encoding}"
            );
        }

        let unknown = server_payload(|e| {
            e.vu(u64::from(S_WELCOME_LEGACY));
            e.vu(1);
            e.vu(2);
            e.none();
        });
        assert!(matches!(
            decode_server(&unknown),
            Err(CodecError::BadTag {
                what: "render encoding",
                tag: 2
            })
        ));
    }

    #[test]
    fn unknown_tags_are_other_and_their_body_is_not_read() {
        assert_eq!(decode_server(&[7]).unwrap(), ServerMsg::Other(7));
        assert_eq!(decode_server(&[7, 1, 2, 3]).unwrap(), ServerMsg::Other(7));
    }

    #[test]
    fn decode_errors_are_reported() {
        assert!(matches!(decode_server(&[]), Err(CodecError::Truncated)));
        // Shutdown with an option tag that is neither 0 nor 1.
        assert!(matches!(
            decode_server(&[3, 9]),
            Err(CodecError::BadTag {
                what: "option",
                tag: 9
            })
        ));
        // Control whose kind string claims 5 bytes but has 1.
        assert!(matches!(
            decode_server(&[20, 5, b'a']),
            Err(CodecError::TooLong(5))
        ));
    }
}
