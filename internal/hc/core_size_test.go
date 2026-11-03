//go:build hccore

package hc

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRealCoreSecondViewerSize records C5 (D43): a second viewer with input
// sets the size of the terminal at attach, the first viewer gets no resize
// frame, and a later RESIZE of the first viewer does not change the size.
func TestRealCoreSecondViewerSize(t *testing.T) {
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
	if err := cl.Call(c, "terminal.run", map[string]any{"argv": []string{"sh"}, "name": "w1", "wait": false, "no_parent": true}, &run); err != nil {
		t.Fatal(err)
	}
	attach := func(cols, rows uint16) *Stream {
		fc, err := DialFramed(c, Command("", env))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fc.Close() })
		s, err := fc.Attach(c, AttachParams{Terminal: run.Address, Cols: cols, Rows: rows, ResizeNotifications: true})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	resizes := make(chan Size, 8)
	a := attach(80, 24)
	go func() {
		for ev := range a.Events() {
			if ev.Resize != nil {
				resizes <- *ev.Resize
			}
		}
	}()
	b := attach(120, 40)
	go func() {
		for range b.Events() {
		}
	}()
	time.Sleep(300 * time.Millisecond)
	_ = a.Resize(100, 30)
	time.Sleep(300 * time.Millisecond)
	_, _ = b.Write([]byte("stty size\r"))
	time.Sleep(500 * time.Millisecond)
	var rd struct {
		Text string `json:"text"`
	}
	if err := cl.Call(c, "terminal.read", map[string]any{"terminal": run.Address}, &rd); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rd.Text, "40 120") {
		t.Errorf("size after the second attach = %q, want 40 120", rd.Text)
	}
	select {
	case sz := <-resizes:
		t.Errorf("first viewer got a resize frame %v", sz)
	default:
	}
}
