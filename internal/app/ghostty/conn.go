// Package ghostty shows hc terminals with MyGo's terminal plugin
// (libghostty-vt): the plugin emulates and draws, hc keeps the program.
package ghostty

import (
	"io"
	"sync"

	"github.com/contursofran/contr/internal/hc"
)

// Stream is the part of an attached hc terminal a Conn uses. *hc.Stream is one.
type Stream interface {
	Events() <-chan hc.StreamEvent
	Done() <-chan struct{}
	Err() error
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
}

// Conn is a terminal.Options.Conn over an hc stream: the plugin reads the
// program's output from it, writes what is typed to it, and tells it the size.
type Conn struct {
	s   Stream
	mu  sync.Mutex
	buf []byte
}

// NewConn wraps an attached stream.
func NewConn(s Stream) *Conn { return &Conn{s: s} }

// Read gives the stream's output; io.EOF once the stream ends.
func (c *Conn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.buf) == 0 {
		ev, ok := <-c.s.Events()
		if !ok {
			return 0, io.EOF
		}
		c.buf = ev.Data
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// Write sends typed input to the program.
func (c *Conn) Write(p []byte) (int, error) { return c.s.Write(p) }

// Resize tells hc the size the view fits.
func (c *Conn) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return c.s.Resize(uint16(cols), uint16(rows))
}

// Close detaches; the program keeps running in hc.
func (c *Conn) Close() error { return c.s.Close() }
