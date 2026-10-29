// Adapted from Herdr UI (Apache-2.0); changes: ported to Go, takes the raw wire bytes, drops the
// pending-write bound (the link queues updates), adds tests for a query and an empty clear.

package endpoint

import "unicode/utf8"

// OSC 52 clipboard writes that herdr forwards to this client.
//
// A program inside a pane can ask to set the system clipboard with an OSC 52
// escape. herdr intercepts that escape and sends the bytes to the client as the
// base64 Clipboard server message. Only text is accepted: the payload is
// bounded, must decode as base64, and must be valid UTF-8. Anything else is
// dropped rather than guessed at. A query ("?") is not base64, so it is
// dropped too: contr never reads the clipboard for a program.

// MaxTextBytes is the largest decoded write contr puts on the clipboard: 2 MiB.
const MaxTextBytes = 2 * 1024 * 1024

// DecodeOSC52 decodes one Clipboard payload, the base64 text from the wire.
//
// Padding is optional and either alphabet is accepted, but any other
// character, data after padding, a non-canonical tail, invalid UTF-8, or a
// decoded payload past MaxTextBytes is rejected (ok is false).
func DecodeOSC52(data []byte) (text string, ok bool) {
	b, ok := decodeBase64(data)
	if !ok || len(b) > MaxTextBytes || !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

func decodeBase64(data []byte) ([]byte, bool) {
	// Reject an oversized payload before allocating for it. Four encoded
	// symbols carry at most three bytes.
	if len(data) > (MaxTextBytes+2)/3*4+4 {
		return nil, false
	}
	out := make([]byte, 0, len(data)/4*3)
	var buffer uint32
	var bits uint
	padding := 0
	for _, c := range data {
		var value byte
		switch {
		case c >= 'A' && c <= 'Z':
			value = c - 'A'
		case c >= 'a' && c <= 'z':
			value = c - 'a' + 26
		case c >= '0' && c <= '9':
			value = c - '0' + 52
		case c == '+' || c == '-':
			value = 62
		case c == '/' || c == '_':
			value = 63
		case c == '=' && padding < 2:
			padding++
			continue
		default:
			return nil, false
		}
		if padding != 0 {
			return nil, false
		}
		buffer = buffer<<6 | uint32(value)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buffer>>bits))
			buffer &= 1<<bits - 1
		}
	}
	// The final symbol may pad its quantum with zero bits, never data bits.
	if buffer != 0 {
		return nil, false
	}
	// Padding, when present, must account for exactly the symbols the last
	// quantum is short by; a lone leftover symbol is never valid.
	symbols := len(data) - padding
	if symbols%4 == 1 || (padding != 0 && symbols%4 != 4-padding) {
		return nil, false
	}
	return out, true
}
