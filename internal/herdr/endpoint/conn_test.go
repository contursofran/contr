package endpoint

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	testGrid = GridSize{Cols: 60, Rows: 12}
)

const wait = 2 * time.Second

func envOf(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func wantPath(t *testing.T, env func(string) string, want string, wantOK bool) {
	t.Helper()
	got, ok := EndpointPathFrom(env)
	if got != want || ok != wantOK {
		t.Errorf("EndpointPathFrom = %q, %v; want %q, %v", got, ok, want, wantOK)
	}
}

func TestEndpointPathFollowsTheAPISocketName(t *testing.T) {
	wantPath(t, envOf("HERDR_SOCKET_PATH", "/s/herdr.sock"), "/s/herdr-client.sock", true)
	wantPath(t, envOf("HERDR_SOCKET_PATH", "/s/a.sock"), "/s/a-client.sock", true)
}

func TestEndpointPathStemDefaultsToHerdrWithoutAFileName(t *testing.T) {
	wantPath(t, envOf("HERDR_SOCKET_PATH", "/s/.."), "/s/herdr-client.sock", true)
}

func TestEndpointPathUsesTheClientVariableWhenNoAPISocketIsSet(t *testing.T) {
	wantPath(t, envOf("HERDR_CLIENT_SOCKET_PATH", "/c/x.sock"), "/c/x.sock", true)
	// An empty API socket counts as unset.
	wantPath(t, envOf("HERDR_SOCKET_PATH", "", "HERDR_CLIENT_SOCKET_PATH", "/c/x.sock"), "/c/x.sock", true)
	// The API socket wins over the client variable.
	wantPath(t, envOf("HERDR_SOCKET_PATH", "/s/a.sock", "HERDR_CLIENT_SOCKET_PATH", "/c/x.sock"), "/s/a-client.sock", true)
}

func TestEndpointPathFallsBackToTheDefaultAPIDirectory(t *testing.T) {
	wantPath(t, envOf("HOME", "/h"), "/h/.config/herdr/herdr-client.sock", true)
	wantPath(t, envOf("XDG_CONFIG_HOME", "/x", "HOME", "/h"), "/x/herdr/herdr-client.sock", true)
}

func TestEndpointPathIsNoneWithoutAnySetting(t *testing.T) {
	wantPath(t, envOf(), "", false)
}

// connErr checks that err is a *ConnError of the kind and returns it.
func connErr(t *testing.T, err error, kind ConnKind) *ConnError {
	t.Helper()
	var ce *ConnError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %v (%T); want a *ConnError", err, err)
	}
	if ce.Kind != kind {
		t.Fatalf("kind = %d (%v); want %d", ce.Kind, ce, kind)
	}
	return ce
}

func openErr(t *testing.T, path string, timeout time.Duration, kind ConnKind) *ConnError {
	t.Helper()
	conn, err := Open(path, testGrid, testCell, timeout)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Open succeeded")
	}
	return connErr(t, err, kind)
}

func mustOpen(t *testing.T, path string) *Conn {
	t.Helper()
	conn, err := Open(path, testGrid, testCell, wait)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func contains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%q does not contain %q", got, want)
	}
}

// logSink collects log lines. It is the default logger while a test runs.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// captureLogs sets the default logger to a collecting one until the test
// ends, also when it panics. These tests must not run in parallel.
func captureLogs(t *testing.T) *logSink {
	t.Helper()
	sink := &logSink{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr { return dropTime(a) },
	})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return sink
}

