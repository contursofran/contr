package hc

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestHelloFramedParams(t *testing.T) {
	f, dial := newFake(t, goodHello)
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	p := <-f.hellq
	if p["framed"] != true || p["generation"] != float64(1) {
		t.Fatalf("hello params %v", p)
	}
}

func TestHelloFramedRefused(t *testing.T) {
	for name, hello := range map[string]string{
		"not framed": `{"generation":1,"framed":false,"features":["attach"]}`,
		"no attach":  `{"generation":1,"framed":true,"features":["framed"]}`,
		"generation": `{"generation":2,"framed":true,"features":["attach"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, dial := newFake(t, hello)
			_, err := DialFramed(context.Background(), dial)
			if !IsKind(err, "incompatible") {
				t.Fatalf("got %v, want kind incompatible", err)
			}
		})
	}
}

func TestAttachNeedsFramed(t *testing.T) {
	f, dial := newFake(t, goodHello)
	f.attach = func(map[string]any) (string, string) {
		return "", `{"code":-32000,"message":"framed only","data":{"kind":"needs_framed"}}`
	}
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	_, err = cl.Attach(context.Background(), AttachParams{Terminal: "t1", Cols: 80, Rows: 24})
	if !IsKind(err, "needs_framed") {
		t.Fatalf("got %v, want kind needs_framed", err)
	}
}

func TestAttachErrors(t *testing.T) {
	for _, kind := range []string{"not_found", "finished", "unavailable", "machine_unknown"} {
		t.Run(kind, func(t *testing.T) {
			f, dial := newFake(t, goodHello)
			f.attach = func(map[string]any) (string, string) {
				return "", `{"code":-32000,"message":"no","data":{"kind":"` + kind + `"}}`
			}
			cl, err := DialFramed(context.Background(), dial)
			if err != nil {
				t.Fatal(err)
			}
			defer cl.Close()
			_, err = cl.Attach(context.Background(), AttachParams{Terminal: "t1", Cols: 80, Rows: 24})
			if !IsKind(err, kind) {
				t.Fatalf("got %v, want kind %s", err, kind)
			}
		})
	}
}

func TestAttachParamsSent(t *testing.T) {
	f, dial := newFake(t, goodHello)
	var got map[string]any
	f.attach = func(p map[string]any) (string, string) {
		got = p
		return `{"channel":3}`, ""
	}
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err := cl.Attach(context.Background(), AttachParams{Terminal: "t1", Cols: 100, Rows: 30, History: 1000, Resumed: true}); err != nil {
		t.Fatal(err)
	}
	if got["terminal"] != "t1" || got["cols"] != float64(100) || got["rows"] != float64(30) || got["history"] != float64(1000) || got["resumed"] != true {
		t.Fatalf("params %v", got)
	}
}

func TestAttachNoChannel(t *testing.T) {
	f, dial := newFake(t, goodHello)
	f.attach = func(map[string]any) (string, string) { return `{"channel":0}`, "" }
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err := cl.Attach(context.Background(), AttachParams{Terminal: "t1", Cols: 80, Rows: 24}); err == nil {
		t.Fatal("want an error for a reply without a channel")
	}
}

func TestStreamEventsCreditAndInput(t *testing.T) {
	f, _, s := attached(t, goodHello)
	// A size change comes before the paint, in order.
	f.send(1, KindResize, []byte{90, 0, 30, 0})
	f.send(1, KindData, []byte("hello"))
	if ev := recvEv(t, s); ev.Resize == nil || *ev.Resize != (Size{90, 30}) {
		t.Fatalf("first event %+v, want resize 90x30", ev)
	}
	if ev := recvEv(t, s); string(ev.Data) != "hello" {
		t.Fatalf("second event %+v, want data", ev)
	}
	// Credit goes back for the bytes that were consumed.
	if fr := f.next(); fr.Channel != 1 || fr.Kind != KindCredit || binary.LittleEndian.Uint32(fr.Payload) != 5 {
		t.Fatalf("credit frame %+v", fr)
	}
	if _, err := s.Write([]byte("echo hi\r")); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Kind != KindData || string(fr.Payload) != "echo hi\r" {
		t.Fatalf("input frame %+v", fr)
	}
	if err := s.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Kind != KindResize || len(fr.Payload) != 4 || binary.LittleEndian.Uint16(fr.Payload) != 100 || binary.LittleEndian.Uint16(fr.Payload[2:]) != 40 {
		t.Fatalf("resize frame %+v", fr)
	}
	if err := s.Focus(true); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Kind != KindFocus || len(fr.Payload) != 1 || fr.Payload[0] != 1 {
		t.Fatalf("focus frame %+v", fr)
	}
	if err := s.Focus(false); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Kind != KindFocus || fr.Payload[0] != 0 {
		t.Fatalf("focus frame %+v", fr)
	}
	if err := s.SetDisplay(Display{Cell: &[2]int{9, 18}}); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Kind != KindFacts || !strings.Contains(string(fr.Payload), `"cell":[9,18]`) {
		t.Fatalf("facts frame %+v", fr)
	}
}

func TestStreamWriteChunks(t *testing.T) {
	f, _, s := attached(t, goodHello)
	big := make([]byte, writeChunk*2+1)
	if _, err := s.Write(big); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{writeChunk, writeChunk, 1} {
		if fr := f.next(); fr.Kind != KindData || len(fr.Payload) != want {
			t.Fatalf("chunk of %d bytes, want %d", len(fr.Payload), want)
		}
	}
}

func TestStreamIgnoresOtherChannels(t *testing.T) {
	f, _, s := attached(t, goodHello)
	f.send(9, KindData, []byte("other"))
	f.send(1, KindData, []byte("mine"))
	if ev := recvEv(t, s); string(ev.Data) != "mine" {
		t.Fatalf("event %+v", ev)
	}
}

func TestCreditBackpressure(t *testing.T) {
	f, _, s := attached(t, goodHello)
	// Nobody reads Events: the first frame stays unacknowledged, so one
	// more byte is past the window.
	f.send(1, KindData, make([]byte, Window))
	f.send(1, KindData, []byte("x"))
	waitDone(t, s)
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "credit window") {
		t.Fatalf("err %v, want the credit window error", err)
	}
}

func TestStreamCloseOnLoss(t *testing.T) {
	f, _, s := attached(t, goodHello)
	f.send(1, KindData, []byte("last screen"))
	f.send(1, KindClose, []byte(`{"reason":"exited"}`))
	// The output that came before the end is delivered first.
	if ev := recvEv(t, s); string(ev.Data) != "last screen" {
		t.Fatalf("event %+v", ev)
	}
	waitDone(t, s)
	var end *StreamEnd
	if err := s.Err(); !errors.As(err, &end) || end.Reason != "exited" {
		t.Fatalf("err %v, want StreamEnd exited", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after end: %v", err)
	}
}

func TestStreamConnectionLost(t *testing.T) {
	f, cl, s := attached(t, goodHello)
	_ = f.toCl.Close()
	waitDone(t, s)
	if err := s.Err(); !errors.Is(err, ErrClosed) {
		t.Fatalf("err %v, want ErrClosed", err)
	}
	if err := cl.Call(context.Background(), "x", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after loss: %v", err)
	}
}

func TestStreamFrameTruncated(t *testing.T) {
	f, _, s := attached(t, goodHello)
	b := AppendFrame(nil, 1, KindData, []byte("abcdef"))
	f.write(b[:HeaderLen+2])
	_ = f.toCl.Close()
	waitDone(t, s)
	if err := s.Err(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err %v, want ErrUnexpectedEOF", err)
	}
}

func TestStreamCloseSendsClose(t *testing.T) {
	f, _, s := attached(t, goodHello)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if fr := f.next(); fr.Channel != 1 || fr.Kind != KindClose || len(fr.Payload) != 0 {
		t.Fatalf("close frame %+v", fr)
	}
	waitDone(t, s)
	if err := s.Err(); err != nil {
		t.Fatalf("err after Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBadResizeFrame(t *testing.T) {
	f, _, s := attached(t, goodHello)
	f.send(1, KindResize, []byte{1, 2})
	waitDone(t, s)
	if s.Err() == nil {
		t.Fatal("want an error for a short resize frame")
	}
}

// A context that ends while the attach reply is on its way must not leave a
// stream attached: the client sends CLOSE for the channel.
func TestAttachCancelledClosesStream(t *testing.T) {
	f, dial := newFake(t, goodHello)
	ctx, cancel := context.WithCancel(context.Background())
	f.attach = func(map[string]any) (string, string) {
		cancel()
		return `{"channel":7,"terminal":{"id":"t1"}}`, ""
	}
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err := cl.Attach(ctx, AttachParams{Terminal: "t1", Cols: 80, Rows: 24}); err == nil {
		t.Fatal("want the context error")
	}
	if fr := f.next(); fr.Channel != 7 || fr.Kind != KindClose {
		t.Fatalf("frame %+v, want CLOSE on channel 7", fr)
	}
	cl.mu.Lock()
	n := len(cl.streams)
	cl.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d streams left", n)
	}
}
