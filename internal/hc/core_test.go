//go:build hccore

// This test runs against a real herdr-core, isolated from the owner's core:
// its own HC_SESSION and scratch XDG folders. Run it with
//
//	go test -tags hccore ./internal/hc
package hc

import (
	"context"
	"encoding/json"
	"github.com/contursofran/contr/internal/hc/hctest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// isolatedCore returns the environment of a scratch core and stops that core
// when the test ends. The folder is short, because the socket path has a
// length limit.
func isolatedCore(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc is not installed")
	}
	return hctest.New(t, "hc", hctest.Session("t")).Env
}

func TestRealCoreRunSendAndEvents(t *testing.T) {
	env := isolatedCore(t)
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cl, err := Dial(c, Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	labels := map[string]string{"app": "contr-test"}
	s, err := cl.Subscribe(c, Filter{Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var run struct {
		Address string `json:"address"`
	}
	err = cl.Call(c, "terminal.run", map[string]any{
		"argv":      []string{"sh", "-c", "read x; echo got $x"},
		"name":      "w1",
		"labels":    labels,
		"wait":      false,
		"no_parent": true,
	}, &run)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.Call(c, "terminal.run", map[string]any{"argv": []string{"true"}, "name": "w1", "wait": false, "no_parent": true}, nil); !IsKind(err, "name_taken") {
		t.Errorf("second w1: err = %v, want name_taken", err)
	}
	if err := cl.Call(c, "terminal.send", map[string]any{"terminal": "w1", "text": "hello"}, nil); err != nil {
		t.Fatal(err)
	}

	created, finished := false, false
	for !finished {
		select {
		case e, ok := <-s.Events():
			if !ok {
				t.Fatalf("events ended: %v", s.Err())
			}
			if e.Terminal != run.Address {
				continue
			}
			created = created || e.Type == "created"
			finished = e.Type == "finished"
		case <-c.Done():
			t.Fatal("no finished event")
		}
	}
	if !created {
		t.Error("no created event")
	}

	var read struct {
		Text string `json:"text"`
	}
	if err := cl.Call(c, "terminal.read", map[string]any{"terminal": "w1"}, &read); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Text, "got hello") {
		t.Errorf("screen = %q", read.Text)
	}

	var list struct {
		Terminals []json.RawMessage `json:"terminals"`
	}
	if err := cl.Call(c, "terminal.list", map[string]any{"all": true, "labels": labels}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Terminals) != 1 {
		t.Errorf("list = %d terminals, want 1", len(list.Terminals))
	}
}

func TestRealCoreFramedAttach(t *testing.T) {
	env := isolatedCore(t)
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cl, err := Dial(c, Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var run struct {
		Address string `json:"address"`
	}
	err = cl.Call(c, "terminal.run", map[string]any{
		"argv": []string{"sh"}, "name": "w1", "wait": false, "no_parent": true,
	}, &run)
	if err != nil {
		t.Fatal(err)
	}

	fc, err := DialFramed(c, Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer fc.Close()
	s, err := fc.Attach(c, AttachParams{Terminal: run.Address, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Write([]byte("echo h''i\r")); err != nil {
		t.Fatal(err)
	}
	var seen []byte
	for !strings.Contains(strings.ReplaceAll(string(seen), "echo h''i", ""), "hi") {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("stream ended: %v; output %q", s.Err(), seen)
			}
			seen = append(seen, ev.Data...)
			t.Logf("event: data %q resize %v", ev.Data, ev.Resize)
		case <-c.Done():
			t.Fatalf("no output with hi; got %q", seen)
		}
	}
}
