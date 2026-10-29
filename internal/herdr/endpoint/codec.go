// Package endpoint speaks herdr's binary endpoint socket.
//
// This file is the codec: bincode 2 "standard" rules (varint integers, little
// endian) and the [u32 LE length][payload] frame.
// Written from herdr's wire layout; see NOTICE.
package endpoint

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// CodecKind tells which rule a payload broke.
type CodecKind int

const (
	// Truncated: the payload ended before the field was complete.
	Truncated CodecKind = iota + 1
	// BadVarint: a varint marker other than 0..=253. Value holds the marker.
	BadVarint
	// BadBool: a bool byte other than 0 or 1. Value holds the byte.
	BadBool
	// BadUTF8: a string that is not UTF-8.
	BadUTF8
	// TooLong: a length larger than the bytes left, or a value that does not
	// fit its integer width (Vu16, Vu32). Value holds the number.
	TooLong
	// BadTag: an enum or option tag this codec does not know. What names the
	// type and Value holds the tag.
	BadTag
	// Invalid: every field parsed, but the values break a rule of the layout.
	// What names the rule.
	Invalid
)

// CodecError is a payload that does not match the layout.
type CodecError struct {
	Kind  CodecKind
	Value uint64
	What  string
}

// NewInvalid returns an Invalid error that names the broken rule.
func NewInvalid(rule string) *CodecError {
	return &CodecError{Kind: Invalid, What: rule}
}

// NewBadTag returns a BadTag error for the named type.
func NewBadTag(what string, tag uint32) *CodecError {
	return &CodecError{Kind: BadTag, What: what, Value: uint64(tag)}
}

func (e *CodecError) Error() string {
	switch e.Kind {
	case Truncated:
		return "endpoint payload ended early"
	case BadVarint:
		return fmt.Sprintf("bad varint marker %d", e.Value)
	case BadBool:
		return fmt.Sprintf("bad bool byte %d", e.Value)
	case BadUTF8:
		return "endpoint string is not UTF-8"
	case TooLong:
		return fmt.Sprintf("endpoint length or value %d does not fit", e.Value)
	case BadTag:
		return fmt.Sprintf("unknown %s tag %d", e.What, e.Value)
	case Invalid:
		return "endpoint payload is invalid: " + e.What
	}
	return "endpoint codec error"
}

// ErrFrameTooLarge marks a frame over MaxFrame, on read or write.
var ErrFrameTooLarge = errors.New("endpoint frame larger than MaxFrame")

// Enc writes the fields of one payload. The zero value is ready to use.
type Enc struct {
	Buf []byte
}

// U8 writes one byte.
func (e *Enc) U8(v uint8) { e.Buf = append(e.Buf, v) }

// Bool writes 0 or 1.
func (e *Enc) Bool(v bool) {
	if v {
		e.Buf = append(e.Buf, 1)
	} else {
		e.Buf = append(e.Buf, 0)
	}
}

// Vu writes a varint unsigned: values up to 250 in one byte, else a marker
// (251, 252 or 253) and the value as u16, u32 or u64, little endian.
func (e *Enc) Vu(v uint64) {
	switch {
	case v <= 250:
		e.Buf = append(e.Buf, byte(v))
	case v <= 0xFFFF:
		e.Buf = binary.LittleEndian.AppendUint16(append(e.Buf, 251), uint16(v))
	case v <= 0xFFFFFFFF:
		e.Buf = binary.LittleEndian.AppendUint32(append(e.Buf, 252), uint32(v))
	default:
		e.Buf = binary.LittleEndian.AppendUint64(append(e.Buf, 253), v)
	}
}

// Str writes a varint length, then the bytes of s.
func (e *Enc) Str(s string) {
	e.Vu(uint64(len(s)))
	e.Buf = append(e.Buf, s...)
}

// Char writes a rune as its UTF-8 bytes, with no length prefix.
func (e *Enc) Char(c rune) { e.Buf = utf8.AppendRune(e.Buf, c) }

// None writes option tag 0.
func (e *Enc) None() { e.Buf = append(e.Buf, 0) }

// Some writes option tag 1; the value follows.
func (e *Enc) Some() { e.Buf = append(e.Buf, 1) }

