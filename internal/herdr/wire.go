// Package herdr reads and changes herdr's state through its JSON API.
//
// This file is the wire format of the API: request lines, replies, the
// snapshot shape and the event subscription. Every call here is a read; the
// commands that change herdr's state live in command.go.
package herdr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf8"
)

// APIProtocol is the protocol number contr was written against (herdr 0.9.3).
const APIProtocol uint32 = 22

// MaxLine is the longest JSON line accepted from herdr, in bytes, without
// the newline.
const MaxLine = 8 * 1024 * 1024

// WaitForever is the wait for [Subscription.NextEvent] that has no timer.
const WaitForever time.Duration = -1

// APISocketPath returns herdr's API socket: HERDR_SOCKET_PATH, else
// $XDG_CONFIG_HOME/herdr/herdr.sock, else $HOME/.config/herdr/herdr.sock.
// The second result is false when none of these is set.
func APISocketPath() (string, bool) {
	return socketPathFrom(os.Getenv)
}

// socketPathFrom is the testable core of [APISocketPath]. Empty values count
// as unset.
func socketPathFrom(env func(string) string) (string, bool) {
	if path := env("HERDR_SOCKET_PATH"); path != "" {
		return path, true
	}
	configHome := env("XDG_CONFIG_HOME")
	if configHome == "" {
		home := env("HOME")
		if home == "" {
			return "", false
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "herdr", "herdr.sock"), true
}

// WireStatus is the agent state as herdr reports it. Any other string maps to
// [StatusUnknown], which is the zero value.
type WireStatus uint8

// The agent states.
const (
	StatusUnknown WireStatus = iota
	StatusIdle
	StatusWorking
	StatusBlocked
	StatusDone
)

// String returns the wire name of the state.
func (s WireStatus) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusWorking:
		return "working"
	case StatusBlocked:
		return "blocked"
	case StatusDone:
		return "done"
	}
	return "unknown"
}

// UnmarshalJSON reads a JSON string. A value that is not a string is an error.
func (s *WireStatus) UnmarshalJSON(data []byte) error {
	var text *string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	if text == nil {
		return errors.New("agent status is null")
	}
	switch *text {
	case "idle":
		*s = StatusIdle
	case "working":
		*s = StatusWorking
	case "blocked":
		*s = StatusBlocked
	case "done":
		*s = StatusDone
	default:
		*s = StatusUnknown
	}
	return nil
}

// Pong is the result of `ping`.
type Pong struct {
	Version  string
	Protocol uint32
}

// UnmarshalJSON requires `version` and `protocol`.
func (p *Pong) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version  *string `json:"version"`
		Protocol *uint32 `json:"protocol"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Version == nil {
		return missing("version")
	}
	if raw.Protocol == nil {
		return missing("protocol")
	}
	*p = Pong{Version: *raw.Version, Protocol: *raw.Protocol}
	return nil
}

func missing(field string) error {
	return fmt.Errorf("missing field `%s`", field)
}

// WireSnapshot is the `snapshot` object of a `session_snapshot` result.
// The optional ids are nil when herdr leaves them out.
type WireSnapshot struct {
	Version            string
	Protocol           uint32
	FocusedWorkspaceID *string
	FocusedTabID       *string
	FocusedPaneID      *string
	Workspaces         []WireWorkspace
	Tabs               []WireTab
	Panes              []WirePane
}

// UnmarshalJSON requires `version` and `protocol`. The lists default to empty.
func (s *WireSnapshot) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version            *string         `json:"version"`
		Protocol           *uint32         `json:"protocol"`
		FocusedWorkspaceID *string         `json:"focused_workspace_id"`
		FocusedTabID       *string         `json:"focused_tab_id"`
		FocusedPaneID      *string         `json:"focused_pane_id"`
		Workspaces         []WireWorkspace `json:"workspaces"`
		Tabs               []WireTab       `json:"tabs"`
		Panes              []WirePane      `json:"panes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Version == nil {
		return missing("version")
	}
	if raw.Protocol == nil {
		return missing("protocol")
	}
	*s = WireSnapshot{
		Version:            *raw.Version,
		Protocol:           *raw.Protocol,
		FocusedWorkspaceID: raw.FocusedWorkspaceID,
		FocusedTabID:       raw.FocusedTabID,
		FocusedPaneID:      raw.FocusedPaneID,
		Workspaces:         raw.Workspaces,
		Tabs:               raw.Tabs,
		Panes:              raw.Panes,
	}
	return nil
}

