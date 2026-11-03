package endpoint

import (
	"errors"
	"reflect"
	"testing"

	"github.com/contursofran/contr/internal/term"
)

func fullSurface() *Surface {
	return &Surface{
		BootID:             "boot",
		ProjectionRevision: 3,
		Revision:           7,
		Size:               GridSize{Cols: 2, Rows: 2},
		Rows: []Row{
			{{Text: "a", Fg: 1, Bg: 2, Attrs: 5}, {Text: "好", Fg: 0x02ff0000, Attrs: 256}},
			{{Text: "", WideTail: true}, {Text: "z", Bg: 9}},
		},
		Cursor: &Cursor{X: 1, Y: 1, Visible: true, Shape: 5},
		Panes: []SurfacePane{
			{
				PaneID: "p1", Rect: Rect{X: 1, Y: 2, Width: 3, Height: 4},
				InnerRect: Rect{X: 2, Y: 3, Width: 1, Height: 2},
				Focused:   true, AlternateScreen: true, MouseReporting: true,
				Scroll: &ScrollMetrics{OffsetFromBottom: 1, MaxOffsetFromBottom: 8, ViewportRows: 2},
			},
			{PaneID: "p2"},
		},
	}
}

// Every field of a surface survives the trip to a screen and back. The test
// fails if the adapter drops a field.
func TestSurfaceScreenRoundTrip(t *testing.T) {
	s := fullSurface()
	scr := ToScreen(s)
	if scr.BootID != "boot" || scr.ProjectionRevision != 3 || scr.Revision != 7 || scr.Size != s.Size {
		t.Errorf("header: %+v", scr)
	}
	if !reflect.DeepEqual(scr.Rows, s.Rows) || !reflect.DeepEqual(scr.Cursor, s.Cursor) {
		t.Errorf("rows or cursor changed: %+v", scr)
	}
	want := term.Pane{
		PaneID: "p1", Rect: term.Rect{X: 1, Y: 2, Width: 3, Height: 4},
		InnerRect: term.Rect{X: 2, Y: 3, Width: 1, Height: 2},
		Focused:   true, AlternateScreen: true, MouseReporting: true,
		Scroll: &term.ScrollMetrics{OffsetFromBottom: 1, MaxOffsetFromBottom: 8, ViewportRows: 2},
	}
	if !reflect.DeepEqual(scr.Panes[0], want) || scr.Panes[1].PaneID != "p2" || scr.Panes[1].Scroll != nil {
		t.Errorf("panes: %+v", scr.Panes)
	}
	if scr.FocusedPane() == nil || scr.FocusedPane().PaneID != "p1" {
		t.Errorf("focused pane: %+v", scr.FocusedPane())
	}
	if back := FromScreen(scr); !reflect.DeepEqual(back, s) {
		t.Errorf("round trip:\n got %+v\nwant %+v", back, s)
	}
}

func TestToScreenNilAndEmpty(t *testing.T) {
	if ToScreen(nil) != nil || FromScreen(nil) != nil {
		t.Error("nil must stay nil")
	}
	if got := ToScreen(&Surface{}); got.Panes != nil || got.Cursor != nil {
		t.Errorf("empty surface: %+v", got)
	}
}

// The screen has its own pane slice and row list: a patch that changes the
// surface in place must not reach it. Rows are shared, as they never change.
func TestToScreenCopiesPanesAndRowList(t *testing.T) {
	s := fullSurface()
	scr := ToScreen(s)
	s.Panes[0].PaneID = "changed"
	s.Rows[0] = Row{{Text: "new"}}
	if scr.Panes[0].PaneID != "p1" || scr.Rows[0][0].Text != "a" {
		t.Errorf("screen followed the surface: %+v", scr)
	}
}

func TestInputEventsConvert(t *testing.T) {
	in := []term.InputEvent{
		term.KeyEvent{Code: term.CharKey('x'), Mods: term.ModAlt, Repeat: true},
		term.TextEvent{Text: "t"},
		term.PasteEvent{Text: "p"},
		term.WheelEvent{Dir: term.WheelLeft, Column: 1, Row: 2, Mods: term.ModShift, Lines: 3},
	}
	want := []InputEvent{
		KeyEvent{Code: CharKey('x'), Mods: ModAlt, Repeat: true},
		TextEvent{Text: "t"},
		PasteEvent{Text: "p"},
		WheelEvent{Dir: WheelLeft, Column: 1, Row: 2, Mods: ModShift, Lines: 3},
	}
	got, err := toInputs(in)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v", got, err)
	}
}

type strangeEvent struct{ term.TextEvent }

func TestUnknownInputEventIsAnError(t *testing.T) {
	if _, err := toInputs([]term.InputEvent{strangeEvent{}}); err == nil {
		t.Error("unknown event accepted")
	}
}

func TestUpdatesConvert(t *testing.T) {
	p := "p9"
	st := LinkState{Kind: LinkLive, ServerVersion: "1", Untested: true}
	cases := []struct {
		in   LinkUpdate
		want term.LinkUpdate
	}{
		{StateUpdate{State: st}, term.StateUpdate{State: st}},
		{ClipboardUpdate{Text: "c"}, term.ClipboardUpdate{Text: "c"}},
		{SnapshotUpdate{}, term.SnapshotUpdate{}},
		{
			SnapshotUpdate{Snapshot: &ShellSnapshot{BootID: "b", Revision: 4, FocusedPaneID: &p}},
			term.SnapshotUpdate{Snapshot: &term.Snapshot{BootID: "b", Revision: 4, FocusedPaneID: &p}},
		},
		{SurfaceUpdate{}, term.ScreenUpdate{}},
	}
	for _, c := range cases {
		if got := toUpdate(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%T: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

// A link that is not connected gives the view's own error.
func TestNotConnectedMapsToTermError(t *testing.T) {
	h := termHandle{Handle{s: &shared{}}}
	if err := h.SetFocus(true); !errors.Is(err, term.ErrNotConnected) {
		t.Errorf("SetFocus: %v", err)
	}
	if err := h.Resize(GridSize{Cols: 1, Rows: 1}, CellPx{}); !errors.Is(err, term.ErrNotConnected) {
		t.Errorf("Resize: %v", err)
	}
	if err := h.SendInput("p", []term.InputEvent{term.TextEvent{Text: "x"}}); !errors.Is(err, term.ErrNotConnected) {
		t.Errorf("SendInput: %v", err)
	}
	if err := h.SendInput("p", []term.InputEvent{strangeEvent{}}); err == nil || errors.Is(err, term.ErrNotConnected) {
		t.Errorf("unknown event: %v", err)
	}
}
