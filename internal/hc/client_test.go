package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeConn is the core's side of one connection.
type fakeConn struct {
	t   *testing.T
	in  *bufio.Scanner
	out io.WriteCloser
	mu  sync.Mutex
}

type fakeReq struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// next returns the next request, or false when the client closed.
func (f *fakeConn) next() (fakeReq, bool) {
	if !f.in.Scan() {
		return fakeReq{}, false
	}
	var r fakeReq
	if err := json.Unmarshal(f.in.Bytes(), &r); err != nil {
		f.t.Errorf("fake core: bad request %q: %v", f.in.Text(), err)
		return fakeReq{}, false
	}
	return r, true
}

func (f *fakeConn) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Errorf("fake core: %v", err)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _ = f.out.Write(append(b, '\n'))
}

func (f *fakeConn) reply(id uint64, result any) {
	f.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (f *fakeConn) replyErr(id uint64, kind, msg, hint string) {
	f.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{
		"code": -32000, "message": msg, "data": map[string]any{"kind": kind, "hint": hint}}})
}

func (f *fakeConn) notify(method string, params any) {
	f.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// hello answers the handshake with generation gen.
func (f *fakeConn) hello(gen int) bool {
	r, ok := f.next()
	if !ok {
		return false
	}
	if r.Method != "hello" {
		f.t.Errorf("first method %q, want hello", r.Method)
	}
	f.reply(r.ID, map[string]any{"generation": gen, "core": map[string]any{"id": "c1", "name": "test"}})
	return true
}

// fakeDialer runs serve for each new connection.
func fakeDialer(t *testing.T, serve func(*fakeConn)) Dialer {
	return func(ctx context.Context) (Pipe, error) {
		reqR, reqW := io.Pipe()
		repR, repW := io.Pipe()
		f := &fakeConn{t: t, in: bufio.NewScanner(reqR), out: repW}
		go func() {
			serve(f)
			_ = repW.Close()
		}()
		return Pipe{W: reqW, R: repR, Close: func() error {
			_ = reqW.Close()
			_ = repR.Close()
			return nil
		}}, nil
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestDialRunsTheHandshake(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) { f.hello(1); f.next() }))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if cl.Hello.Core.ID != "c1" || cl.Hello.Generation != 1 {
		t.Errorf("hello = %+v", cl.Hello)
	}
}

func TestDialRefusesAnotherGeneration(t *testing.T) {
	_, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) { f.hello(2); f.next() }))
	if err == nil {
		t.Fatal("Dial with generation 2: no error")
	}
}

