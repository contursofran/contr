package terminal

import (
	"context"
	"sync"

	"github.com/contursofran/contr/internal/hc"
)

// CoreSource is the [HCSource] of one terminal on a core that dial reaches.
// It dials a framed connection on the first attach, and again after the
// connection ended.
type CoreSource struct {
	dial     hc.Dialer
	terminal string

	mu sync.Mutex
	fc *hc.FramedClient
}

// NewCoreSource returns a source for the terminal with the given address.
func NewCoreSource(dial hc.Dialer, terminal string) *CoreSource {
	return &CoreSource{dial: dial, terminal: terminal}
}

// Attach attaches to the terminal.
func (c *CoreSource) Attach(ctx context.Context, p hc.AttachParams) (HCStream, error) {
	fc, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	p.Terminal = c.terminal
	s, err := fc.Attach(ctx, p)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (c *CoreSource) client(ctx context.Context) (*hc.FramedClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fc != nil {
		select {
		case <-c.fc.Done():
			c.fc = nil
		default:
			return c.fc, nil
		}
	}
	fc, err := hc.DialFramed(ctx, c.dial)
	if err != nil {
		return nil, err
	}
	c.fc = fc
	return fc, nil
}

// Watch subscribes to the events of the terminal on a connection of its own.
func (c *CoreSource) Watch(ctx context.Context) (<-chan hc.Event, func(), error) {
	cl, err := hc.Dial(ctx, c.dial)
	if err != nil {
		return nil, nil, err
	}
	sub, err := cl.Subscribe(ctx, hc.Filter{Terminal: c.terminal})
	if err != nil {
		_ = cl.Close()
		return nil, nil, err
	}
	return sub.Events(), func() { _ = sub.Close(); _ = cl.Close() }, nil
}

// Close ends the framed connection.
func (c *CoreSource) Close() error {
	c.mu.Lock()
	fc := c.fc
	c.fc = nil
	c.mu.Unlock()
	if fc == nil {
		return nil
	}
	return fc.Close()
}
