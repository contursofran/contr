// Connection to herdr's endpoint socket: connect, hello, the welcome check,
// then framed reads and writes.
// Written from herdr's wire layout; see NOTICE.
package endpoint

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/contursofran/contr/internal/herdr"
)

// EndpointSocketPath returns herdr's endpoint socket for this user, found the
// way herdr finds it. The second result is false when no setting gives a path.
func EndpointSocketPath() (string, bool) {
	return EndpointPathFrom(os.Getenv)
}

// EndpointPathFrom is the testable core of [EndpointSocketPath]. Empty values
// count as unset. HERDR_SOCKET_PATH gives <stem>-client.sock in its directory.
// Otherwise HERDR_CLIENT_SOCKET_PATH. Otherwise the default API socket's
// directory with the file name herdr-client.sock.
func EndpointPathFrom(env func(string) string) (string, bool) {
	if api := env("HERDR_SOCKET_PATH"); api != "" {
		return clientSibling(api), true
	}
	if path := env("HERDR_CLIENT_SOCKET_PATH"); path != "" {
		return path, true
	}
	api, ok := herdr.SocketPathFrom(env)
	if !ok {
		return "", false
	}
	return filepath.Join(filepath.Dir(api), "herdr-client.sock"), true
}

// clientSibling returns <stem>-client.sock in the directory of api. A path
// without a usable file name gets the stem "herdr".
func clientSibling(api string) string {
	dir, file := filepath.Split(api)
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	if file == "" || file == "." || file == ".." {
		stem = "herdr"
	}
	return dir + stem + "-client.sock"
}

// ConnKind tells why a connection to herdr's endpoint socket failed.
type ConnKind int

const (
	// NotRunning: nothing listens there, or herdr closed or stayed silent
	// before its welcome.
	NotRunning ConnKind = iota + 1
	// Incompatible: herdr refused contr, or sent something contr cannot read.
	// Retrying the same way fails the same way.
	Incompatible
	// IOError: a read or write failed. Err holds the cause. A read that ends
	// with io.ErrUnexpectedEOF means herdr closed the socket. A frame header
	// over MaxFrame is also IOError, with ErrFrameTooLarge, not Codec.
	IOError
	// Codec: a frame did not decode after the handshake. Err is the
	// *CodecError. The connection must be dropped.
	Codec
)

// ConnError is a failed open, send or recv.
type ConnError struct {
	Kind ConnKind
	// Msg is the text for NotRunning and Incompatible.
	Msg string
	// Err is the cause for IOError and Codec.
	Err error
}

func (e *ConnError) Error() string {
	switch e.Kind {
	case IOError:
		return fmt.Sprintf("endpoint socket: %v", e.Err)
	case Codec:
		return e.Err.Error()
	}
	return e.Msg
}

// Unwrap returns the cause, if any.
func (e *ConnError) Unwrap() error { return e.Err }

func notRunning(format string, args ...any) error {
	return &ConnError{Kind: NotRunning, Msg: fmt.Sprintf(format, args...)}
}

func incompatible(format string, args ...any) error {
	return &ConnError{Kind: Incompatible, Msg: fmt.Sprintf(format, args...)}
}

func ioError(err error) error { return &ConnError{Kind: IOError, Err: err} }

// Conn is an endpoint connection that passed the welcome check. Call Recv
// from one goroutine. Send and the Writer are safe to use from others.
type Conn struct {
	sock         net.Conn
	inbox        inbox
	Welcome      Welcome
	Version      VersionCheck
	readTimeout  time.Duration
	writeTimeout time.Duration
	writeMu      sync.Mutex
}

// Open connects, sends the hello and reads the welcome. No surface is
// requested yet. timeout bounds the connect and the whole handshake. It then
// stays the write timeout of every send, and the read timeout of Recv until
// SetReadTimeout changes it. A timeout of 0 or less means no limit.
func Open(path string, size GridSize, cell CellPx, timeout time.Duration) (*Conn, error) {
	sock, err := (&net.Dialer{Timeout: max(timeout, 0)}).Dial("unix", path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, notRunning("herdr is not running: %v", err)
		}
		return nil, ioError(err)
	}
	c := &Conn{sock: sock, readTimeout: timeout, writeTimeout: timeout}
	if err := c.handshake(size, cell, timeout); err != nil {
		_ = sock.Close()
		return nil, err
	}
	return c, nil
}

