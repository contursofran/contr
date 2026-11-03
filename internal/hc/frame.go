package hc

// Written from herdr-core's wire layout; see NOTICE.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// A framed connection carries frames: a 9 byte header, then the payload.
// The header is the channel (uint32), the kind (uint8) and the payload
// length (uint32), little endian. Channel 0 carries JSON-RPC messages; each
// attached terminal gets a channel of its own.
const (
	// HeaderLen is the length of a frame header.
	HeaderLen = 9
	// MaxPayload is the longest payload that either side accepts.
	MaxPayload = 16 << 20
	// Window is the credit that a stream starts with: the bytes that the
	// core may send before the viewer acknowledges any.
	Window = 256 * 1024
	// creditBatch is the least credit that a stream sends at once. A flood
	// comes as many small frames, and a credit for each costs a write. The
	// core can still send Window-creditBatch bytes before it must wait.
	creditBatch = 16 * 1024
	// writeChunk is the longest DATA payload that this client sends.
	writeChunk = 16 * 1024
)

// Frame kinds.
const (
	// KindData is payload bytes: JSON-RPC on channel 0, terminal bytes on a stream.
	KindData byte = 0
	// KindCredit (uint32): the receiver consumed that many bytes.
	KindCredit byte = 1
	// KindClose ends a channel. The core sends a JSON reason; a viewer sends none.
	KindClose byte = 2
	// KindResize is uint16 cols, uint16 rows. Viewer to core, and core to an
	// observer or to a viewer that asked for resize notifications.
	KindResize byte = 3
	// KindFocus (viewer to core): 1 gained focus, 0 lost it.
	KindFocus byte = 4
	// KindFacts (viewer to core): JSON display facts.
	KindFacts byte = 5
	// KindAutomaticInput (viewer to core): terminal replies, not human input.
	KindAutomaticInput byte = 6
)

// Frame is one decoded frame.
type Frame struct {
	Channel uint32
	Kind    byte
	Payload []byte
}

// ErrFrameTooLong is the error for a frame that is longer than MaxPayload.
var ErrFrameTooLong = errors.New("hc: frame too long")

// AppendFrame appends the encoding of a frame to dst.
func AppendFrame(dst []byte, channel uint32, kind byte, payload []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, channel)
	dst = append(dst, kind)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(payload)))
	return append(dst, payload...)
}

// ReadFrame reads one frame. It returns io.EOF when the input ends between
// frames, and io.ErrUnexpectedEOF when it ends inside one. A frame longer than
// MaxPayload is an error before any payload is read or allocated.
func ReadFrame(r io.Reader) (Frame, error) {
	var h [HeaderLen]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err // io.EOF on a clean end, ErrUnexpectedEOF in a header
	}
	n := binary.LittleEndian.Uint32(h[5:])
	if n > MaxPayload {
		return Frame{}, fmt.Errorf("%w: %d bytes, limit %d", ErrFrameTooLong, n, MaxPayload)
	}
	f := Frame{Channel: binary.LittleEndian.Uint32(h[:4]), Kind: h[4], Payload: make([]byte, n)}
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return f, nil
}
