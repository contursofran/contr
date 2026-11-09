// Package hc is a client for the JSON-RPC API of herdr-core. It talks to the
// core through `hc connect`: JSON-RPC 2.0, one message per line on stdin and
// stdout.
//
// A connection that subscribes to events sends only events after that, so a
// Client uses one connection for calls and opens one more for each
// subscription.
package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Generation is the protocol generation that this client speaks.
const Generation = 1

// ErrClosed is the error of a call that cannot complete because the
// connection is closed.
var ErrClosed = errors.New("hc: connection closed")

// ErrNotSent is the error of a call made on a connection that was already
// closed: the core never got it, so the call can be made again on a new
// connection. errors.Is(ErrNotSent, ErrClosed) is true.
var ErrNotSent = fmt.Errorf("%w before the call was sent", ErrClosed)

// Pipe is one connection to the core: the requests go to W, the replies and
// events come from R. Close is required: it ends the connection, so that R
// ends, and frees its resources. Stderr, when set, returns the last output of
// the core's side for error messages.
type Pipe struct {
	W      io.Writer
	R      io.Reader
	Close  func() error
	Stderr func() string
}

// tail keeps the last bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailMax = 2048

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = t.buf[len(t.buf)-tailMax:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// Dialer opens a new connection to the core.
type Dialer func(ctx context.Context) (Pipe, error)

// ErrNotFound is the error of Find when hc is on no searched path.
var ErrNotFound = errors.New("hc: program not found on PATH or in ~/.local/bin, /opt/homebrew/bin, /usr/local/bin; set CONTR_HC")

// Find returns the hc program: path when set, else "hc" from PATH, else the
// first hc in the usual places. An app opened from Finder has only the short
// launchd PATH, which lacks them. home is the home folder; dirs, when not
// nil, replaces the usual places.
func Find(path, home string, dirs []string) (string, error) {
	if path != "" {
		return path, nil
	}
	if p, err := exec.LookPath("hc"); err == nil {
		return p, nil
	}
	if dirs == nil {
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}
		if home != "" {
			dirs = append([]string{filepath.Join(home, ".local", "bin")}, dirs...)
		}
	}
	for _, d := range dirs {
		p := filepath.Join(d, "hc")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// The Claude Code session variables: claudeVar and each name that starts
// with claudePrefix. CleanEnv and CleanArgv remove them.
const (
	claudeVar    = "CLAUDECODE"
	claudePrefix = "CLAUDE_CODE_"
)

// ClaudeSessionVar tells whether name is a Claude Code session variable:
// CLAUDECODE or CLAUDE_CODE_*.
func ClaudeSessionVar(name string) bool {
	return name == claudeVar || strings.HasPrefix(name, claudePrefix)
}

// CleanEnv returns environ without CLAUDECODE and the CLAUDE_CODE_* variables.
// The hc server keeps the environment of the `hc connect` that starts it, and
// every terminal it opens inherits that. A server started inside a Claude Code
// session would mark all its terminals as child sessions of that session.
func CleanEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if ClaudeSessionVar(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// cleanScript unsets the variables that CleanEnv removes, then runs its
// arguments. A line of a value with several lines can give a name that is
// not set; unset of that name does nothing.
var cleanScript = `for v in $(env | sed -n -E 's/^(` + claudeVar + `|` + claudePrefix +
	`[A-Za-z0-9_]*)=.*/\1/p'); do unset "$v"; done; exec "$@"`

// cleanName is $0 of cleanScript, for ps and error messages.
const cleanName = "contr-clean-env"

// CleanArgv returns argv started through /bin/sh with the variables that
// CleanEnv removes unset (D216). terminal.run sets env on top of the core's
// environment and cannot unset a name, and contr cannot read the core's
// environment, so the shell removes them when the terminal starts.
func CleanArgv(argv []string) []string {
	return append([]string{"/bin/sh", "-c", cleanScript, cleanName}, argv...)
}

// UncleanArgv returns the argv that CleanArgv wrapped, and true. When argv
// does not start with the clean wrapper, it returns argv and false.
func UncleanArgv(argv []string) ([]string, bool) {
	if len(argv) > 4 && argv[3] == cleanName {
		return argv[4:], true
	}
	return argv, false
}

// Command returns a Dialer that starts `<path> connect` with env added to the
// environment of this process, less CLAUDECODE and CLAUDE_CODE_* (CleanEnv).
// The env argument is kept as it is. An empty path means hc, found with Find when
// the Dialer runs, so building the Dialer does no file work; when hc is not
// found, the Dialer returns that error.
func Command(path string, env []string) Dialer {
	return func(ctx context.Context) (Pipe, error) {
		path := path
		if path == "" {
			home, _ := os.UserHomeDir()
			found, err := Find("", home, nil)
			if err != nil {
				return Pipe{}, err
			}
			path = found
		}
		cmd := exec.Command(path, "connect")
		cmd.Env = append(CleanEnv(cmd.Environ()), env...)
		w, err := cmd.StdinPipe()
		if err != nil {
			return Pipe{}, err
		}
		r, err := cmd.StdoutPipe()
		if err != nil {
			return Pipe{}, err
		}
		errs := &tail{}
		cmd.Stderr = errs
		if err := cmd.Start(); err != nil {
			return Pipe{}, fmt.Errorf("hc: start %s connect: %w", path, err)
		}
		return Pipe{W: w, R: r, Close: func() error {
			_ = w.Close()
			_ = cmd.Process.Kill()
			err := cmd.Wait()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return nil
			}
			return err
		}, Stderr: errs.String}, nil
	}
}

// Error is an error reply of the core. Kind is stable (for example
// "not_found", "name_taken", "blocked"); Message and Hint are for people.
type Error struct {
	Code    int
	Kind    string
	Message string
	Hint    string
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("hc: %s: %s (%s)", e.Kind, e.Message, e.Hint)
	}
	return fmt.Sprintf("hc: %s: %s", e.Kind, e.Message)
}

// IsKind tells if err is an Error of the core with that kind.
func IsKind(err error, kind string) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type message struct {
	ID     *uint64         `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Kind string `json:"kind"`
			Hint string `json:"hint"`
		} `json:"data"`
	} `json:"error"`
}

func (m *message) err() error {
	if m.Error == nil {
		return nil
	}
	return &Error{Code: m.Error.Code, Kind: m.Error.Data.Kind, Message: m.Error.Message, Hint: m.Error.Data.Hint}
}

// conn is one JSON-RPC connection. A goroutine reads it until it ends.
type conn struct {
	pipe    Pipe
	writeMu sync.Mutex
	enc     *json.Encoder

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan *message
	closed  bool
	readErr error

	notify    func(*message) error // called on the reader goroutine
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newConn(pipe Pipe, notify func(*message) error) *conn {
	c := &conn{pipe: pipe, enc: json.NewEncoder(pipe.W), pending: map[uint64]chan *message{}, notify: notify, done: make(chan struct{})}
	go c.read()
	return c
}

func (c *conn) read() {
	defer close(c.done)
	sc := bufio.NewScanner(c.pipe.R)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			c.fail(fmt.Errorf("hc: bad message: %w", err))
			return
		}
		if m.ID == nil {
			if m.Method != "" && c.notify != nil {
				if err := c.notify(&m); err != nil {
					c.fail(err)
					return
				}
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[*m.ID]
		delete(c.pending, *m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- &m
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	if c.pipe.Stderr != nil {
		if msg := c.pipe.Stderr(); msg != "" {
			err = fmt.Errorf("hc: connection ended: %s: %w", msg, err)
		}
	}
	c.fail(err)
}

// fail ends every pending call. The first error is kept.
func (c *conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readErr == nil {
		c.readErr = err
	}
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.closed = true
}

func (c *conn) call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrNotSent
	}
	c.nextID++
	id := c.nextID
	ch := make(chan *message, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err := c.enc.Encode(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	c.writeMu.Unlock()
	if err != nil {
		c.drop(id)
		return fmt.Errorf("hc: send %s: %w", method, err)
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return ErrClosed
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
		c.drop(id)
		return ctx.Err()
	}
}

// isClosed tells whether the connection has ended or was closed.
func (c *conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *conn) drop(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// close ends the connection once; later calls return the same error.
func (c *conn) close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.pipe.Close != nil {
			c.closeErr = c.pipe.Close()
		}
		<-c.done
	})
	return c.closeErr
}

// Hello is the reply to the handshake.
type Hello struct {
	Generation int      `json:"generation"`
	Version    string   `json:"version"` // the core's version
	Methods    []string `json:"methods"`
	Features   []string `json:"features"`
	Core       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"core"`
}

// hello runs the handshake on c.
func hello(ctx context.Context, c *conn) (Hello, error) {
	var h Hello
	if err := c.call(ctx, "hello", map[string]any{}, &h); err != nil {
		return h, err
	}
	if h.Generation != Generation {
		return h, fmt.Errorf("hc: core speaks protocol generation %d, want %d", h.Generation, Generation)
	}
	return h, nil
}

// Client calls methods of the core.
type Client struct {
	dial  Dialer
	c     *conn
	Hello Hello
}

// Dial opens the call connection and runs the handshake.
func Dial(ctx context.Context, dial Dialer) (*Client, error) {
	pipe, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	c := newConn(pipe, nil)
	h, err := hello(ctx, c)
	if err != nil {
		_ = c.close()
		return nil, err
	}
	return &Client{dial: dial, c: c, Hello: h}, nil
}

// Call sends method with params and decodes the result into result (which
// can be nil). An error reply of the core is an *Error.
func (cl *Client) Call(ctx context.Context, method string, params, result any) error {
	return cl.c.call(ctx, method, params, result)
}

// Closed tells whether the call connection has ended or was closed. A call on
// it returns ErrNotSent.
func (cl *Client) Closed() bool {
	return cl.c.isClosed()
}

// Close ends the call connection. Pending calls end with ErrClosed. A second
// Close returns the result of the first.
func (cl *Client) Close() error {
	return cl.c.close()
}
