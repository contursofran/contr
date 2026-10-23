//! Binary codec for herdr's endpoint socket: bincode 2 "standard" rules over
//! serde (varint integers, little endian) and the `[u32 LE length][payload]`
//! frame. Written from herdr's wire layout; see NOTICE.

use std::fmt;
use std::io::{self, Read, Write};

use super::messages::MAX_FRAME;

/// A payload that does not match the layout.
#[derive(Debug)]
pub enum CodecError {
    /// The payload ended before the field was complete.
    Truncated,
    /// A varint marker other than 0..=253.
    BadVarint(u8),
    /// A bool byte other than 0 or 1.
    BadBool(u8),
    /// A string that is not UTF-8.
    BadUtf8,
    /// A length larger than the bytes left, or a value that does not fit its
    /// integer width (`vu16`, `vu32`).
    TooLong(u64),
    /// An enum or option tag this codec does not know.
    BadTag { what: &'static str, tag: u32 },
}

impl fmt::Display for CodecError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Truncated => write!(f, "endpoint payload ended early"),
            Self::BadVarint(marker) => write!(f, "bad varint marker {marker}"),
            Self::BadBool(byte) => write!(f, "bad bool byte {byte}"),
            Self::BadUtf8 => write!(f, "endpoint string is not UTF-8"),
            Self::TooLong(len) => write!(f, "endpoint length or value {len} does not fit"),
            Self::BadTag { what, tag } => write!(f, "unknown {what} tag {tag}"),
        }
    }
}

impl std::error::Error for CodecError {}

/// Writes the fields of one payload.
#[derive(Default)]
pub struct Enc {
    pub buf: Vec<u8>,
}

impl Enc {
    pub fn u8(&mut self, v: u8) {
        self.buf.push(v);
    }

    pub fn bool(&mut self, v: bool) {
        self.buf.push(u8::from(v));
    }

    /// Varint unsigned: values up to 250 in one byte, else a marker (251, 252
    /// or 253) and the value as u16, u32 or u64, little endian.
    pub fn vu(&mut self, v: u64) {
        if v <= 250 {
            self.buf.push(v as u8);
        } else if let Ok(small) = u16::try_from(v) {
            self.buf.push(251);
            self.buf.extend_from_slice(&small.to_le_bytes());
        } else if let Ok(mid) = u32::try_from(v) {
            self.buf.push(252);
            self.buf.extend_from_slice(&mid.to_le_bytes());
        } else {
            self.buf.push(253);
            self.buf.extend_from_slice(&v.to_le_bytes());
        }
    }

    /// Varint length, then the UTF-8 bytes.
    pub fn str(&mut self, s: &str) {
        self.vu(s.len() as u64);
        self.buf.extend_from_slice(s.as_bytes());
    }

    /// A char is its UTF-8 bytes, with no length prefix.
    pub fn char(&mut self, c: char) {
        let mut utf8 = [0u8; 4];
        self.buf
            .extend_from_slice(c.encode_utf8(&mut utf8).as_bytes());
    }

    /// Option tag 0 (None).
    pub fn none(&mut self) {
        self.buf.push(0);
    }

    /// Option tag 1 (Some); the value follows.
    pub fn some(&mut self) {
        self.buf.push(1);
    }
}

/// Reads the fields of one payload. Every read is bounds-checked.
pub struct Dec<'a> {
    buf: &'a [u8],
    pos: usize,
}

impl<'a> Dec<'a> {
    pub fn new(buf: &'a [u8]) -> Self {
        Self { buf, pos: 0 }
    }

    fn take(&mut self, n: usize) -> Result<&'a [u8], CodecError> {
        let end = self.pos.checked_add(n).ok_or(CodecError::Truncated)?;
        let bytes = self.buf.get(self.pos..end).ok_or(CodecError::Truncated)?;
        self.pos = end;
        Ok(bytes)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], CodecError> {
        self.take(N)?.try_into().map_err(|_| CodecError::Truncated)
    }

    pub fn u8(&mut self) -> Result<u8, CodecError> {
        Ok(self.array::<1>()?[0])
    }

    pub fn bool(&mut self) -> Result<bool, CodecError> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            other => Err(CodecError::BadBool(other)),
        }
    }

    pub fn vu(&mut self) -> Result<u64, CodecError> {
        match self.u8()? {
            marker @ 0..=250 => Ok(u64::from(marker)),
            251 => Ok(u64::from(u16::from_le_bytes(self.array()?))),
            252 => Ok(u64::from(u32::from_le_bytes(self.array()?))),
            253 => Ok(u64::from_le_bytes(self.array()?)),
            other => Err(CodecError::BadVarint(other)),
        }
    }

    pub fn vu16(&mut self) -> Result<u16, CodecError> {
        let v = self.vu()?;
        u16::try_from(v).map_err(|_| CodecError::TooLong(v))
    }

    pub fn vu32(&mut self) -> Result<u32, CodecError> {
        let v = self.vu()?;
        u32::try_from(v).map_err(|_| CodecError::TooLong(v))
    }

    /// A varint length. Rejects a length larger than the bytes left: every
    /// element takes at least one byte, so a bad length never allocates.
    // This reads a length prefix; it is not the size of the decoder, so there is no `is_empty`.
    #[allow(clippy::len_without_is_empty)]
    pub fn len(&mut self) -> Result<usize, CodecError> {
        let n = self.vu()?;
        let remaining = self.buf.len().saturating_sub(self.pos);
        match usize::try_from(n) {
            Ok(n) if n <= remaining => Ok(n),
            _ => Err(CodecError::TooLong(n)),
        }
    }

    pub fn str(&mut self) -> Result<String, CodecError> {
        let n = self.len()?;
        String::from_utf8(self.take(n)?.to_vec()).map_err(|_| CodecError::BadUtf8)
    }

    /// Skips a varint length and that many bytes.
    pub fn skip_bytes(&mut self) -> Result<(), CodecError> {
        let n = self.len()?;
        self.take(n)?;
        Ok(())
    }

    /// Option tag. `false` = None; the value follows only for `true`.
    pub fn option(&mut self) -> Result<bool, CodecError> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            other => Err(CodecError::BadTag {
                what: "option",
                tag: u32::from(other),
            }),
        }
    }
}

