package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeFramedCore is the core side of a framed connection, in memory.
type fakeFramedCore struct {
	t     *testing.T
	toCl  *io.PipeWriter // core to client
	clR   *io.PipeReader // client to core
	hello string         // result of hello
	// attach returns the result JSON, or an error object JSON when the
	// second value is set.
	attach func(params map[string]any) (result, errObj string)

	mu    sync.Mutex
	wmu   sync.Mutex
	hellq chan map[string]any
	got   chan Frame // frames of the client that are not channel 0 JSON
}

const goodHello = `{"generation":1,"framed":true,"features":["framed","attach","events"],"methods":[]}`

func newFake(t *testing.T, hello string) (*fakeFramedCore, Dialer) {
	t.Helper()
	f := &fakeFramedCore{t: t, hello: hello, hellq: make(chan map[string]any, 1), got: make(chan Frame, 64)}
	f.attach = func(map[string]any) (string, string) { return `{"channel":1,"terminal":{"id":"t1"}}`, "" }
	dial := func(ctx context.Context) (Pipe, error) {
		cr, cw := io.Pipe() // client reads, core writes
		sr, sw := io.Pipe() // core reads, client writes
		f.toCl, f.clR = cw, sr
		go f.serve()
		return Pipe{W: sw, R: cr, Close: func() error { _ = sw.Close(); _ = cw.Close(); _ = cr.Close(); return nil }}, nil
	}
	return f, dial
}

func (f *fakeFramedCore) write(b []byte) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = f.toCl.Write(b)
}

func (f *fakeFramedCore) send(channel uint32, kind byte, payload []byte) {
	f.write(AppendFrame(nil, channel, kind, payload))
}

func (f *fakeFramedCore) serve() {
	br := bufio.NewReader(f.clR)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal(line, &req)
	f.hellq <- req.Params
	f.write([]byte(`{"jsonrpc":"2.0","id":0,"result":` + f.hello + "}\n"))
	for {
		fr, err := ReadFrame(br)
		if err != nil {
			return
		}
		if fr.Channel != 0 || fr.Kind != KindData {
			f.got <- fr
			continue
		}
		var m struct {
			ID     uint64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(fr.Payload, &m)
		res, errObj := f.attach(m.Params)
		body := `{"jsonrpc":"2.0","id":` + itoa(m.ID) + `,"result":` + res + `}`
		if errObj != "" {
			body = `{"jsonrpc":"2.0","id":` + itoa(m.ID) + `,"error":` + errObj + `}`
		}
		f.send(0, KindData, []byte(body+"\n"))
	}
}

func itoa(n uint64) string { b, _ := json.Marshal(n); return string(b) }

// next returns the next frame that the client sent on a stream.
func (f *fakeFramedCore) next() Frame {
	f.t.Helper()
	select {
	case fr := <-f.got:
		return fr
	case <-time.After(5 * time.Second):
		f.t.Fatal("no frame from the client")
		return Frame{}
	}
}

func attached(t *testing.T, hello string) (*fakeFramedCore, *FramedClient, *Stream) {
	t.Helper()
	f, dial := newFake(t, hello)
	cl, err := DialFramed(context.Background(), dial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	s, err := cl.Attach(context.Background(), AttachParams{Terminal: "t1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	return f, cl, s
}

func recvEv(t *testing.T, s *Stream) StreamEvent {
	t.Helper()
	select {
	case ev, ok := <-s.Events():
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
		return StreamEvent{}
	}
}

func waitDone(t *testing.T, s *Stream) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stream not done")
	}
}
