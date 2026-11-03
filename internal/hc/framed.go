package hc

// Written from herdr-core's wire layout; see NOTICE.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// KindIncompatible is the Error kind of a core that cannot do framed attach.
const KindIncompatible = "incompatible"

func incompatible(format string, args ...any) error {
	return &Error{Kind: KindIncompatible, Message: fmt.Sprintf(format, args...)}
}

// FramedClient is a connection in framed mode. JSON-RPC calls go on channel
// 0; each attached terminal has a channel of its own (a Stream).
type FramedClient struct {
	pipe  Pipe
	Hello Hello

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]*framedCall
	streams map[uint32]*Stream
	failed  error // set once the connection is over

	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type reply struct {
	m   *message
	err error
}

type framedCall struct {
	ch chan reply
	// hook runs on the reader goroutine with the result, before the next
	// frame is read, so that a stream exists before its first frame comes.
	hook func(json.RawMessage) error
}

// DialFramed opens a connection and switches it to framed mode: it sends
// hello with framed set, and checks that the core agreed and can attach.
// A core that cannot gives an error for which IsKind(err, "incompatible")
// is true.
func DialFramed(ctx context.Context, dial Dialer) (*FramedClient, error) {
	pipe, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(pipe.R)
	h, err := framedHello(ctx, pipe, br)
	if err != nil {
		if pipe.Close != nil {
			_ = pipe.Close()
		}
		return nil, err
	}
	cl := &FramedClient{pipe: pipe, Hello: h, pending: map[uint64]*framedCall{}, streams: map[uint32]*Stream{}, done: make(chan struct{})}
	go cl.read(br)
	return cl, nil
}

