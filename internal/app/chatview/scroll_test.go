package chatview

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// turns are n turns of a session: a prompt "prompt N", then a long answer of Claude.
func turns(from, n int) []string {
	var out []string
	answer := strings.Repeat("The builder reads the issue, the code and the spec, then writes its steps. ", 12)
	for i := from; i < from+n; i++ {
		k := strconv.Itoa(i)
		out = append(out,
			`{"type":"user","uuid":"p`+k+`","timestamp":"2026-11-10T10:00:00Z","message":{"content":"prompt `+k+`"}}`,
			said("s"+k+"a", answer), said("s"+k+"b", answer), said("s"+k+"c", answer))
	}
	return out
}

// scrolledRig is a chat with 6 long turns, at its end.
func scrolledRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(turns(1, 6)...)
	r.tt.Frame()
	return r
}

// up scrolls the transcript up by dy points and lets the list settle.
func (r *rig) up(dy float32) {
	r.tt.Wheel(500, 400, 0, -dy)
	r.settle()
}

// settle lets a scroll end.
func (r *rig) settle() {
	r.tt.Frame()
	r.tt.Advance(time.Second)
}

func (r *rig) pinnedText() (string, bool) {
	for _, s := range []string{"1", "2", "3", "4", "5", "6"} {
		if r.has(LabelPinned + " prompt " + s) {
			return "prompt " + s, true
		}
	}
	return "", false
}

func TestPinnedRow(t *testing.T) {
	rows := []Row{{Kind: RowPrompt}, {Kind: RowSaid}, {Kind: RowGroup}, {Kind: RowPrompt}, {Kind: RowSaid}}
	for first, want := range map[int]int{-1: -1, 0: -1, 1: 0, 2: 0, 3: -1, 4: 3, 5: -1} {
		if got := PinnedRow(rows, first); got != want {
			t.Errorf("PinnedRow(first %d) = %d, want %d", first, got, want)
		}
	}
	if got := PinnedRow([]Row{{Kind: RowSaid}, {Kind: RowSaid}}, 1); got != -1 {
		t.Errorf("no prompt above: %d, want -1", got)
	}
}

// At the end of the chat nothing is pinned and no jump shows.
func TestAtTheEndNothingIsPinned(t *testing.T) {
	r := scrolledRig(t)
	if !r.ch.list.AtEnd() {
		t.Fatal("the chat does not start at its end")
	}
	if p, ok := r.pinnedText(); ok {
		t.Errorf("at the end, %q is pinned", p)
	}
	if r.has(TextJump) || r.has(TextNew) {
		t.Error("at the end, the jump shows")
	}
}

// Scrolled up, the prompt of the turn in view is pinned when it is above the visible part, and a
// click on it scrolls to the prompt.
func TestScrolledUpPinsThePromptOfTheTurnInView(t *testing.T) {
	r := scrolledRig(t)
	r.up(500)
	first, _ := r.ch.list.Visible()
	want := PinnedRow(r.ch.model.Rows(), first)
	if want < 0 {
		t.Fatalf("first row %d has no prompt above it", first)
	}
	text := r.ch.model.Entry(r.ch.model.Rows()[want].Entry).Text
	got, ok := r.pinnedText()
	if !ok || got != text {
		t.Fatalf("pinned %q (%v), want %q; first row %d", got, ok, text, first)
	}
	if !r.tt.HasText(TextYou) {
		t.Error("the pinned prompt has no You")
	}
	if err := r.tt.Click(LabelPinned + " " + text); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if first, _ := r.ch.list.Visible(); first != want {
		t.Errorf("after the click the first row is %d, want the prompt %d", first, want)
	}
	if p, ok := r.pinnedText(); ok {
		t.Errorf("the prompt shows, and %q is still pinned", p)
	}
}

// Off the end, Jump to latest shows; content that comes makes it say New messages; a click goes
// to the end, and the list follows again.
func TestJumpToLatest(t *testing.T) {
	r := scrolledRig(t)
	r.up(500)
	if !r.has(TextJump) || r.has(TextNew) {
		t.Fatalf("scrolled up: jump %v, new %v; texts %v", r.has(TextJump), r.has(TextNew), r.tt.Texts())
	}
	r.write(turns(1, 7)...)
	if !r.has(TextJump+", new messages") || !r.tt.HasText(TextNew) {
		t.Fatalf("content came: no New messages in %v", r.tt.Texts())
	}
	if err := r.tt.Click(TextJump + ", new messages"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !r.ch.list.AtEnd() || r.has(TextJump) || r.has(TextNew) {
		t.Fatalf("after the click: at end %v, jump %v", r.ch.list.AtEnd(), r.has(TextJump))
	}
	if p, ok := r.pinnedText(); ok {
		t.Errorf("at the end, %q is pinned", p)
	}
	// The list follows again: a new turn keeps the end in view.
	r.write(turns(1, 8)...)
	r.tt.Frame()
	if !r.ch.list.AtEnd() || !r.tt.HasText("prompt 8") {
		t.Errorf("the list does not follow after the jump: at end %v", r.ch.list.AtEnd())
	}
	// Scrolled up again, the jump starts without New messages.
	r.up(500)
	if !r.has(TextJump) || r.has(TextNew) {
		t.Errorf("scrolled up again: jump %v, new %v", r.has(TextJump), r.has(TextNew))
	}
}

// A scrolled chat at rest builds no frame (D55): the pin and the jump come from the list state.
// The window has no focus, so the composer's caret does not blink.
func TestScrolledChatAtRestBuildsNoFrame(t *testing.T) {
	r := scrolledRig(t)
	builds := 0
	r.tt.SetWindowHidden(true)
	r.tt = ui.NewTester(func(c *ui.Context) {
		builds++
		r.ch.View(c)
	}, 1000, 900)
	r.tt.SetFocused(false)
	r.tt.Frame()
	r.up(500)
	if _, ok := r.pinnedText(); !ok || !r.has(TextJump) {
		t.Fatal("nothing pinned, or no jump")
	}
	before := builds
	r.tt.Advance(3 * time.Second)
	if builds != before || r.tt.FramePending() {
		t.Errorf("a scrolled chat at rest built %d frames in 3 s, pending %v; want 0", builds-before, r.tt.FramePending())
	}
}