// Dec reads the fields of one payload. Every read is bounds-checked.
type Dec struct {
	buf []byte
	pos int
}

// NewDec returns a decoder over buf.
func NewDec(buf []byte) *Dec { return &Dec{buf: buf} }

func (d *Dec) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.buf)-d.pos {
		return nil, &CodecError{Kind: Truncated}
	}
	b := d.buf[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

// U8 reads one byte.
func (d *Dec) U8() (uint8, error) {
	b, err := d.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// Bool reads a byte that must be 0 or 1.
func (d *Dec) Bool() (bool, error) {
	b, err := d.U8()
	if err != nil {
		return false, err
	}
	switch b {
	case 0:
		return false, nil
	case 1:
		return true, nil
	}
	return false, &CodecError{Kind: BadBool, Value: uint64(b)}
}

// Vu reads a varint unsigned.
func (d *Dec) Vu() (uint64, error) {
	m, err := d.U8()
	if err != nil {
		return 0, err
	}
	size := 0
	switch {
	case m <= 250:
		return uint64(m), nil
	case m == 251:
		size = 2
	case m == 252:
		size = 4
	case m == 253:
		size = 8
	default:
		return 0, &CodecError{Kind: BadVarint, Value: uint64(m)}
	}
	b, err := d.take(size)
	if err != nil {
		return 0, err
	}
	switch size {
	case 2:
		return uint64(binary.LittleEndian.Uint16(b)), nil
	case 4:
		return uint64(binary.LittleEndian.Uint32(b)), nil
	}
	return binary.LittleEndian.Uint64(b), nil
}

// Vu16 reads a varint that must fit in 16 bits.
func (d *Dec) Vu16() (uint16, error) {
	v, err := d.Vu()
	if err != nil {
		return 0, err
	}
	if v > 0xFFFF {
		return 0, &CodecError{Kind: TooLong, Value: v}
	}
	return uint16(v), nil
}

// Vu32 reads a varint that must fit in 32 bits.
func (d *Dec) Vu32() (uint32, error) {
	v, err := d.Vu()
	if err != nil {
		return 0, err
	}
	if v > 0xFFFFFFFF {
		return 0, &CodecError{Kind: TooLong, Value: v}
	}
	return uint32(v), nil
}

// Len reads a varint length. It rejects a length larger than the bytes left:
// every element takes at least one byte, so a bad length never allocates.
func (d *Dec) Len() (int, error) {
	n, err := d.Vu()
	if err != nil {
		return 0, err
	}
	if n > uint64(len(d.buf)-d.pos) {
		return 0, &CodecError{Kind: TooLong, Value: n}
	}
	return int(n), nil
}

// Str reads a varint length, then that many bytes that must be UTF-8.
func (d *Dec) Str() (string, error) {
	b, err := d.Bytes()
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", &CodecError{Kind: BadUTF8}
	}
	return string(b), nil
}

// Bytes reads a varint length, then that many raw bytes, with no UTF-8 check.
// The slice shares the decoder's buffer.
func (d *Dec) Bytes() ([]byte, error) {
	n, err := d.Len()
	if err != nil {
		return nil, err
	}
	return d.take(n)
}

// SkipBytes skips a varint length and that many bytes.
func (d *Dec) SkipBytes() error {
	_, err := d.Bytes()
	return err
}

// Option reads an option tag. false is None; the value follows only for true.
func (d *Dec) Option() (bool, error) {
	b, err := d.U8()
	if err != nil {
		return false, err
	}
	switch b {
	case 0:
		return false, nil
	case 1:
		return true, nil
	}
	return false, NewBadTag("option", uint32(b))
}

// WriteFrame writes [u32 LE len][payload] in one Write call. It refuses a
// payload over MaxFrame.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrame {
		return ErrFrameTooLarge
	}
	frame := binary.LittleEndian.AppendUint32(make([]byte, 0, 4+len(payload)), uint32(len(payload)))
	frame = append(frame, payload...)
	_, err := w.Write(frame)
	return err
}

// ReadFrame reads one frame. It returns io.EOF on a clean EOF before the
// length. EOF inside the header or the body is io.ErrUnexpectedEOF. A length
// over MaxFrame is ErrFrameTooLarge, and nothing is allocated for it.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(header[:])
	if n > MaxFrame {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return payload, nil
}