func dropTime(a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

func serveWelcome(t *testing.T, c net.Conn) {
	readHello(t, c)
	put(t, c, welcome("0.9.3"))
}

func TestOpenAcceptsAV1WelcomeAfterTheHello(t *testing.T) {
	logs := captureLogs(t)
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	if conn.Version.Tested() != true {
		t.Errorf("Version = %+v; want tested", conn.Version)
	}
	if conn.Welcome.Generation != 1 || conn.Welcome.ServerVersion != "0.9.3" {
		t.Errorf("Welcome = %+v", conn.Welcome)
	}
	contains(t, logs.String(), "herdr endpoint connected")
	contains(t, logs.String(), "server_version=0.9.3")
	contains(t, logs.String(), "generation=1")
	if strings.Contains(logs.String(), "not tested") {
		t.Errorf("unexpected warning: %s", logs)
	}
	join()
}

func TestCaptureSeesLogsAfterAnotherGoroutineLoggedFirst(t *testing.T) {
	logs := captureLogs(t)
	serve := func(c net.Conn) struct{} {
		serveWelcome(t, c)
		return struct{}{}
	}
	path, join := fakeHerdr(t, serve)
	otherPath, otherJoin := fakeHerdr(t, serve)
	done := make(chan struct{})
	go func() {
		defer close(done)
		mustOpen(t, otherPath)
	}()
	<-done
	mustOpen(t, path)
	if n := strings.Count(logs.String(), "herdr endpoint connected"); n != 2 {
		t.Errorf("connected lines = %d; want 2\n%s", n, logs)
	}
	join()
	otherJoin()
}

func TestCaptureRestoresTheLoggerWhenTheTestPanics(t *testing.T) {
	before := slog.Default()
	t.Run("inner", func(t *testing.T) {
		captureLogs(t)
		if slog.Default() == before {
			t.Error("capture did not replace the logger")
		}
	})
	if slog.Default() != before {
		t.Error("the logger was not restored")
	}
}

func TestOpenConnectsAndWarnsOnAnUntestedVersion(t *testing.T) {
	logs := captureLogs(t)
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		put(t, c, welcome("0.9.9"))
		return struct{}{}
	})
	conn := mustOpen(t, path)
	if conn.Version.Untested != "0.9.9" {
		t.Errorf("Version = %+v; want untested 0.9.9", conn.Version)
	}
	contains(t, logs.String(), "herdr endpoint not tested with contr; wire layout may differ")
	contains(t, logs.String(), "server_version=0.9.9")
	join()
}

// refuses runs a server that sends reply after the hello, and checks that
// Open fails as Incompatible with a message that holds every want text.
func refuses(t *testing.T, reply []byte, want ...string) {
	t.Helper()
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		put(t, c, reply)
		return struct{}{}
	})
	ce := openErr(t, path, wait, Incompatible)
	for _, w := range want {
		contains(t, ce.Msg, w)
	}
	join()
}

func TestOpenRefusesAWelcomeWithAnError(t *testing.T) {
	json := welcomeBody(1, "0.9.3", `,"error":{"code":"invalid_hello","message":"bad hello"}`)
	refuses(t, control(WelcomeKind, json), "invalid_hello", "bad hello")
}

func TestOpenRefusesAnotherGeneration(t *testing.T) {
	refuses(t, control(WelcomeKind, welcomeBody(2, "0.9.3", "")), "generation 2")
}

func TestOpenRefusesAChangedCodecName(t *testing.T) {
	json := strings.Replace(welcomeBody(1, "0.9.3", ""), SurfaceCodec, "shell.surface.v2", 1)
	refuses(t, control(WelcomeKind, json), "shell.surface.v2")
}

func TestOpenRefusesALegacyWelcomeWithItsError(t *testing.T) {
	text := "client too old"
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		put(t, c, legacyWelcome(&text))
		return struct{}{}
	})
	if got := openErr(t, path, wait, Incompatible).Msg; got != "client too old" {
		t.Errorf("Msg = %q", got)
	}
	join()
}

func TestOpenRefusesALegacyWelcomeWithoutAnError(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		put(t, c, legacyWelcome(nil))
		return struct{}{}
	})
	if got := openErr(t, path, wait, Incompatible).Msg; got != "herdr refused the endpoint hello" {
		t.Errorf("Msg = %q", got)
	}
	join()
}

func TestOpenRefusesAFirstMessageThatIsNotAWelcome(t *testing.T) {
	refuses(t, control(SnapshotKind, "{}"), "not a welcome")
}

func TestOpenRefusesAnUndecodableFirstFrame(t *testing.T) {
	refuses(t, []byte{0xFF}, "does not decode") // a varint marker that does not exist
}

func TestOpenRefusesAWelcomeWhoseJSONIsUnreadable(t *testing.T) {
	refuses(t, control(WelcomeKind, "{"), "not readable")
}

func TestOpenRefusesAFirstFrameOverTheFrameLimit(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		_, _ = c.Write(binary.LittleEndian.AppendUint32(nil, MaxFrame+1))
		return struct{}{}
	})
	contains(t, openErr(t, path, wait, Incompatible).Msg, "cannot read")
	join()
}

func TestOpenWithoutAReplyIsNotRunningAfterTheTimeout(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		time.Sleep(600 * time.Millisecond)
		return struct{}{}
	})
	started := time.Now()
	openErr(t, path, 200*time.Millisecond, NotRunning)
	if time.Since(started) > 5*time.Second {
		t.Errorf("Open took %v", time.Since(started))
	}
	join()
}

func TestOpenWhenHerdrClosesBeforeTheWelcomeIsNotRunning(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		readHello(t, c)
		return struct{}{}
	})
	contains(t, openErr(t, path, wait, NotRunning).Msg, "closed")
	join()
}

