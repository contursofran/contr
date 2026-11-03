// Adapter from endpoint types to the terminal view's [term] types. The cell,
// geometry and key types are shared, so only the surface, the input events
// and the link need conversion. Rows are shared, not copied: they are never
// changed in place.
// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"errors"
	"fmt"
	"slices"

	"github.com/contursofran/contr/internal/term"
)

// ToScreen converts a surface to a screen. The rows are shared and the panes
// are copied. A nil surface gives nil.
func ToScreen(s *Surface) *term.Screen {
	if s == nil {
		return nil
	}
	out := &term.Screen{
		BootID:             s.BootID,
		ProjectionRevision: s.ProjectionRevision,
		Revision:           s.Revision,
		Size:               s.Size,
		Rows:               slices.Clone(s.Rows),
		Cursor:             s.Cursor,
	}
	if s.Panes != nil {
		out.Panes = make([]term.Pane, len(s.Panes))
		for i, p := range s.Panes {
			out.Panes[i] = term.Pane(p)
		}
	}
	return out
}

// FromScreen converts a screen to a surface, the reverse of [ToScreen].
func FromScreen(s *term.Screen) *Surface {
	if s == nil {
		return nil
	}
	out := &Surface{
		BootID:             s.BootID,
		ProjectionRevision: s.ProjectionRevision,
		Revision:           s.Revision,
		Size:               s.Size,
		Rows:               slices.Clone(s.Rows),
		Cursor:             s.Cursor,
	}
	if s.Panes != nil {
		out.Panes = make([]SurfacePane, len(s.Panes))
		for i, p := range s.Panes {
			out.Panes[i] = SurfacePane(p)
		}
	}
	return out
}

// toInput converts one input event. An unknown event is an error.
func toInput(e term.InputEvent) (InputEvent, error) {
	switch e := e.(type) {
	case term.KeyEvent:
		return KeyEvent(e), nil
	case term.TextEvent:
		return TextEvent(e), nil
	case term.PasteEvent:
		return PasteEvent(e), nil
	case term.WheelEvent:
		return WheelEvent(e), nil
	}
	return nil, fmt.Errorf("unknown input event %T", e)
}

func toInputs(events []term.InputEvent) ([]InputEvent, error) {
	out := make([]InputEvent, len(events))
	for i, e := range events {
		c, err := toInput(e)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// toUpdate converts a link update for the view. An unknown update gives nil.
func toUpdate(u LinkUpdate) term.LinkUpdate {
	switch u := u.(type) {
	case StateUpdate:
		return term.StateUpdate{State: u.State}
	case SurfaceUpdate:
		return term.ScreenUpdate{Screen: ToScreen(u.Surface)}
	case SnapshotUpdate:
		if u.Snapshot == nil {
			return term.SnapshotUpdate{}
		}
		return term.SnapshotUpdate{Snapshot: &term.Snapshot{
			BootID:        u.Snapshot.BootID,
			Revision:      u.Snapshot.Revision,
			FocusedPaneID: u.Snapshot.FocusedPaneID,
		}}
	case ClipboardUpdate:
		return term.ClipboardUpdate{Text: u.Text}
	}
	return nil
}

// TermLink is the link to herdr as the terminal view uses it.
type TermLink interface {
	SendInput(paneID string, events []term.InputEvent) error
	Resize(size term.GridSize, cell term.CellPx) error
	SetFocus(focused bool) error
	TakeLatest() *term.Screen
}

// termHandle gives a [Handle] the shape of the view's link.
type termHandle struct{ h Handle }

func (t termHandle) SendInput(paneID string, events []term.InputEvent) error {
	in, err := toInputs(events)
	if err != nil {
		return err
	}
	return mapErr(t.h.SendInput(paneID, in))
}

func (t termHandle) Resize(size term.GridSize, cell term.CellPx) error {
	return mapErr(t.h.Resize(size, cell))
}

func (t termHandle) SetFocus(focused bool) error { return mapErr(t.h.SetFocus(focused)) }

func (t termHandle) TakeLatest() *term.Screen { return ToScreen(t.h.TakeLatest()) }

func mapErr(err error) error {
	if errors.Is(err, ErrLinkNotConnected) {
		return term.ErrNotConnected
	}
	return err
}

// SpawnTermLink starts the surface link to the local herdr. It returns the
// view's link and a function that stops it. It never fails (D19).
func SpawnTermLink(size term.GridSize, cell term.CellPx, notify func(term.LinkUpdate)) (TermLink, func(), error) {
	l, h := SpawnLink(LinkTarget{}, size, cell, func(u LinkUpdate) {
		if c := toUpdate(u); c != nil {
			notify(c)
		}
	})
	return termHandle{h}, l.Stop, nil
}