// framedHello sends hello as a JSON line and reads the reply line. The
// frames start after it.
func framedHello(ctx context.Context, pipe Pipe, br *bufio.Reader) (Hello, error) {
	type result struct {
		h   Hello
		err error
	}
	out := make(chan result, 1)
	go func() {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"hello","params":{"generation":%d,"framed":true}}`+"\n", Generation)
		if _, err := io.WriteString(pipe.W, req); err != nil {
			out <- result{err: fmt.Errorf("hc: send hello: %w", err)}
			return
		}
		line, err := br.ReadBytes('\n')
		if err != nil {
			if pipe.Stderr != nil {
				if msg := pipe.Stderr(); msg != "" {
					err = fmt.Errorf("%s: %w", msg, err)
				}
			}
			out <- result{err: fmt.Errorf("hc: read hello: %w", err)}
			return
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			out <- result{err: fmt.Errorf("hc: bad hello reply: %w", err)}
			return
		}
		if err := m.err(); err != nil {
			out <- result{err: err}
			return
		}
		var r struct {
			Hello
			Framed bool `json:"framed"`
		}
		if err := json.Unmarshal(m.Result, &r); err != nil {
			out <- result{err: fmt.Errorf("hc: hello result: %w", err)}
			return
		}
		switch {
		case r.Generation != Generation:
			out <- result{err: incompatible("core speaks protocol generation %d, want %d", r.Generation, Generation)}
		case !r.Framed:
			out <- result{err: incompatible("core did not switch to framed mode")}
		case !slices.Contains(r.Features, "attach"):
			out <- result{err: incompatible("core has no attach feature")}
		default:
			out <- result{h: r.Hello}
		}
	}()
	select {
	case r := <-out:
		return r.h, r.err
	case <-ctx.Done():
		if pipe.Close != nil {
			_ = pipe.Close() // ends the goroutine
		}
		return Hello{}, ctx.Err()
	}
}

func (cl *FramedClient) send(channel uint32, kind byte, payload []byte) error {
	buf := AppendFrame(make([]byte, 0, HeaderLen+len(payload)), channel, kind, payload)
	cl.writeMu.Lock()
	defer cl.writeMu.Unlock()
	if _, err := cl.pipe.W.Write(buf); err != nil {
		return fmt.Errorf("hc: send frame: %w", err)
	}
	return nil
}

func (cl *FramedClient) read(r io.Reader) {
	var err error
	for {
		var f Frame
		if f, err = ReadFrame(r); err != nil {
			break
		}
		if f.Channel == 0 {
			if f.Kind == KindData {
				if err = cl.reply(f.Payload); err != nil {
					break
				}
			}
			continue
		}
		cl.mu.Lock()
		s := cl.streams[f.Channel]
		cl.mu.Unlock()
		if s != nil {
			s.frame(f)
		}
	}
	if err == io.EOF {
		err = ErrClosed
	} else if cl.pipe.Stderr != nil {
		if msg := cl.pipe.Stderr(); msg != "" {
			err = fmt.Errorf("hc: connection ended: %s: %w", msg, err)
		}
	}
	cl.fail(err)
}

// reply handles the JSON-RPC messages of one channel 0 payload.
func (cl *FramedClient) reply(p []byte) error {
	dec := json.NewDecoder(bytes.NewReader(p))
	for {
		var m message
		if err := dec.Decode(&m); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("hc: bad message: %w", err)
		}
		if m.ID == nil {
			continue
		}
		cl.mu.Lock()
		c := cl.pending[*m.ID]
		delete(cl.pending, *m.ID)
		cl.mu.Unlock()
		if c == nil {
			continue
		}
		var hookErr error
		if c.hook != nil && m.Error == nil {
			hookErr = c.hook(m.Result)
		}
		c.ch <- reply{&m, hookErr}
	}
}

// fail ends the connection's calls and streams. The first error is kept.
func (cl *FramedClient) fail(err error) {
	cl.mu.Lock()
	if cl.failed == nil {
		cl.failed = err
	}
	err = cl.failed
	pending, streams := cl.pending, cl.streams
	cl.pending, cl.streams = map[uint64]*framedCall{}, map[uint32]*Stream{}
	cl.mu.Unlock()
	for _, c := range pending {
		close(c.ch)
	}
	for _, s := range streams {
		s.abort(err)
	}
	close(cl.done)
}

// Call sends method with params on channel 0 and decodes the result into
// result (which can be nil). An error reply of the core is an *Error.
func (cl *FramedClient) Call(ctx context.Context, method string, params, result any) error {
	return cl.call(ctx, method, params, result, nil)
}

func (cl *FramedClient) call(ctx context.Context, method string, params, result any, hook func(json.RawMessage) error) error {
	cl.mu.Lock()
	if cl.failed != nil {
		cl.mu.Unlock()
		return ErrClosed
	}
	cl.nextID++
	id := cl.nextID
	c := &framedCall{ch: make(chan reply, 1), hook: hook}
	cl.pending[id] = c
	cl.mu.Unlock()

	body, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err == nil {
		err = cl.send(0, KindData, body)
	}
	if err != nil {
		cl.mu.Lock()
		delete(cl.pending, id)
		cl.mu.Unlock()
		return fmt.Errorf("hc: send %s: %w", method, err)
	}
	select {
	case r, ok := <-c.ch:
		if !ok {
			return ErrClosed
		}
		m := r.m
		if r.err != nil {
			return r.err
		}
		if err := m.err(); err != nil {
			return err
		}
		if result == nil || len(m.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(m.Result, result); err != nil {
			return fmt.Errorf("hc: %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		// A call with a hook keeps its entry: the reply can still come,
		// and the hook must see it (Attach closes the stream then).
		if hook == nil {
			cl.mu.Lock()
			delete(cl.pending, id)
			cl.mu.Unlock()
		}
		return ctx.Err()
	}
}

// Close ends the connection and every stream on it. A second Close returns
// the result of the first.
func (cl *FramedClient) Close() error {
	cl.closeOnce.Do(func() {
		if cl.pipe.Close != nil {
			cl.closeErr = cl.pipe.Close()
		}
		<-cl.done
	})
	return cl.closeErr
}

// Display is what a viewer tells the core about its screen.
type Display struct {
	FG     *string `json:"fg"`     // "#rrggbb" or nil
	BG     *string `json:"bg"`     // "#rrggbb" or nil
	Cursor *string `json:"cursor"` // "#rrggbb" or nil
	Cell   *[2]int `json:"cell"`   // cell size in pixels: width, height
}

// AttachParams are the parameters of terminal.attach.
type AttachParams struct {
	Terminal            string   `json:"terminal"`
	Cols                uint16   `json:"cols"`
	Rows                uint16   `json:"rows"`
	Display             *Display `json:"display,omitempty"`
	History             uint     `json:"history,omitempty"` // scrollback lines to show first
	Observe             bool     `json:"observe,omitempty"` // watch only: no input, no size
	ResizeNotifications bool     `json:"resize_notifications,omitempty"`
	Resumed             bool     `json:"resumed,omitempty"`
	Caller              *string  `json:"caller,omitempty"`
}

// Attach attaches to a terminal. The first events on the stream are the
// first paint, then live output. An error reply is an *Error; the kinds are
// "not_found", "machine_unknown", "finished", "unavailable" and
// "needs_framed".
func (cl *FramedClient) Attach(ctx context.Context, p AttachParams) (*Stream, error) {
	var res struct {
		Channel  uint32          `json:"channel"`
		Terminal json.RawMessage `json:"terminal"`
	}
	var (
		mu        sync.Mutex
		s         *Stream
		abandoned bool
	)
	err := cl.call(ctx, "terminal.attach", p, &res, func(raw json.RawMessage) error {
		var r struct {
			Channel uint32 `json:"channel"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		if r.Channel == 0 {
			return errors.New("attach reply has no channel")
		}
		mu.Lock()
		defer mu.Unlock()
		if abandoned {
			// The caller is gone; the core has attached anyway.
			_ = cl.send(r.Channel, KindClose, nil)
			return ctx.Err()
		}
		s = newStream(cl, r.Channel)
		cl.mu.Lock()
		defer cl.mu.Unlock()
		if cl.failed != nil {
			return ErrClosed
		}
		cl.streams[r.Channel] = s
		return nil
	})
	if err != nil {
		mu.Lock()
		abandoned = true
		st := s
		mu.Unlock()
		if st != nil {
			_ = st.Close() // the hook ran: the core has a live stream
		}
		return nil, err
	}
	s.Terminal = res.Terminal
	return s, nil
}

