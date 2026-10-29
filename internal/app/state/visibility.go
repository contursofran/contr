// Package state holds the shared app state: window visibility, the sidebar
// selection and the status bar message. Read and write it on the UI thread,
// except where a type says otherwise.
//
// Ported from the Rust contr modules visibility, selection and
// status_message. The GPUI entities and globals become plain values owned
// by the app.
package state

// Visibility tells whether the frame window is presented to the user.
// Background work follows it, so a minimized or hidden window costs no
// wake-ups. See D9 in docs/DECISIONS.md.
type Visibility struct {
	visible  bool
	onChange func(bool)
}

// NewVisibility returns a Visibility that is visible until a window says
// otherwise. onChange may be nil. It runs when the value changes.
func NewVisibility(onChange func(bool)) *Visibility {
	return &Visibility{visible: true, onChange: onChange}
}

// Visible reports the last value set.
func (v *Visibility) Visible() bool { return v.visible }

// Set stores the new value and calls onChange only when it changed.
func (v *Visibility) Set(visible bool) {
	if v.visible == visible {
		return
	}
	v.visible = visible
	if v.onChange != nil {
		v.onChange(visible)
	}
}

// WindowEvents is the part of a MyGo window that Watch needs. *mygo.Window
// has these methods.
type WindowEvents interface {
	IsVisible() bool
	OnShow(fn func()) (off func())
	OnHide(fn func()) (off func())
	OnMinimize(fn func()) (off func())
	OnRestore(fn func()) (off func())
}

// Watch sets the value from the window now, then follows its show, hide,
// minimize and restore events. It returns a function that stops the
// watching. A window covered by other windows sends no event (D9).
func (v *Visibility) Watch(w WindowEvents) (stop func()) {
	v.Set(w.IsVisible())
	offs := []func(){
		w.OnShow(func() { v.Set(true) }),
		w.OnHide(func() { v.Set(false) }),
		w.OnMinimize(func() { v.Set(false) }),
		w.OnRestore(func() { v.Set(w.IsVisible()) }),
	}
	return func() {
		for _, off := range offs {
			if off != nil {
				off()
			}
		}
	}
}
