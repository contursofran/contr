package hc

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	b := AppendFrame(nil, 7, KindData, []byte("hi"))
	want := []byte{7, 0, 0, 0, 0, 2, 0, 0, 0, 'h', 'i'}
	if !bytes.Equal(b, want) {
		t.Fatalf("encoding %v, want %v", b, want)
	}
	f, err := ReadFrame(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if f.Channel != 7 || f.Kind != KindData || string(f.Payload) != "hi" {
		t.Fatalf("frame %+v", f)
	}
}

func TestFrameEmptyPayloadAndEOF(t *testing.T) {
	r := bytes.NewReader(AppendFrame(nil, 1, KindClose, nil))
	f, err := ReadFrame(r)
	if err != nil || f.Kind != KindClose || len(f.Payload) != 0 {
		t.Fatalf("frame %+v err %v", f, err)
	}
	if _, err := ReadFrame(r); err != io.EOF {
		t.Fatalf("clean end: got %v, want EOF", err)
	}
}

func TestFrameTooLong(t *testing.T) {
	h := AppendFrame(nil, 1, KindData, nil)
	h[5], h[6], h[7], h[8] = 0xff, 0xff, 0xff, 0xff
	// Only the header is there. A reader that tried to read the payload
	// would report a truncated frame, not a long one.
	_, err := ReadFrame(bytes.NewReader(h))
	if !errors.Is(err, ErrFrameTooLong) {
		t.Fatalf("got %v, want ErrFrameTooLong", err)
	}
}

func TestFrameTruncated(t *testing.T) {
	b := AppendFrame(nil, 1, KindData, []byte("abcd"))
	for _, n := range []int{3, HeaderLen, HeaderLen + 2} {
		if _, err := ReadFrame(bytes.NewReader(b[:n])); err != io.ErrUnexpectedEOF {
			t.Errorf("cut at %d: got %v, want ErrUnexpectedEOF", n, err)
		}
	}
}