// Size is a grid size in cells.
type Size struct{ Cols, Rows uint16 }

// StreamEvent is one item of a stream. Data is terminal output. Resize is set
// when the core changes the grid size; it comes in order with the data.
type StreamEvent struct {
	Data   []byte
	Resize *Size
}

// StreamEnd is the Err of a stream that the core ended, for example because
// the program finished.
type StreamEnd struct{ Reason string }

func (e *StreamEnd) Error() string {
	if e.Reason == "" {
		return "hc: stream ended"
	}
	return "hc: stream ended: " + e.Reason
}

// Stream is one attached terminal.
type Stream struct {
	cl      *FramedClient
	channel uint32
	// Terminal is the terminal record of the attach reply, as JSON.
	Terminal json.RawMessage

	events chan StreamEvent
	done   chan struct{}
	stop   chan struct{} // closed to end the pump at once
	wake   chan struct{}

	mu          sync.Mutex
	queue       []StreamEvent
	outstanding int // bytes received and not yet acknowledged
	ended       bool
	err         error
	stopOnce    sync.Once
}

func newStream(cl *FramedClient, channel uint32) *Stream {
	s := &Stream{cl: cl, channel: channel, events: make(chan StreamEvent), done: make(chan struct{}), stop: make(chan struct{}), wake: make(chan struct{}, 1)}
	go s.pump()
	return s
}

// Events delivers the output and the size changes of the terminal, in
// order. It closes when the stream is over, after the output that the core
// sent before it ended. The consumer must read it: the core stops sending
// when it has Window bytes that are not acknowledged.
func (s *Stream) Events() <-chan StreamEvent { return s.events }

// Done closes when the stream is over and Events has closed.
func (s *Stream) Done() <-chan struct{} { return s.done }