func TestOpenWithNoSocketIsNotRunning(t *testing.T) {
	openErr(t, filepath.Join(shortTempDir(t), "herdr-client.sock"), wait, NotRunning)
}

func TestOpenWithASocketNobodyListensOnIsNotRunning(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "herdr-client.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	// Closing the listener without unlinking leaves the socket file behind:
	// connect is refused.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	contains(t, openErr(t, path, wait, NotRunning).Msg, "not running")
}

func TestOpenWithAPathTooLongForASocketIsAnIOError(t *testing.T) {
	openErr(t, "/tmp/"+strings.Repeat("a", 120), wait, IOError)
}

func TestSendWritesOneFrameTheServerReads(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) []byte {
		serveWelcome(t, c)
		p, _ := ReadFrame(c)
		return p
	})
	conn := mustOpen(t, path)
	if err := conn.Send(FocusMsg{true}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got := join(); !bytes.Equal(got, FocusMsg{true}.Encode()) {
		t.Errorf("server read %v", got)
	}
}

func TestAClonedWriterWritesToTheSameConnection(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) []byte {
		serveWelcome(t, c)
		p, _ := ReadFrame(c)
		return p
	})
	conn := mustOpen(t, path)
	if err := conn.Writer().Send(FocusMsg{false}); err != nil {
		t.Fatal(err)
	}
	if got := join(); !bytes.Equal(got, FocusMsg{false}.Encode()) {
		t.Errorf("server read %v", got)
	}
}

func TestAClonedWriterSharesTheHandshakeWriteTimeout(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		time.Sleep(100 * time.Millisecond)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	if got := conn.Writer().c.writeTimeout; got != wait {
		t.Errorf("write timeout = %v; want %v", got, wait)
	}
	join()
}

func TestOpenLeavesTheHandshakeTimeoutAsTheReadTimeout(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		time.Sleep(100 * time.Millisecond)
		return struct{}{}
	})
	if got := mustOpen(t, path).ReadTimeout(); got != wait {
		t.Errorf("read timeout = %v; want %v", got, wait)
	}
	join()
}

func TestErrorsNameTheirCause(t *testing.T) {
	cases := []struct {
		err  *ConnError
		want string
	}{
		{&ConnError{Kind: NotRunning, Msg: "herdr is not running"}, "herdr is not running"},
		{&ConnError{Kind: Incompatible, Msg: "generation 2"}, "generation 2"},
		{&ConnError{Kind: IOError, Err: errors.New("boom")}, "endpoint socket: boom"},
		{&ConnError{Kind: Codec, Err: &CodecError{Kind: Truncated}}, "endpoint payload ended early"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q; want %q", got, c.want)
		}
	}
}

func TestRecvKeepsAPartialFrameAcrossReadTimeouts(t *testing.T) {
	const data = `{"boot_id":"b1","revision":3}`
	var frame bytes.Buffer
	_ = WriteFrame(&frame, control(SnapshotKind, data))
	first, rest := frame.Bytes()[:6], frame.Bytes()[6:]
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		_, _ = c.Write(first)
		time.Sleep(300 * time.Millisecond)
		_, _ = c.Write(rest)
		time.Sleep(100 * time.Millisecond)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	conn.SetReadTimeout(30 * time.Millisecond)
	started := time.Now()
	timeouts := 0
	var msg ServerMsg
	for msg == nil {
		if time.Since(started) > 5*time.Second {
			t.Fatal("frame never completed")
		}
		var err error
		if msg, err = conn.Recv(); err != nil {
			t.Fatal(err)
		}
		if msg == nil {
			timeouts++
		}
	}
	if timeouts == 0 {
		t.Error("no read timeout hit while the frame was split")
	}
	if want := (ControlMsg{Kind: SnapshotKind, Data: data}); msg != want {
		t.Errorf("msg = %+v; want %+v", msg, want)
	}
	join()
}

func TestRecvReportsAnUndecodableFrameAsACodecError(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		put(t, c, []byte{0xFF})
		time.Sleep(100 * time.Millisecond)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	_, err := conn.Recv()
	ce := connErr(t, err, Codec)
	var cerr *CodecError
	if !errors.As(ce, &cerr) || cerr.Kind != BadVarint || cerr.Value != 0xFF {
		t.Errorf("cause = %v; want BadVarint 0xFF", ce.Err)
	}
	join()
}

func TestRecvReportsAnOversizedFrameHeaderAsInvalidDataNotCodec(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		_, _ = c.Write(binary.LittleEndian.AppendUint32(nil, MaxFrame+1))
		time.Sleep(100 * time.Millisecond)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	_, err := conn.Recv()
	if ce := connErr(t, err, IOError); !errors.Is(ce, ErrFrameTooLarge) {
		t.Errorf("cause = %v; want ErrFrameTooLarge", ce.Err)
	}
	join()
}

