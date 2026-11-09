package agents

import (
	"context"
	"errors"
	"sync"

	"github.com/contursofran/contr/internal/hc"
)

// Conn is one connection to the core.
type Conn interface {
	Core
	Close() error
}

// Redial is a Core that connects to hc on first use and again after the
// connection ended (#559). A call that the core never got (hc.ErrNotSent) is
// sent once more on a new connection; a call that was sent is not, because
// the core may have done it (D87).
type Redial struct {
	dial func(context.Context) (Conn, error)
	// OnDial, when set, runs after each good dial with the new connection,
	// before it is used. Set it before the first use.
	OnDial func(Conn)

	mu      sync.Mutex
	core    Conn
	dialing chan struct{} // not nil while a dial runs
	closed  bool
}

// NewRedial returns a Redial that connects with dial. It does not dial.
func NewRedial(dial func(context.Context) (Conn, error)) *Redial {
	return &Redial{dial: dial}
}

// get returns the current connection. It dials when there is none or when
// the current one has ended. The dial runs without the lock, so a caller
// that waits for another caller's dial can stop on its own ctx.
func (r *Redial) get(ctx context.Context) (Conn, error) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, hc.ErrClosed
		}
		if r.core != nil && dead(r.core) {
			_ = r.core.Close()
			r.core = nil
		}
		if r.core != nil {
			c := r.core
			r.mu.Unlock()
			return c, nil
		}
		if wait := r.dialing; wait != nil {
			r.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		r.dialing = done
		r.mu.Unlock()

		c, err := r.dial(ctx)

		r.mu.Lock()
		close(done)
		r.dialing = nil
		if err != nil {
			r.mu.Unlock()
			return nil, err
		}
		if r.closed {
			r.mu.Unlock()
			_ = c.Close()
			return nil, hc.ErrClosed
		}
		r.core = c
		r.mu.Unlock()
		if r.OnDial != nil {
			r.OnDial(c)
		}
		return c, nil
	}
}

// dead tells whether c says that it has ended.
func dead(c Conn) bool {
	cl, ok := c.(interface{ Closed() bool })
	return ok && cl.Closed()
}

// drop closes used, the connection of a call that ended with hc.ErrClosed,
// so the next call dials again. A connection that a later dial already
// replaced stays as it is.
func (r *Redial) drop(used Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.core == used && used != nil {
		_ = used.Close()
		r.core = nil
	}
}

// Call sends method on the current connection. On hc.ErrNotSent it sends it
// once more on a new connection (D87).
func (r *Redial) Call(ctx context.Context, method string, params, result any) error {
	for try := 0; ; try++ {
		c, err := r.get(ctx)
		if err != nil {
			return err
		}
		err = c.Call(ctx, method, params, result)
		if errors.Is(err, hc.ErrClosed) {
			r.drop(c)
		}
		if try == 0 && errors.Is(err, hc.ErrNotSent) {
			continue
		}
		return err
	}
}

// Subscribe subscribes on the current connection, and dials when there is
// none. It does not try again: the watch loop of the caller does.
func (r *Redial) Subscribe(ctx context.Context, f hc.Filter) (Stream, error) {
	c, err := r.get(ctx)
	if err != nil {
		return nil, err
	}
	s, err := c.Subscribe(ctx, f)
	if errors.Is(err, hc.ErrClosed) {
		r.drop(c)
	}
	return s, err
}

// Close closes the current connection. Later calls return hc.ErrClosed and
// do not dial.
func (r *Redial) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.core == nil {
		return nil
	}
	err := r.core.Close()
	r.core = nil
	return err
}