// Err tells why the stream ended; call it after Done closes. It is nil after
// Close, a *StreamEnd when the core ended the stream, and another error when
// the connection or the protocol failed.
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// frame handles one frame of the stream on the reader goroutine.
func (s *Stream) frame(f Frame) {
	switch f.Kind {
	case KindData:
		s.mu.Lock()
		if s.ended {
			s.mu.Unlock()
			return
		}
		s.outstanding += len(f.Payload)
		if s.outstanding > Window {
			s.mu.Unlock()
			s.abort(fmt.Errorf("hc: stream sent more than the credit window of %d bytes", Window))
			return
		}
		s.queue = append(s.queue, StreamEvent{Data: f.Payload})
		s.mu.Unlock()
		s.signal()
	case KindResize:
		if len(f.Payload) != 4 {
			s.abort(fmt.Errorf("hc: resize frame of %d bytes, want 4", len(f.Payload)))
			return
		}
		sz := Size{binary.LittleEndian.Uint16(f.Payload), binary.LittleEndian.Uint16(f.Payload[2:])}
		s.mu.Lock()
		if !s.ended {
			s.queue = append(s.queue, StreamEvent{Resize: &sz})
		}
		s.mu.Unlock()
		s.signal()
	case KindClose:
		var r struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(f.Payload, &r)
		s.mu.Lock()
		if !s.ended {
			s.ended = true
			s.err = &StreamEnd{Reason: r.Reason}
		}
		s.mu.Unlock()
		s.unregister()
		s.signal() // the pump delivers what is queued, then ends
	}
}

func (s *Stream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Stream) unregister() {
	s.cl.mu.Lock()
	if s.cl.streams[s.channel] == s {
		delete(s.cl.streams, s.channel)
	}
	s.cl.mu.Unlock()
}

// abort ends the stream at once and drops what is queued.
func (s *Stream) abort(err error) {
	s.mu.Lock()
	if !s.ended {
		s.ended = true
		s.err = err
	}
	s.queue = nil
	s.mu.Unlock()
	s.unregister()
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Stream) pump() {
	defer func() {
		close(s.events)
		close(s.done)
	}()
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			ended := s.ended
			s.mu.Unlock()
			if ended {
				return
			}
			select {
			case <-s.wake:
			case <-s.stop:
				return
			}
			continue
		}
		ev := s.queue[0]
		s.queue[0] = StreamEvent{}
		s.queue = s.queue[1:]
		s.mu.Unlock()
		select {
		case s.events <- ev:
		case <-s.stop:
			return
		}
		if n := len(ev.Data); n > 0 {
			s.mu.Lock()
			s.outstanding -= n
			live := !s.ended
			s.mu.Unlock()
			if live {
				var b [4]byte
				binary.LittleEndian.PutUint32(b[:], uint32(n))
				_ = s.cl.send(s.channel, KindCredit, b[:])
			}
		}
	}
}

// Write sends bytes to the terminal as input.
func (s *Stream) Write(p []byte) (int, error) {
	if err := s.live(); err != nil {
		return 0, err
	}
	n := 0
	for len(p) > 0 {
		c := min(len(p), writeChunk)
		if err := s.cl.send(s.channel, KindData, p[:c]); err != nil {
			return n, err
		}
		n += c
		p = p[c:]
	}
	return n, nil
}

// Resize tells the core the size of the viewer's grid.
func (s *Stream) Resize(cols, rows uint16) error {
	if err := s.live(); err != nil {
		return err
	}
	var b [4]byte
	binary.LittleEndian.PutUint16(b[:], cols)
	binary.LittleEndian.PutUint16(b[2:], rows)
	return s.cl.send(s.channel, KindResize, b[:])
}

// Focus tells the core that the viewer gained or lost focus.
func (s *Stream) Focus(focused bool) error {
	if err := s.live(); err != nil {
		return err
	}
	b := byte(0)
	if focused {
		b = 1
	}
	return s.cl.send(s.channel, KindFocus, []byte{b})
}

// SetDisplay sends new display facts (colors, cell size).
func (s *Stream) SetDisplay(d Display) error {
	if err := s.live(); err != nil {
		return err
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.cl.send(s.channel, KindFacts, body)
}

func (s *Stream) live() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return ErrClosed
	}
	return nil
}

// Close detaches from the terminal. The terminal keeps running. A second
// Close does nothing.
func (s *Stream) Close() error {
	s.mu.Lock()
	was := s.ended
	s.mu.Unlock()
	if !was {
		_ = s.cl.send(s.channel, KindClose, nil)
	}
	s.abort(nil)
	<-s.done
	return nil
}