// WireWorkspace is one workspace of a snapshot.
type WireWorkspace struct {
	WorkspaceID string
	Label       string
	Focused     bool
	AgentStatus WireStatus
}

// UnmarshalJSON requires `workspace_id`, `label` and `agent_status`.
func (w *WireWorkspace) UnmarshalJSON(data []byte) error {
	var raw struct {
		WorkspaceID *string     `json:"workspace_id"`
		Label       *string     `json:"label"`
		Focused     bool        `json:"focused"`
		AgentStatus *WireStatus `json:"agent_status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.WorkspaceID == nil:
		return missing("workspace_id")
	case raw.Label == nil:
		return missing("label")
	case raw.AgentStatus == nil:
		return missing("agent_status")
	}
	*w = WireWorkspace{*raw.WorkspaceID, *raw.Label, raw.Focused, *raw.AgentStatus}
	return nil
}

// WireTab is one tab of a snapshot.
type WireTab struct {
	TabID       string
	WorkspaceID string
	Label       string
	Focused     bool
	AgentStatus WireStatus
}

// UnmarshalJSON requires `tab_id`, `workspace_id`, `label` and `agent_status`.
func (t *WireTab) UnmarshalJSON(data []byte) error {
	var raw struct {
		TabID       *string     `json:"tab_id"`
		WorkspaceID *string     `json:"workspace_id"`
		Label       *string     `json:"label"`
		Focused     bool        `json:"focused"`
		AgentStatus *WireStatus `json:"agent_status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.TabID == nil:
		return missing("tab_id")
	case raw.WorkspaceID == nil:
		return missing("workspace_id")
	case raw.Label == nil:
		return missing("label")
	case raw.AgentStatus == nil:
		return missing("agent_status")
	}
	*t = WireTab{*raw.TabID, *raw.WorkspaceID, *raw.Label, raw.Focused, *raw.AgentStatus}
	return nil
}

// WirePane is one pane of a snapshot. The optional strings are nil when herdr
// leaves them out.
type WirePane struct {
	PaneID       string
	TabID        string
	WorkspaceID  string
	Focused      bool
	AgentStatus  WireStatus
	Agent        *string
	DisplayAgent *string
	Label        *string
	Cwd          *string
	// Tokens are the pane tokens as herdr reports them. A pane without
	// `tokens` has none.
	Tokens map[string]string
}

