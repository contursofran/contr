package agents

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

// redialConn is a fake connection: a fakeCore that can be closed and can
// say that it ended.
type redialConn struct {
	*fakeCore
	mu     sync.Mutex
	closed bool
	ended  atomic.Bool
}

func (c *redialConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *redialConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *redialConn) Closed() bool { return c.ended.Load() }

// count returns how many calls of method c got.
func (c *redialConn) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fakeCore.mu.Lock()
	defer c.fakeCore.mu.Unlock()
	n := 0
	for _, cl := range c.calls {
		if cl.Method == method {
			n++
		}
	}
	return n
}

// newRedialConn returns a conn whose method fails with err (nil: works).
func newRedialConn(method string, err error) *redialConn {
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){}}
	if method != "" {
		f.handle[method] = func(map[string]any) (any, error) { return nil, err }
	}
	return &redialConn{fakeCore: f}
}

// dialer hands out conns from a list (a nil conn: the dial fails with
// errNoHC) and counts dials.
type dialer struct {
	mu    sync.Mutex
	conns []*redialConn
	n     int
}

var errNoHC = errors.New("no hc")

func (d *dialer) dial(ctx context.Context) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.n
	d.n++
	if i >= len(d.conns) || d.conns[i] == nil {
		return nil, errNoHC
	}
	return d.conns[i], nil
}

func (d *dialer) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

func TestRedialDialsOnFirstUseOnly(t *testing.T) {
	a := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a}}
	r := NewRedial(d.dial)
	var got []Conn
	r.OnDial = func(c Conn) { got = append(got, c) }
	if d.dials() != 0 {
		t.Fatalf("NewRedial dialled %d times", d.dials())
	}
	for range 2 {
		if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if d.dials() != 1 || len(got) != 1 || got[0] != Conn(a) {
		t.Fatalf("dials = %d, OnDial = %v, want 1 dial and OnDial once with a", d.dials(), got)
	}
	if a.count("terminal.list") != 2 {
		t.Fatalf("a got %d calls, want 2", a.count("terminal.list"))
	}
}

func TestRedialDialErrorIsReturnedAndRetriedNextCall(t *testing.T) {
	b := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{nil, b}}
	r := NewRedial(d.dial)
	ran := 0
	r.OnDial = func(Conn) { ran++ }
	if err := r.Call(context.Background(), "terminal.list", nil, nil); !errors.Is(err, errNoHC) {
		t.Fatalf("err = %v, want %v", err, errNoHC)
	}
	if ran != 0 {
		t.Fatal("OnDial ran after a failed dial")
	}
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	if d.dials() != 2 || ran != 1 || b.count("terminal.list") != 1 {
		t.Fatalf("dials = %d, OnDial = %d, b calls = %d", d.dials(), ran, b.count("terminal.list"))
	}
}

func TestRedialSendsAgainOnlyWhenNotSent(t *testing.T) {
	a := newRedialConn("terminal.send", hc.ErrNotSent)
	b := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a, b}}
	r := NewRedial(d.dial)
	if err := r.Call(context.Background(), "terminal.send", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !a.isClosed() || d.dials() != 2 || b.count("terminal.send") != 1 {
		t.Fatalf("a closed = %v, dials = %d, b sends = %d", a.isClosed(), d.dials(), b.count("terminal.send"))
	}
}

func TestRedialDoesNotSendAgainAfterClosed(t *testing.T) {
	a := newRedialConn("terminal.send", hc.ErrClosed)
	b := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a, b}}
	r := NewRedial(d.dial)
	if err := r.Call(context.Background(), "terminal.send", nil, nil); !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if !a.isClosed() || d.dials() != 1 || b.count("terminal.send") != 0 {
		t.Fatalf("a closed = %v, dials = %d, b sends = %d", a.isClosed(), d.dials(), b.count("terminal.send"))
	}
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	if d.dials() != 2 || b.count("terminal.list") != 1 {
		t.Fatalf("dials = %d, b lists = %d, want the next call on b", d.dials(), b.count("terminal.list"))
	}
}

func TestRedialRetriesOnce(t *testing.T) {
	a := newRedialConn("terminal.send", hc.ErrNotSent)
	b := newRedialConn("terminal.send", hc.ErrNotSent)
	c := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a, b, c}}
	r := NewRedial(d.dial)
	if err := r.Call(context.Background(), "terminal.send", nil, nil); !errors.Is(err, hc.ErrNotSent) {
		t.Fatalf("err = %v, want ErrNotSent", err)
	}
	if d.dials() != 2 {
		t.Fatalf("dials = %d, want 2", d.dials())
	}
}