func TestDialReportsADialError(t *testing.T) {
	want := errors.New("no hc")
	_, err := Dial(ctx(t), func(context.Context) (Pipe, error) { return Pipe{}, want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestCallDecodesResultAndErrors(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		for {
			r, ok := f.next()
			if !ok {
				return
			}
			switch r.Method {
			case "terminal.get":
				f.reply(r.ID, map[string]any{"address": "t7", "params": r.Params})
			case "terminal.send":
				f.replyErr(r.ID, "not_found", `no terminal "x"`, "see `hc ls --all`")
			case "terminal.kill":
				f.reply(r.ID, nil)
			}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	var got struct {
		Address string          `json:"address"`
		Params  json.RawMessage `json:"params"`
	}
	if err := cl.Call(ctx(t), "terminal.get", map[string]string{"terminal": "t7"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Address != "t7" || string(got.Params) != `{"terminal":"t7"}` {
		t.Errorf("result = %+v", got)
	}

	err = cl.Call(ctx(t), "terminal.send", map[string]string{"terminal": "x", "text": "hi"}, nil)
	var e *Error
	if !errors.As(err, &e) || e.Kind != "not_found" || e.Hint == "" || !IsKind(err, "not_found") {
		t.Fatalf("err = %#v", err)
	}
	if IsKind(err, "blocked") || IsKind(errors.New("x"), "not_found") {
		t.Error("IsKind matches another kind")
	}

	if err := cl.Call(ctx(t), "terminal.kill", map[string]string{"terminal": "t7"}, nil); err != nil {
		t.Errorf("null result: %v", err)
	}
}

func TestConcurrentCallsMatchTheirReplies(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		var held []fakeReq
		for len(held) < 3 {
			r, ok := f.next()
			if !ok {
				return
			}
			held = append(held, r)
		}
		for i := len(held) - 1; i >= 0; i-- { // reply in reverse order
			f.reply(held[i].ID, string(held[i].Params))
		}
		f.next()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want, _ := json.Marshal(map[string]int{"n": i})
			var got string
			if err := cl.Call(ctx(t), "echo", map[string]int{"n": i}, &got); err != nil {
				t.Error(err)
				return
			}
			if got != string(want) {
				t.Errorf("call %d got %s", i, got)
			}
		}()
	}
	wg.Wait()
}

func TestPendingCallEndsWhenTheCoreGoesAway(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		f.next() // read the call, then end without a reply
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	// The call was sent: it may have run, so it is not ErrNotSent.
	if err := cl.Call(ctx(t), "terminal.wait", nil, nil); !errors.Is(err, ErrClosed) || errors.Is(err, ErrNotSent) {
		t.Fatalf("err = %v, want ErrClosed and not ErrNotSent", err)
	}
	// A call after the end was never sent.
	if err := cl.Call(ctx(t), "terminal.list", nil, nil); !errors.Is(err, ErrNotSent) || !errors.Is(err, ErrClosed) {
		t.Fatalf("call after end: %v, want ErrNotSent", err)
	}
}

func TestCallStopsOnContextAndAfterClose(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		for {
			if _, ok := f.next(); !ok {
				return
			}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := cl.Call(c, "terminal.wait", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline", err)
	}
	if err := cl.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cl.Call(ctx(t), "terminal.list", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestABadLineEndsTheConnection(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		if _, ok := f.next(); !ok {
			return
		}
		_, _ = f.out.Write([]byte("not json\n"))
		f.next()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if err := cl.Call(ctx(t), "terminal.list", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestCloseTwice(t *testing.T) {
	closes := 0
	base := fakeDialer(t, func(f *fakeConn) { f.hello(1); f.next() })
	dial := func(c context.Context) (Pipe, error) {
		p, err := base(c)
		inner := p.Close
		p.Close = func() error { closes++; return inner() }
		return p, err
	}
	cl, err := Dial(ctx(t), dial)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cl.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if closes != 1 {
		t.Errorf("pipe closed %d times, want 1", closes)
	}
}

func fakeHC(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "hc")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindExplicitPathWins(t *testing.T) {
	if got, err := Find("/x/hc", t.TempDir(), nil); err != nil || got != "/x/hc" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFindOnPath(t *testing.T) {
	dir := t.TempDir()
	want := fakeHC(t, dir)
	t.Setenv("PATH", dir)
	got, err := Find("", t.TempDir(), []string{})
	if err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestFindInFallbackDir(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := fakeHC(t, bin)
	got, err := Find("", home, []string{"/no/such/dir", bin})
	if err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestFindDefaultDirsIncludeLocalBin(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := fakeHC(t, bin)
	if got, err := Find("", home, nil); err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestFindNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find("", t.TempDir(), []string{t.TempDir()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v, want ErrNotFound", err)
	}
}

func TestFindSkipsANonExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hc"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Find("", t.TempDir(), []string{dir}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v, want ErrNotFound", err)
	}
}

// Building a Dialer must not look for hc: that work runs on the UI thread.
// The lookup happens when the Dialer runs, so an hc that appears on PATH
// after Command is found, and a missing hc is an error of the Dialer.
func TestCommandLooksUpHcOnlyWhenDialing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir)
	dial := Command("", nil) // hc is not there yet
	if _, err := dial(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dial without hc = %v, want ErrNotFound", err)
	}
	script := filepath.Join(dir, "hc")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := dial(context.Background())
	if err != nil {
		t.Fatalf("dial after hc appeared: %v", err)
	}
	_ = p.Close()
}
