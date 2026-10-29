// Fake endpoint servers for the tests of Conn and the link. They run in this
// process on a socket in a temporary folder. They never touch herdr.
package endpoint

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// shortTempDir makes a folder in /tmp with a short name. A socket path must
// stay under about 104 bytes, and t.TempDir is too long on macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ce")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fakeHerdr listens on a socket in a short temp folder. A goroutine runs
// script on the first client that connects, then closes that connection.
// join waits for the script and returns its result. The cleanup closes the
// listener and the connection and waits for the goroutine, so none leaks.
func fakeHerdr[T any](t *testing.T, script func(c net.Conn) T) (path string, join func() T) {
	t.Helper()
	path = filepath.Join(shortTempDir(t), "herdr-client.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	done := make(chan struct{})
	accepted := make(chan net.Conn, 1)
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return // closed by the cleanup before a client came
		}
		accepted <- c
		defer c.Close()
		result = script(c)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case c := <-accepted:
			_ = c.Close()
		default:
		}
		<-done
	})
	return path, func() T {
		t.Helper()
		<-done
		return result
	}
}

// readHello reads the client's hello and checks its kind.
func readHello(t *testing.T, c net.Conn) {
	t.Helper()
	payload, err := ReadFrame(c)
	if err != nil {
		t.Errorf("read hello: %v", err)
		return
	}
	d := NewDec(payload)
	if tag, err := d.Vu(); err != nil || tag != CControl {
		t.Errorf("hello tag = %d, %v; want %d", tag, err, CControl)
	}
	if kind, err := d.Str(); err != nil || kind != HelloKind {
		t.Errorf("hello kind = %q, %v; want %q", kind, err, HelloKind)
	}
}

// put writes one frame.
func put(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	if err := WriteFrame(c, payload); err != nil {
		t.Errorf("put: %v", err)
	}
}

// control is a server EndpointControl payload.
func control(kind, data string) []byte {
	var e Enc
	e.Vu(SControl)
	e.Str(kind)
	e.Str(data)
	return e.Buf
}

func welcomeBody(generation uint32, version, extra string) string {
	return fmt.Sprintf(`{"generation":%d,"server_version":%q,"snapshot_codec":%q,"surface_codec":%q,"input_codec":%q,"blob_codec":%q%s}`,
		generation, version, SnapshotCodec, SurfaceCodec, InputCodec, BlobCodec, extra)
}

// welcome is the v1 welcome as herdr sends it after a good hello.
func welcome(version string) []byte {
	return control(WelcomeKind, welcomeBody(1, version, ""))
}

// legacyWelcome is herdr's pre-generation-1 answer: a legacy Welcome (tag 0).
func legacyWelcome(errText *string) []byte {
	var e Enc
	e.Vu(SWelcomeLegacy)
	e.Vu(1) // version
	e.Vu(0) // render encoding
	if errText == nil {
		e.None()
	} else {
		e.Some()
		e.Str(*errText)
	}
	return e.Buf
}
