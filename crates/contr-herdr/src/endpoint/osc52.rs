// Adapted from Herdr UI (Apache-2.0); changes: takes the raw wire bytes, drops the
// pending-write bound (the link queues updates), adds tests for a query and an empty clear.

//! OSC 52 clipboard writes that herdr forwards to this client.
//!
//! A program inside a pane can ask to set the system clipboard with an OSC 52
//! escape. herdr intercepts that escape and sends the bytes to the client as the
//! base64 `Clipboard` server message. Only text is accepted: the payload is
//! bounded, must decode as base64, and must be valid UTF-8. Anything else is
//! dropped rather than guessed at, so a malformed or hostile escape cannot put
//! bytes on the clipboard that this client never agreed to write. A query (`?`)
//! is not base64, so it is dropped too: contr never reads the clipboard for a
//! program.

/// The largest decoded write contr puts on the clipboard: 2 MiB.
pub(crate) const MAX_TEXT_BYTES: usize = 2 * 1024 * 1024;

/// Decodes one `Clipboard` payload, the base64 text from the wire.
///
/// The daemon encodes the raw bytes with standard base64. Padding is optional
/// and either alphabet is accepted, but any other character, data after
/// padding, a non-canonical tail, invalid UTF-8, or a decoded payload past
/// [`MAX_TEXT_BYTES`] is rejected.
pub(crate) fn decode(data: &[u8]) -> Option<String> {
    let bytes = decode_base64(data)?;
    if bytes.len() > MAX_TEXT_BYTES {
        return None;
    }
    String::from_utf8(bytes).ok()
}

fn decode_base64(data: &[u8]) -> Option<Vec<u8>> {
    // Reject an oversized payload before allocating for it. Four encoded
    // symbols carry at most three bytes.
    if data.len() > MAX_TEXT_BYTES.div_ceil(3) * 4 + 4 {
        return None;
    }
    let mut out = Vec::with_capacity(data.len() / 4 * 3);
    let mut buffer: u32 = 0;
    let mut bits: u32 = 0;
    let mut padding = 0usize;
    for &byte in data {
        let value = match byte {
            b'A'..=b'Z' => byte - b'A',
            b'a'..=b'z' => byte - b'a' + 26,
            b'0'..=b'9' => byte - b'0' + 52,
            b'+' | b'-' => 62,
            b'/' | b'_' => 63,
            b'=' if padding < 2 => {
                padding += 1;
                continue;
            }
            _ => return None,
        };
        if padding != 0 {
            return None;
        }
        buffer = (buffer << 6) | u32::from(value);
        bits += 6;
        if bits >= 8 {
            bits -= 8;
            out.push((buffer >> bits) as u8);
            buffer &= (1 << bits) - 1;
        }
    }
    // The final symbol may pad its quantum with zero bits, never data bits.
    if buffer != 0 {
        return None;
    }
    // Padding, when present, must account for exactly the symbols the last
    // quantum is short by; a lone leftover symbol is never valid.
    let symbols = data.len() - padding;
    if symbols % 4 == 1 || (padding != 0 && symbols % 4 != 4 - padding) {
        return None;
    }
    Some(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn decode_str(data: &str) -> Option<String> {
        decode(data.as_bytes())
    }

    #[test]
    fn decodes_padded_unpadded_and_multibyte_payloads() {
        assert_eq!(decode_str("dGVzdA==").as_deref(), Some("test"));
        assert_eq!(decode_str("dGVzdA").as_deref(), Some("test"));
        assert_eq!(
            decode_str("aGVsbG8gd29ybGQ=").as_deref(),
            Some("hello world")
        );
        assert_eq!(decode_str("5Lmf5LiN").as_deref(), Some("\u{4e5f}\u{4e0d}"));
    }

    #[test]
    fn an_empty_payload_is_a_clear_not_an_error() {
        assert_eq!(decode_str("").as_deref(), Some(""));
    }

    #[test]
    fn rejects_malformed_and_noncanonical_payloads() {
        for data in [
            "dGVzdA=",   // wrong padding for the symbol count
            "dGVzdA===", // more padding than a quantum allows
            "dGVzd!",    // outside the alphabet
            "dGVzdA==x", // data after padding
            "=",         // no symbols at all
            "A",         // a lone symbol carries no byte
            "AB",        // a two-symbol quantum with non-zero trailing bits
        ] {
            assert_eq!(decode_str(data), None, "{data}");
        }
    }

    #[test]
    fn a_query_is_not_a_write() {
        // An OSC 52 query sends `?` in place of the data. It is not base64.
        assert_eq!(decode_str("?"), None);
    }

    #[test]
    fn rejects_non_utf8_payloads() {
        // 0xff is a byte, not valid UTF-8 text.
        assert_eq!(decode_str("/w=="), None);
    }

    #[test]
    fn rejects_oversized_payloads() {
        // Four symbols carry three bytes, so this is the largest payload that
        // still fits the limit; one more quantum spills past it.
        let allowed = "AAAA".repeat(MAX_TEXT_BYTES / 3);
        assert!(decode_str(&allowed).is_some());
        let too_long = format!("{allowed}AAAA");
        assert_eq!(decode_str(&too_long), None);
        // Far past the limit: refused before any decoding.
        assert_eq!(decode_str(&"A".repeat(MAX_TEXT_BYTES * 2)), None);
    }
}
