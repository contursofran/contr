//go:build hccore

// This test runs against a real herdr-core, isolated from the owner's core:
// its own HC_SESSION and scratch XDG folders. Run it with
//
//	go test -tags hccore ./internal/hc
package hc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	dir, err := os.MkdirTemp("/tmp", "hc")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"HC_SESSION=" + fmt.Sprintf("t%d", os.Getpid())}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME"} {
		p := dir + "/" + strings.ToLower(v[4:6])
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, v+"="+p)
	}
	t.Cleanup(func() {
		cmd := exec.Command("hc", "server", "stop")
		cmd.Env = append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop the scratch core: %v: %s", err, out)
		}
		_ = os.RemoveAll(dir)
	})
	return env
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