/// Writes `[u32 LE len][payload]`. Refuses a payload over [`MAX_FRAME`].
pub fn write_frame(w: &mut impl Write, payload: &[u8]) -> io::Result<()> {
    if payload.len() > MAX_FRAME {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "endpoint frame larger than MAX_FRAME",
        ));
    }
    // Checked against MAX_FRAME (32 MiB) above, so the length fits in u32.
    let len = u32::try_from(payload.len())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "endpoint frame too long"))?;
    w.write_all(&len.to_le_bytes())?;
    w.write_all(payload)
}

/// Reads one frame. `Ok(None)` on a clean EOF before the length. EOF inside
/// the header or the body is `UnexpectedEof`; a length over [`MAX_FRAME`] is
/// `InvalidData` and nothing is allocated for it.
pub fn read_frame(r: &mut impl Read) -> io::Result<Option<Vec<u8>>> {
    let mut header = [0u8; 4];
    let mut got = 0;
    while got < header.len() {
        match r.read(&mut header[got..]) {
            Ok(0) if got == 0 => return Ok(None),
            Ok(0) => return Err(io::ErrorKind::UnexpectedEof.into()),
            Ok(n) => got += n,
            Err(e) if e.kind() == io::ErrorKind::Interrupted => {}
            Err(e) => return Err(e),
        }
    }
    let len = u32::from_le_bytes(header) as usize;
    if len > MAX_FRAME {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "endpoint frame larger than MAX_FRAME",
        ));
    }
    let mut payload = vec![0u8; len];
    r.read_exact(&mut payload)?;
    Ok(Some(payload))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::io::Cursor;

    use super::*;

    fn vu_bytes(v: u64) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(v);
        e.buf
    }

    #[test]
    fn varint_golden_bytes() {
        assert_eq!(vu_bytes(0), [0]);
        assert_eq!(vu_bytes(250), [250]);
        assert_eq!(vu_bytes(251), [251, 251, 0]);
        assert_eq!(vu_bytes(65_535), [251, 255, 255]);
        assert_eq!(vu_bytes(65_536), [252, 0, 0, 1, 0]);
        assert_eq!(
            vu_bytes(u64::MAX),
            [253, 255, 255, 255, 255, 255, 255, 255, 255]
        );
    }

    #[test]
    fn varint_round_trips() {
        for v in [
            0,
            1,
            250,
            251,
            65_535,
            65_536,
            u64::from(u32::MAX),
            u64::MAX,
        ] {
            let bytes = vu_bytes(v);
            let mut d = Dec::new(&bytes);
            assert_eq!(d.vu().unwrap(), v);
        }
    }

    #[test]
    fn vu16_and_vu32_reject_overflow() {
        let bytes = vu_bytes(70_000);
        assert!(matches!(
            Dec::new(&bytes).vu16(),
            Err(CodecError::TooLong(70_000))
        ));
        let bytes = vu_bytes(u64::from(u32::MAX) + 1);
        assert!(matches!(
            Dec::new(&bytes).vu32(),
            Err(CodecError::TooLong(_))
        ));
        let bytes = vu_bytes(70_000);
        assert_eq!(Dec::new(&bytes).vu32().unwrap(), 70_000);
    }

    #[test]
    fn varint_bad_marker_is_an_error() {
        assert!(matches!(
            Dec::new(&[254]).vu(),
            Err(CodecError::BadVarint(254))
        ));
        assert!(matches!(
            Dec::new(&[255]).vu(),
            Err(CodecError::BadVarint(255))
        ));
    }

    #[test]
    fn str_and_char_bytes() {
        let mut e = Enc::default();
        e.str("é");
        assert_eq!(e.buf, [2, 0xC3, 0xA9]);
        let mut e = Enc::default();
        e.char('é');
        assert_eq!(e.buf, [0xC3, 0xA9]);
    }

    #[test]
    fn str_round_trips_and_rejects_bad_utf8() {
        let mut e = Enc::default();
        e.str("w1:p1");
        assert_eq!(Dec::new(&e.buf).str().unwrap(), "w1:p1");
        assert!(matches!(
            Dec::new(&[1, 0xFF]).str(),
            Err(CodecError::BadUtf8)
        ));
    }

    #[test]
    fn bool_accepts_only_zero_and_one() {
        assert!(!Dec::new(&[0]).bool().unwrap());
        assert!(Dec::new(&[1]).bool().unwrap());
        assert!(matches!(Dec::new(&[2]).bool(), Err(CodecError::BadBool(2))));
    }

    #[test]
    fn option_tags() {
        assert!(!Dec::new(&[0]).option().unwrap());
        assert!(Dec::new(&[1]).option().unwrap());
        assert!(matches!(
            Dec::new(&[7]).option(),
            Err(CodecError::BadTag {
                what: "option",
                tag: 7
            })
        ));
    }

    #[test]
    fn truncated_input_is_an_error() {
        assert!(matches!(Dec::new(&[]).u8(), Err(CodecError::Truncated)));
        assert!(matches!(
            Dec::new(&[251, 1]).vu(),
            Err(CodecError::Truncated)
        ));
        assert!(matches!(
            Dec::new(&[3, b'a']).str(),
            Err(CodecError::TooLong(3))
        ));
    }

    #[test]
    fn len_rejects_a_length_past_the_end() {
        // 1000 as varint, then only 3 bytes left.
        let bytes = [251, 0xE8, 0x03, 1, 2, 3];
        assert!(matches!(
            Dec::new(&bytes).len(),
            Err(CodecError::TooLong(1000))
        ));
    }

    #[test]
    fn skip_bytes_moves_past_a_blob() {
        let bytes = [2, 9, 9, 7];
        let mut d = Dec::new(&bytes);
        d.skip_bytes().unwrap();
        assert_eq!(d.u8().unwrap(), 7);
    }

    #[test]
    fn frame_round_trip() {
        let mut wire = Vec::new();
        write_frame(&mut wire, b"hello").unwrap();
        assert_eq!(wire, [5, 0, 0, 0, b'h', b'e', b'l', b'l', b'o']);
        let mut r = Cursor::new(wire);
        assert_eq!(read_frame(&mut r).unwrap(), Some(b"hello".to_vec()));
        assert_eq!(read_frame(&mut r).unwrap(), None);
    }

    #[test]
    fn empty_reader_is_a_clean_eof() {
        let mut r = Cursor::new(Vec::new());
        assert_eq!(read_frame(&mut r).unwrap(), None);
    }

    #[test]
    fn short_header_is_unexpected_eof() {
        let mut r = Cursor::new(vec![5, 0]);
        let err = read_frame(&mut r).unwrap_err();
        assert_eq!(err.kind(), io::ErrorKind::UnexpectedEof);
    }

    #[test]
    fn short_body_is_unexpected_eof() {
        let mut r = Cursor::new(vec![5, 0, 0, 0, b'a']);
        let err = read_frame(&mut r).unwrap_err();
        assert_eq!(err.kind(), io::ErrorKind::UnexpectedEof);
    }

    #[test]
    fn oversized_header_is_invalid_data_without_allocating() {
        let len = u32::try_from(MAX_FRAME + 1).unwrap();
        let mut r = Cursor::new(len.to_le_bytes().to_vec());
        let err = read_frame(&mut r).unwrap_err();
        assert_eq!(err.kind(), io::ErrorKind::InvalidData);
    }

    #[test]
    fn oversized_payload_is_refused_on_write() {
        let payload = vec![0u8; MAX_FRAME + 1];
        let mut wire = Vec::new();
        let err = write_frame(&mut wire, &payload).unwrap_err();
        assert_eq!(err.kind(), io::ErrorKind::InvalidInput);
        assert!(wire.is_empty());
    }

    /// Fails once with `Interrupted` before it hands out the bytes.
    struct Interrupting {
        interrupted: bool,
        inner: Cursor<Vec<u8>>,
    }

    impl Read for Interrupting {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            if !self.interrupted {
                self.interrupted = true;
                return Err(io::ErrorKind::Interrupted.into());
            }
            self.inner.read(buf)
        }
    }

    #[test]
    fn interrupted_reads_are_retried() {
        let mut r = Interrupting {
            interrupted: false,
            inner: Cursor::new(vec![2, 0, 0, 0, b'o', b'k']),
        };
        assert_eq!(read_frame(&mut r).unwrap(), Some(b"ok".to_vec()));
    }
}
