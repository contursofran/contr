// Package view holds what each view of the window gets from the frame: Env,
// Runner and Latest. The feature packages under internal/app import it
// (issue #571).
package view

// Env is what a view gets from the window: a runner for work off the UI
// thread, a redraw, a short message and a web address opener. app.Frame.Attach
// makes one Env and gives it to each view; a view keeps it in a field named
// Env and calls its methods, never the fields, so it needs no nil checks.
//
// The zero value works in tests: Do runs the work and its result at once,
// and Redraw, Show and Open do nothing.
type Env struct {
	// Run runs work off the UI thread and posts the result to it. A zero
	// Run makes Do run both at once.
	Run Runner
	// Invalidate asks the window for a redraw (Window.Invalidate).
	Invalidate func()
	// Status shows a short message as a toast (D126).
	Status func(string)
	// OpenURL opens a web address in the default browser.
	OpenURL func(string)
}

// Do runs work off the UI thread through Run. The function that work
// returns runs on the UI thread. With a zero Run, both run at once.
func (e Env) Do(work func() func()) {
	if e.Run.Go == nil && e.Run.Post == nil {
		if done := work(); done != nil {
			done()
		}
		return
	}
	e.Run.Do(work)
}

// Post runs fn on the UI thread. Without Run.Post it runs fn at once.
func (e Env) Post(fn func()) {
	if e.Run.Post == nil {
		fn()
		return
	}
	e.Run.Post(fn)
}

// Redraw asks for a new frame.
func (e Env) Redraw() {
	if e.Invalidate != nil {
		e.Invalidate()
	}
}

// Show shows msg as a short message.
func (e Env) Show(msg string) {
	if e.Status != nil {
		e.Status(msg)
	}
}

// Open opens the web address u.
func (e Env) Open(u string) {
	if e.OpenURL != nil {
		e.OpenURL(u)
	}
}