func TestRecvAfterHerdrClosesIsAnUnexpectedEOF(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		return struct{}{}
	})
	conn := mustOpen(t, path)
	join()
	_, err := conn.Recv()
	if ce := connErr(t, err, IOError); !errors.Is(ce, io.ErrUnexpectedEOF) {
		t.Errorf("cause = %v; want unexpected EOF", ce.Err)
	}
}

func TestRecvEndsWhenTheConnIsClosed(t *testing.T) {
	path, join := fakeHerdr(t, func(c net.Conn) struct{} {
		serveWelcome(t, c)
		_, _ = io.Copy(io.Discard, c) // hold the socket open until the client closes
		return struct{}{}
	})
	conn := mustOpen(t, path)
	conn.SetReadTimeout(0)
	errc := make(chan error, 1)
	go func() { _, err := conn.Recv(); errc <- err }()
	time.Sleep(50 * time.Millisecond)
	_ = conn.Close()
	if err := <-errc; err == nil {
		t.Error("Recv returned no error after Close")
	}
	join()
}

// A scripted reader: each step is one read's outcome.
type step struct {
	bytes []byte
	err   error
	eof   bool
}

type script struct{ steps []step }

func (s *script) Read(buf []byte) (int, error) {
	if len(s.steps) == 0 {
		return 0, io.EOF
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	switch {
	case st.err != nil:
		return 0, st.err
	case st.eof:
		return 0, io.EOF
	}
	n := copy(buf, st.bytes)
	if n < len(st.bytes) {
		s.steps = append([]step{{bytes: st.bytes[n:]}}, s.steps...)
	}
	return n, nil
}

// framed is [u32 LE len][payload] as bytes.
func framed(payload []byte) []byte {
	var wire bytes.Buffer
	_ = WriteFrame(&wire, payload)
	return wire.Bytes()
}

func data(b []byte) step { return step{bytes: b} }

func TestAFrameSplitAcrossTimeoutsIsAssembled(t *testing.T) {
	wire := framed([]byte("hello"))
	r := &script{[]step{
		data(wire[:2]),
		{err: os.ErrDeadlineExceeded},
		data(wire[2:6]),
		{err: os.ErrDeadlineExceeded},
		data(wire[6:]),
	}}
	var in inbox
	for i := range 2 {
		if p, err := readPayload(r, &in); p != nil || err != nil {
			t.Fatalf("read %d = %q, %v; want a timeout", i, p, err)
		}
	}
	if p, err := readPayload(r, &in); err != nil || string(p) != "hello" {
		t.Errorf("read = %q, %v; want hello", p, err)
	}
}

func TestTwoFramesInOneReadComeOutInOrder(t *testing.T) {
	r := &script{[]step{data(append(framed([]byte("one")), framed([]byte("two"))...))}}
	var in inbox
	for _, want := range []string{"one", "two"} {
		if p, err := readPayload(r, &in); err != nil || string(p) != want {
			t.Errorf("read = %q, %v; want %s", p, err, want)
		}
	}
}

func TestInterruptedReadsAreRetriedByTheInbox(t *testing.T) {
	r := &script{[]step{{err: syscall.EINTR}, data(framed([]byte("ok")))}}
	if p, err := readPayload(r, &inbox{}); err != nil || string(p) != "ok" {
		t.Errorf("read = %q, %v; want ok", p, err)
	}
}

func TestEOFBetweenFramesIsAnUnexpectedEOF(t *testing.T) {
	_, err := readPayload(&script{[]step{{eof: true}}}, &inbox{})
	if ce := connErr(t, err, IOError); !errors.Is(ce, io.ErrUnexpectedEOF) {
		t.Errorf("cause = %v", ce.Err)
	}
}

func TestEOFInsideAFrameIsAnUnexpectedEOF(t *testing.T) {
	r := &script{[]step{data(framed([]byte("hello"))[:3]), {eof: true}}}
	_, err := readPayload(r, &inbox{})
	if ce := connErr(t, err, IOError); !errors.Is(ce, io.ErrUnexpectedEOF) {
		t.Errorf("cause = %v", ce.Err)
	}
}

func TestOversizedHeaderIsRefusedBeforeTheBody(t *testing.T) {
	r := &script{[]step{data(binary.LittleEndian.AppendUint32(nil, MaxFrame+1))}}
	_, err := readPayload(r, &inbox{})
	if ce := connErr(t, err, IOError); !errors.Is(ce, ErrFrameTooLarge) {
		t.Errorf("cause = %v", ce.Err)
	}
}