func (c *Conn) handshake(size GridSize, cell CellPx, timeout time.Duration) error {
	if err := c.send(HelloMsg{Size: size, Cell: cell}); err != nil {
		return err
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if err := c.sock.SetReadDeadline(deadline); err != nil {
		return ioError(err)
	}
	first, err := readPayload(c.sock, &c.inbox)
	switch {
	case err == nil && first == nil:
		return notRunning("herdr sent no welcome within %v", timeout)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return notRunning("herdr closed the endpoint before its welcome")
	case errors.Is(err, ErrFrameTooLarge):
		return incompatible("herdr sent a frame contr cannot read: %v", ErrFrameTooLarge)
	case err != nil:
		return err
	}

	msg, err := DecodeServer(first)
	if err != nil {
		return incompatible("herdr's first message does not decode: %v", err)
	}
	var welcome Welcome
	switch m := msg.(type) {
	case ControlMsg:
		if m.Kind != WelcomeKind {
			return incompatible("herdr's first message is not a welcome")
		}
		if err := json.Unmarshal([]byte(m.Data), &welcome); err != nil {
			return incompatible("herdr's welcome is not readable: %v", err)
		}
	case LegacyWelcomeMsg:
		if m.Error != nil {
			return incompatible("%s", *m.Error)
		}
		return incompatible("herdr refused the endpoint hello")
	default:
		return incompatible("herdr's first message is not a welcome")
	}
	version, err := CheckWelcome(&welcome)
	if err != nil {
		return incompatible("%v", err)
	}
	slog.Info("herdr endpoint connected", "server_version", welcome.ServerVersion, "generation", EndpointGeneration)
	if !version.Tested() {
		slog.Warn("herdr endpoint not tested with contr; wire layout may differ", "server_version", version.Untested)
	}
	c.Welcome, c.Version = welcome, version
	return nil
}

// Send writes one client message as one frame.
func (c *Conn) Send(msg ClientMsg) error { return c.send(msg) }

func (c *Conn) send(msg ClientMsg) error {
	return c.Writer().writeFrame(msg.Encode())
}

// Recv returns the next server message. It returns (nil, nil) when the read
// timeout passed before a whole frame arrived. The bytes read so far are
// kept, and the next call finishes that frame. A frame that does not decode
// is a Codec error. A frame header above MaxFrame is an IOError with
// ErrFrameTooLarge.
func (c *Conn) Recv() (ServerMsg, error) {
	var deadline time.Time
	if c.readTimeout > 0 {
		deadline = time.Now().Add(c.readTimeout)
	}
	if err := c.sock.SetReadDeadline(deadline); err != nil {
		return nil, ioError(err)
	}
	payload, err := readPayload(c.sock, &c.inbox)
	if err != nil || payload == nil {
		return nil, err
	}
	msg, err := DecodeServer(payload)
	if err != nil {
		return nil, &ConnError{Kind: Codec, Err: err}
	}
	return msg, nil
}

// SetReadTimeout sets how long one Recv waits for bytes. 0 or less means no
// limit. Call it from the goroutine that calls Recv.
func (c *Conn) SetReadTimeout(d time.Duration) { c.readTimeout = d }

// ReadTimeout returns the timeout that Recv waits for.
func (c *Conn) ReadTimeout() time.Duration { return c.readTimeout }

// Writer is a frame writer on the connection, for writes from another
// goroutine. It shares the write timeout that Open set, and the write lock.
type Writer struct{ c *Conn }

// Writer returns a frame writer on this connection.
func (c *Conn) Writer() Writer { return Writer{c} }

// Send writes one client message as one frame.
func (w Writer) Send(msg ClientMsg) error { return w.writeFrame(msg.Encode()) }

func (w Writer) writeFrame(payload []byte) error {
	c := w.c
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var deadline time.Time
	if c.writeTimeout > 0 {
		deadline = time.Now().Add(c.writeTimeout)
	}
	if err := c.sock.SetWriteDeadline(deadline); err != nil {
		return ioError(err)
	}
	if err := WriteFrame(c.sock, payload); err != nil {
		return ioError(err)
	}
	return nil
}

// Close closes the socket. It also ends a Recv that waits in another
// goroutine.
func (c *Conn) Close() error { return c.sock.Close() }

// readPayload returns the next whole frame's payload. It returns (nil, nil)
// on a read timeout; the bytes read so far stay in the inbox. EOF is an
// IOError with io.ErrUnexpectedEOF.
func readPayload(r io.Reader, in *inbox) ([]byte, error) {
	for {
		payload, err := in.takeFrame()
		if err != nil {
			return nil, ioError(err)
		}
		if payload != nil {
			return payload, nil
		}
		switch fill, err := in.fill(r); {
		case err != nil:
			return nil, ioError(err)
		case fill == fillTimeout:
			return nil, nil
		case fill == fillEOF:
			return nil, ioError(fmt.Errorf("herdr closed the endpoint: %w", io.ErrUnexpectedEOF))
		}
	}
}

// inbox holds bytes read from the socket that do not make a whole frame yet.
// A timeout leaves the partial frame here.
type inbox struct{ buf []byte }

type fillResult int

const (
	fillRead fillResult = iota
	fillTimeout
	fillEOF
)

// takeFrame cuts the first whole frame off the buffer. It returns nil while
// the frame is incomplete.
func (b *inbox) takeFrame() ([]byte, error) {
	if len(b.buf) < 4 {
		return nil, nil
	}
	n := binary.LittleEndian.Uint32(b.buf)
	if n > MaxFrame {
		return nil, ErrFrameTooLarge
	}
	end := 4 + int(n)
	if len(b.buf) < end {
		return nil, nil
	}
	frame := make([]byte, n) // never nil, also for an empty frame
	copy(frame, b.buf[4:end])
	b.buf = b.buf[end:]
	return frame, nil
}

// fill does one read from r. Interrupted reads are retried. A timeout and EOF
// are values, not errors.
func (b *inbox) fill(r io.Reader) (fillResult, error) {
	var chunk [8192]byte
	for {
		n, err := r.Read(chunk[:])
		b.buf = append(b.buf, chunk[:n]...)
		switch {
		case err == nil && n > 0:
			return fillRead, nil
		case err == nil, errors.Is(err, syscall.EINTR):
			// An empty read or an interrupted one: read again.
		case n > 0 && !isTimeout(err) && err != io.EOF:
			return fillRead, nil // use the bytes; the next read reports the error
		case err == io.EOF:
			if n > 0 {
				return fillRead, nil
			}
			return fillEOF, nil
		case isTimeout(err):
			if n > 0 {
				return fillRead, nil
			}
			return fillTimeout, nil
		default:
			return fillRead, err
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}