func TestRedialKeepsTheConnOnOtherErrors(t *testing.T) {
	a := newRedialConn("terminal.get", &hc.Error{Kind: "not_found"})
	d := &dialer{conns: []*redialConn{a}}
	r := NewRedial(d.dial)
	if err := r.Call(context.Background(), "terminal.get", nil, nil); !hc.IsKind(err, "not_found") {
		t.Fatalf("err = %v, want not_found", err)
	}
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	if a.isClosed() || d.dials() != 1 || a.count("terminal.list") != 1 {
		t.Fatalf("a closed = %v, dials = %d, a lists = %d", a.isClosed(), d.dials(), a.count("terminal.list"))
	}
}

func TestRedialReplacesAConnThatEnded(t *testing.T) {
	a := newRedialConn("", nil)
	b := newRedialConn("", nil)
	c := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a, b, c}}
	r := NewRedial(d.dial)
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	a.ended.Store(true)
	if err := r.Call(context.Background(), "terminal.send", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !a.isClosed() || a.count("terminal.send") != 0 || b.count("terminal.send") != 1 {
		t.Fatalf("a closed = %v, a sends = %d, b sends = %d", a.isClosed(), a.count("terminal.send"), b.count("terminal.send"))
	}
	b.ended.Store(true)
	if _, err := r.Subscribe(context.Background(), hc.Filter{}); err != nil {
		t.Fatal(err)
	}
	if !b.isClosed() || d.dials() != 3 {
		t.Fatalf("b closed = %v, dials = %d, want Subscribe on a new conn", b.isClosed(), d.dials())
	}
}

func TestRedialDropOnlyTheConnItUsed(t *testing.T) {
	a := newRedialConn("", nil)
	b := newRedialConn("", nil)
	d := &dialer{conns: []*redialConn{a, b}}
	r := NewRedial(d.dial)
	// A call on a waits for its reply while a ends and b replaces it.
	sent := make(chan struct{})
	release := make(chan struct{})
	a.handle["terminal.wait"] = func(map[string]any) (any, error) {
		close(sent)
		<-release
		return nil, hc.ErrClosed
	}
	errc := make(chan error, 1)
	go func() { errc <- r.Call(context.Background(), "terminal.wait", nil, nil) }()
	<-sent
	a.ended.Store(true)
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errc; !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if b.isClosed() {
		t.Fatal("a late ErrClosed from a closed b")
	}
	if err := r.Call(context.Background(), "terminal.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	if d.dials() != 2 || b.count("terminal.list") != 2 {
		t.Fatalf("dials = %d, b lists = %d, want b to stay", d.dials(), b.count("terminal.list"))
	}
}

func TestRedialSubscribeDialsAndDropsAClosedConn(t *testing.T) {
	a := newRedialConn("", nil)
	a.subErr = hc.ErrClosed
	b := newRedialConn("", nil)
	b.stream = &fakeStream{}
	d := &dialer{conns: []*redialConn{a, b}}
	r := NewRedial(d.dial)
	if _, err := r.Subscribe(context.Background(), hc.Filter{}); !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if d.dials() != 1 || !a.isClosed() {
		t.Fatalf("dials = %d, a closed = %v", d.dials(), a.isClosed())
	}
	s, err := r.Subscribe(context.Background(), hc.Filter{})
	if err != nil || s != Stream(b.stream) || d.dials() != 2 {
		t.Fatalf("stream = %v, err = %v, dials = %d", s, err, d.dials())
	}
}

// blockDial returns a dial that waits for release, then gives c.
func blockDial(c *redialConn, started chan<- struct{}, release <-chan struct{}, n *atomic.Int32) func(context.Context) (Conn, error) {
	return func(ctx context.Context) (Conn, error) {
		if n.Add(1) == 1 {
			close(started)
			<-release
			return c, nil
		}
		return nil, errNoHC
	}
}

func TestRedialWaiterStopsOnItsContext(t *testing.T) {
	a := newRedialConn("", nil)
	started, release := make(chan struct{}), make(chan struct{})
	var n atomic.Int32
	r := NewRedial(blockDial(a, started, release, &n))
	first := make(chan error, 1)
	go func() { first <- r.Call(context.Background(), "terminal.list", nil, nil) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- r.Call(ctx, "terminal.list", nil, nil) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter did not stop on its context")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("dials = %d, want 1", n.Load())
	}
}

func TestRedialCloseDuringDial(t *testing.T) {
	a := newRedialConn("", nil)
	started, release := make(chan struct{}), make(chan struct{})
	var n atomic.Int32
	r := NewRedial(blockDial(a, started, release, &n))
	first := make(chan error, 1)
	go func() { first <- r.Call(context.Background(), "terminal.list", nil, nil) }()
	<-started
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-first; !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if !a.isClosed() || a.count("terminal.list") != 0 {
		t.Fatalf("a closed = %v, a calls = %d", a.isClosed(), a.count("terminal.list"))
	}
	if err := r.Call(context.Background(), "terminal.list", nil, nil); !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err after Close = %v, want ErrClosed", err)
	}
	if n.Load() != 1 {
		t.Fatalf("dials = %d, want 1 (no dial after Close)", n.Load())
	}
}