// UnmarshalJSON requires `pane_id`, `tab_id`, `workspace_id` and
// `agent_status`. A token value that is not a string is an error.
func (p *WirePane) UnmarshalJSON(data []byte) error {
	var raw struct {
		PaneID       *string           `json:"pane_id"`
		TabID        *string           `json:"tab_id"`
		WorkspaceID  *string           `json:"workspace_id"`
		Focused      bool              `json:"focused"`
		AgentStatus  *WireStatus       `json:"agent_status"`
		Agent        *string           `json:"agent"`
		DisplayAgent *string           `json:"display_agent"`
		Label        *string           `json:"label"`
		Cwd          *string           `json:"cwd"`
		Tokens       map[string]string `json:"tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.PaneID == nil:
		return missing("pane_id")
	case raw.TabID == nil:
		return missing("tab_id")
	case raw.WorkspaceID == nil:
		return missing("workspace_id")
	case raw.AgentStatus == nil:
		return missing("agent_status")
	}
	*p = WirePane{
		PaneID:       *raw.PaneID,
		TabID:        *raw.TabID,
		WorkspaceID:  *raw.WorkspaceID,
		Focused:      raw.Focused,
		AgentStatus:  *raw.AgentStatus,
		Agent:        raw.Agent,
		DisplayAgent: raw.DisplayAgent,
		Label:        raw.Label,
		Cwd:          raw.Cwd,
		Tokens:       raw.Tokens,
	}
	return nil
}

// ErrKind tells why a request to herdr failed.
type ErrKind uint8

// The kinds of [WireError].
const (
	// ErrIO: connect, read or write failed.
	ErrIO ErrKind = iota
	// ErrClosed: the peer closed the stream.
	ErrClosed
	// ErrServer: herdr answered with {"error":{code,message}}.
	ErrServer
	// ErrUnexpected: the reply did not parse, had the wrong shape, or a line
	// was too long.
	ErrUnexpected
)

// WireError is why a request to herdr failed. Match the kind with
// errors.As and the Kind field.
type WireError struct {
	Kind ErrKind
	// Err is the cause of an ErrIO error.
	Err error
	// Code and Message are the fields of an ErrServer error.
	Code, Message string
	// What describes an ErrUnexpected error.
	What string
}

// IsNotRunning is true when no herdr is listening: the socket file is
// missing or refuses connections.
func (e *WireError) IsNotRunning() bool {
	return e.Kind == ErrIO && (errors.Is(e.Err, fs.ErrNotExist) ||
		errors.Is(e.Err, syscall.ECONNREFUSED) ||
		errors.Is(e.Err, syscall.EADDRNOTAVAIL))
}

// Error returns the text for the user.
func (e *WireError) Error() string {
	switch e.Kind {
	case ErrIO:
		return e.Err.Error()
	case ErrClosed:
		return "herdr closed the connection"
	case ErrServer:
		return "herdr error " + e.Code + ": " + e.Message
	}
	return e.What
}

// Unwrap returns the cause of an ErrIO error.
func (e *WireError) Unwrap() error {
	if e.Kind == ErrIO {
		return e.Err
	}
	return nil
}

func ioError(err error) error { return &WireError{Kind: ErrIO, Err: err} }

func closedError() error { return &WireError{Kind: ErrClosed} }

func unexpected(format string, args ...any) error {
	return &WireError{Kind: ErrUnexpected, What: fmt.Sprintf(format, args...)}
}

func timedOut() error { return ioError(os.ErrDeadlineExceeded) }

// RequestLine returns one request line: {"id","method","params"} followed by
// the newline that ends it. Keys keep this order, and the text is not
// escaped for HTML.
func RequestLine(id, method string, params any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{id, method, params})
	if err != nil {
		return "", unexpected("request cannot be encoded: %v", err)
	}
	// Encode ends the value with the one newline that ends the line.
	return buf.String(), nil
}

// parseReply returns the `result` of one reply line, or the server's error.
func parseReply(line string) (json.RawMessage, error) {
	var reply struct {
		Result *json.RawMessage `json:"result"`
		Error  *struct {
			Code    *string `json:"code"`
			Message *string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		return nil, unexpected("reply is not a JSON object: %v", err)
	}
	switch {
	case reply.Error != nil:
		if reply.Error.Code == nil || reply.Error.Message == nil {
			return nil, unexpected("reply is not a JSON object: error has no code or message")
		}
		return nil, &WireError{Kind: ErrServer, Code: *reply.Error.Code, Message: *reply.Error.Message}
	case reply.Result != nil:
		return *reply.Result, nil
	}
	return nil, unexpected("reply has neither result nor error")
}

func connect(path string) (*net.UnixConn, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, ioError(err)
	}
	return conn, nil
}

// deadlineReader bounds each read, not the whole stream.
type deadlineReader struct {
	conn    net.Conn
	timeout time.Duration
}

func (r deadlineReader) Read(p []byte) (int, error) {
	if err := r.conn.SetReadDeadline(time.Now().Add(r.timeout)); err != nil {
		return 0, err
	}
	return r.conn.Read(p)
}

// exchange sends one request on a fresh connection and returns the `result`
// of the reply.
func exchange(path string, timeout time.Duration, id, method string, params any) (json.RawMessage, error) {
	request, err := RequestLine(id, method, params)
	if err != nil {
		return nil, err
	}
	conn, err := connect(path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return nil, ioError(err)
	}
	if _, err := conn.Write([]byte(request)); err != nil {
		return nil, ioError(err)
	}
	// The reply id is not compared with `id`: one request per connection, so
	// the first line on it is the reply to that request.
	line, ok, err := NewLineReader(deadlineReader{conn, timeout}).NextLine()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, timedOut()
	}
	return parseReply(line)
}

// expectType checks the `type` field of a result.
func expectType(result json.RawMessage, kind string) error {
	found := "none"
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) == nil {
		var text string
		if json.Unmarshal(fields["type"], &text) == nil {
			found = text
		}
	}
	if found != kind {
		return unexpected("expected result type %s, got %s", kind, found)
	}
	return nil
}

func fromResult(result json.RawMessage, into any) error {
	if err := json.Unmarshal(result, into); err != nil {
		return unexpected("result has an unexpected shape: %v", err)
	}
	return nil
}

// Ping asks herdr for its version and protocol (`ping`). It uses one
// connection per call. timeout bounds each read and each write, not the
// whole call: a peer that sends one byte at a time can keep the call going
// longer than timeout.
func Ping(path string, timeout time.Duration) (Pong, error) {
	result, err := exchange(path, timeout, "contr-ping", "ping", map[string]any{})
	if err != nil {
		return Pong{}, err
	}
	if err := expectType(result, "pong"); err != nil {
		return Pong{}, err
	}
	var pong Pong
	if err := fromResult(result, &pong); err != nil {
		return Pong{}, err
	}
	return pong, nil
}

// Snapshot reads the whole session (`session.snapshot`). It uses one
// connection per call. timeout bounds each read and each write, not the
// whole call (see [Ping]).
func Snapshot(path string, timeout time.Duration) (WireSnapshot, error) {
	result, err := exchange(path, timeout, "contr-snapshot", "session.snapshot", map[string]any{})
	if err != nil {
		return WireSnapshot{}, err
	}
	if err := expectType(result, "session_snapshot"); err != nil {
		return WireSnapshot{}, err
	}
	var body struct {
		Snapshot *WireSnapshot `json:"snapshot"`
	}
	if err := fromResult(result, &body); err != nil {
		return WireSnapshot{}, err
	}
	if body.Snapshot == nil {
		return WireSnapshot{}, unexpected("result has an unexpected shape: missing field `snapshot`")
	}
	return *body.Snapshot, nil
}

// LineReader splits a byte stream into newline-terminated lines and keeps
// partial lines between calls, so a read timeout never loses bytes.
type LineReader struct {
	inner io.Reader
	buf   []byte
}

// NewLineReader returns a LineReader that reads from r.
func NewLineReader(r io.Reader) *LineReader {
	return &LineReader{inner: r}
}

func isTimeout(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.ETIMEDOUT)
}

// NextLine returns a full line without its newline and true. It returns
// "", false, nil on a read timeout. It returns a Closed error at the end of
// the stream, and an Unexpected error when a line exceeds [MaxLine] or is not
// UTF-8. Other read errors are IO errors.
func (r *LineReader) NextLine() (string, bool, error) {
	// Bytes at the front of buf already known to hold no newline.
	scanned := 0
	for {
		if offset := bytes.IndexByte(r.buf[scanned:], '\n'); offset >= 0 {
			end := scanned + offset
			if end > MaxLine {
				return "", false, overlong()
			}
			line := r.buf[:end]
			valid := utf8.Valid(line)
			text := string(line)
			n := copy(r.buf, r.buf[end+1:])
			r.buf = r.buf[:n]
			if !valid {
				return "", false, unexpected("herdr sent a line that is not UTF-8")
			}
			return text, true, nil
		}
		scanned = len(r.buf)
		if scanned > MaxLine {
			return "", false, overlong()
		}
		var chunk [8192]byte
		n, err := r.inner.Read(chunk[:])
		if n > 0 {
			r.buf = append(r.buf, chunk[:n]...)
			continue
		}
		switch {
		case err == nil, errors.Is(err, io.EOF):
			return "", false, closedError()
		case errors.Is(err, syscall.EINTR):
		case isTimeout(err):
			return "", false, nil
		default:
			return "", false, ioError(err)
		}
	}
}

func overlong() error {
	return unexpected("herdr sent a line longer than %d bytes", MaxLine)
}

// GlobalEvents are the event kinds subscribed to globally. Agent status is
// not one of them: it needs one pane.agent_status_changed per pane.
var GlobalEvents = []string{
	"workspace.created",
	"workspace.updated",
	"workspace.renamed",
	"workspace.moved",
	"workspace.reordered",
	"workspace.closed",
	"workspace.focused",
	"tab.created",
	"tab.closed",
	"tab.focused",
	"tab.renamed",
	"tab.moved",
	"pane.created",
	"pane.closed",
	"pane.updated",
	"pane.focused",
	"pane.moved",
	"pane.exited",
	"pane.agent_detected",
}

// Subscription is an open `events.subscribe` stream. Each event only means
// "something changed". One goroutine calls NextEvent; [Subscription.Wake] and
// [Subscription.Close] may run on others.
type Subscription struct {
	conn   *net.UnixConn
	reader *LineReader
}

// OpenSubscription connects and subscribes to [GlobalEvents] plus one
// pane.agent_status_changed per pane id. It waits up to timeout for
// `subscription_started`; a server error (for example `pane_not_found`)
// fails. timeout bounds the write and the ack wait.
func OpenSubscription(path string, paneIDs []string, timeout time.Duration) (*Subscription, error) {
	subs := make([]map[string]string, 0, len(GlobalEvents)+len(paneIDs))
	for _, kind := range GlobalEvents {
		subs = append(subs, map[string]string{"type": kind})
	}
	for _, id := range paneIDs {
		subs = append(subs, map[string]string{"type": "pane.agent_status_changed", "pane_id": id})
	}
	request, err := RequestLine("contr-sub", "events.subscribe", map[string]any{"subscriptions": subs})
	if err != nil {
		return nil, err
	}
	conn, err := connect(path)
	if err != nil {
		return nil, err
	}
	sub := &Subscription{conn: conn, reader: NewLineReader(conn)}
	if err := sub.handshake(request, timeout); err != nil {
		conn.Close()
		return nil, err
	}
	return sub, nil
}

func (s *Subscription) handshake(request string, timeout time.Duration) error {
	if err := s.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return ioError(err)
	}
	if _, err := s.conn.Write([]byte(request)); err != nil {
		return ioError(err)
	}
	deadline := time.Now().Add(timeout)
	var ack string
	for {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return ioError(err)
		}
		line, ok, err := s.reader.NextLine()
		if err != nil {
			return err
		}
		if ok {
			ack = line
			break
		}
		if !time.Now().Before(deadline) {
			return timedOut()
		}
	}
	// The ack id is not compared with `contr-sub`: one subscription per
	// connection.
	result, err := parseReply(ack)
	if err != nil {
		return err
	}
	return expectType(result, "subscription_started")
}

// NextEvent waits for the next event. wait is the longest wait;
// [WaitForever] waits until an event comes or the stream ends, with no timer.
// It returns true when an event line arrived, false when nothing came within
// wait, and a Closed error when the stream ended.
func (s *Subscription) NextEvent(wait time.Duration) (bool, error) {
	var deadline time.Time // zero: no deadline
	if wait != WaitForever {
		// A wait that is already over becomes the shortest wait.
		deadline = time.Now().Add(max(wait, time.Millisecond))
	}
	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return false, ioError(err)
	}
	_, ok, err := s.reader.NextLine()
	return ok, err
}

// Wake shuts the socket down. A [Subscription.NextEvent] that is blocked on
// another goroutine then ends at once with a Closed error.
func (s *Subscription) Wake() error {
	err := s.conn.CloseRead()
	if werr := s.conn.CloseWrite(); err == nil {
		err = werr
	}
	if err != nil {
		return ioError(err)
	}
	return nil
}

// Close closes the socket.
func (s *Subscription) Close() error {
	return s.conn.Close()
}
