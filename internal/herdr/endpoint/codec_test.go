// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

func vuBytes(v uint64) []byte {
	var e Enc
	e.Vu(v)
	return e.Buf
}

func wantKind(t *testing.T, err error, kind CodecKind, value uint64) {
	t.Helper()
	var ce *CodecError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not a CodecError", err)
	}
	if ce.Kind != kind || ce.Value != value {
		t.Fatalf("got kind %d value %d, want kind %d value %d", ce.Kind, ce.Value, kind, value)
	}
}

func TestVarintGoldenBytes(t *testing.T) {
	cases := []struct {
		v    uint64
		want []byte
	}{
		{0, []byte{0}},
		{250, []byte{250}},
		{251, []byte{251, 251, 0}},
		{65535, []byte{251, 255, 255}},
		{65536, []byte{252, 0, 0, 1, 0}},
		{math.MaxUint64, []byte{253, 255, 255, 255, 255, 255, 255, 255, 255}},
	}
	for _, c := range cases {
		if got := vuBytes(c.v); !bytes.Equal(got, c.want) {
			t.Errorf("Vu(%d) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestVarintRoundTrips(t *testing.T) {
	for _, v := range []uint64{0, 1, 250, 251, 65535, 65536, math.MaxUint32, math.MaxUint64} {
		got, err := NewDec(vuBytes(v)).Vu()
		if err != nil || got != v {
			t.Errorf("round trip %d = %d, %v", v, got, err)
		}
	}
}

func TestVu16AndVu32RejectOverflow(t *testing.T) {
	_, err := NewDec(vuBytes(70000)).Vu16()
	wantKind(t, err, TooLong, 70000)
	_, err = NewDec(vuBytes(math.MaxUint32 + 1)).Vu32()
	wantKind(t, err, TooLong, math.MaxUint32+1)
	got, err := NewDec(vuBytes(70000)).Vu32()
	if err != nil || got != 70000 {
		t.Fatalf("Vu32 = %d, %v", got, err)
	}
}

func TestVarintBadMarkerIsAnError(t *testing.T) {
	_, err := NewDec([]byte{254}).Vu()
	wantKind(t, err, BadVarint, 254)
	_, err = NewDec([]byte{255}).Vu()
	wantKind(t, err, BadVarint, 255)
}

func TestStrAndCharBytes(t *testing.T) {
	var e Enc
	e.Str("é")
	if !bytes.Equal(e.Buf, []byte{2, 0xC3, 0xA9}) {
		t.Errorf("Str = %v", e.Buf)
	}
	e = Enc{}
	e.Char('é')
	if !bytes.Equal(e.Buf, []byte{0xC3, 0xA9}) {
		t.Errorf("Char = %v", e.Buf)
	}
}

func TestStrRoundTripsAndRejectsBadUTF8(t *testing.T) {
	var e Enc
	e.Str("w1:p1")
	got, err := NewDec(e.Buf).Str()
	if err != nil || got != "w1:p1" {
		t.Fatalf("Str = %q, %v", got, err)
	}
	_, err = NewDec([]byte{1, 0xFF}).Str()
	wantKind(t, err, BadUTF8, 0)
}

func TestBoolAcceptsOnlyZeroAndOne(t *testing.T) {
	if v, err := NewDec([]byte{0}).Bool(); err != nil || v {
		t.Errorf("0 = %v, %v", v, err)
	}
	if v, err := NewDec([]byte{1}).Bool(); err != nil || !v {
		t.Errorf("1 = %v, %v", v, err)
	}
	_, err := NewDec([]byte{2}).Bool()
	wantKind(t, err, BadBool, 2)
}

func TestOptionTags(t *testing.T) {
	if v, err := NewDec([]byte{0}).Option(); err != nil || v {
		t.Errorf("0 = %v, %v", v, err)
	}
	if v, err := NewDec([]byte{1}).Option(); err != nil || !v {
		t.Errorf("1 = %v, %v", v, err)
	}
	_, err := NewDec([]byte{7}).Option()
	wantKind(t, err, BadTag, 7)
	var ce *CodecError
	errors.As(err, &ce)
	if ce.What != "option" {
		t.Errorf("What = %q", ce.What)
	}
}

func TestTruncatedInputIsAnError(t *testing.T) {
	_, err := NewDec(nil).U8()
	wantKind(t, err, Truncated, 0)
	_, err = NewDec([]byte{251, 1}).Vu()
	wantKind(t, err, Truncated, 0)
	_, err = NewDec([]byte{3, 'a'}).Str()
	wantKind(t, err, TooLong, 3)
}

func TestInvalidNamesTheBrokenRule(t *testing.T) {
	got := NewInvalid("frame size").Error()
	if got != "endpoint payload is invalid: frame size" {
		t.Errorf("got %q", got)
	}
}

func TestLenRejectsALengthPastTheEnd(t *testing.T) {
	// 1000 as varint, then only 3 bytes left.
	_, err := NewDec([]byte{251, 0xE8, 0x03, 1, 2, 3}).Len()
	wantKind(t, err, TooLong, 1000)
}

func TestSkipBytesMovesPastABlob(t *testing.T) {
	d := NewDec([]byte{2, 9, 9, 7})
	if err := d.SkipBytes(); err != nil {
		t.Fatal(err)
	}
	if v, err := d.U8(); err != nil || v != 7 {
		t.Fatalf("U8 = %d, %v", v, err)
	}
}

func TestBytesReadsABlobWithoutAUTF8Check(t *testing.T) {
	d := NewDec([]byte{2, 0xFF, 0x00, 7})
	b, err := d.Bytes()
	if err != nil || !bytes.Equal(b, []byte{0xFF, 0x00}) {
		t.Fatalf("Bytes = %v, %v", b, err)
	}
	if v, err := d.U8(); err != nil || v != 7 {
		t.Fatalf("U8 = %d, %v", v, err)
	}
}

func TestBytesRejectsALengthPastTheEnd(t *testing.T) {
	_, err := NewDec([]byte{5, 1, 2}).Bytes()
	wantKind(t, err, TooLong, 5)
}

func TestFrameRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	if err := WriteFrame(&wire, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if want := []byte{5, 0, 0, 0, 'h', 'e', 'l', 'l', 'o'}; !bytes.Equal(wire.Bytes(), want) {
		t.Fatalf("wire = %v", wire.Bytes())
	}
	got, err := ReadFrame(&wire)
	if err != nil || string(got) != "hello" {
		t.Fatalf("ReadFrame = %q, %v", got, err)
	}
	if _, err := ReadFrame(&wire); err != io.EOF {
		t.Fatalf("second read err = %v, want io.EOF", err)
	}
}

func TestEmptyReaderIsACleanEOF(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader(nil)); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestShortHeaderIsUnexpectedEOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{5, 0}))
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("err = %v", err)
	}
}

func TestShortBodyIsUnexpectedEOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{5, 0, 0, 0, 'a'}))
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("err = %v", err)
	}
	// A header with no body at all is also unexpected, not clean.
	_, err = ReadFrame(bytes.NewReader([]byte{5, 0, 0, 0}))
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("headerless body err = %v", err)
	}
}

func TestOversizedHeaderIsRefusedWithoutAllocating(t *testing.T) {
	n := uint32(MaxFrame + 1)
	_, err := ReadFrame(bytes.NewReader([]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)}))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestOversizedPayloadIsRefusedOnWrite(t *testing.T) {
	var wire bytes.Buffer
	err := WriteFrame(&wire, make([]byte, MaxFrame+1))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if wire.Len() != 0 {
		t.Fatal("wrote bytes for a refused frame")
	}
}

// interrupting hands out the bytes in one-byte reads and returns an empty
// read with no error first, as a reader that was interrupted does.
type interrupting struct {
	inner io.Reader
	first bool
}

func (r *interrupting) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return 0, nil
	}
	return r.inner.Read(p[:1])
}

func TestInterruptedReadsAreRetried(t *testing.T) {
	r := &interrupting{inner: bytes.NewReader([]byte{2, 0, 0, 0, 'o', 'k'})}
	got, err := ReadFrame(r)
	if err != nil || string(got) != "ok" {
		t.Fatalf("ReadFrame = %q, %v", got, err)
	}
}
