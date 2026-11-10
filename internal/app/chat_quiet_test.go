package app

import (
	"testing"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// Deps.RunOnMain is the quiet post of the views' Env: the chat's transcript signals go through it
// and ask for no redraw (#660, D257). Without it, a quiet post is a Window.Update.
func TestFrameQuietPostIsRunOnMain(t *testing.T) {
	for _, quiet := range []bool{true, false} {
		ran := 0
		deps := Deps{}
		if quiet {
			deps.RunOnMain = func(fn func()) { ran++; fn() }
		}
		f := NewFrame(deps)
		f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
		f.Usage = usage.NewSampler(usage.Unread, nil)
		w := &fakeWindow{visible: true}
		stop := f.Attach(w)
		w.flush()
		queued, redraws := w.queued(), w.redraws()
		done := false
		f.Chat.Env.PostQuiet(func() { done = true })
		switch {
		case quiet && (ran != 1 || !done || w.queued() != queued || w.redraws() != redraws):
			t.Errorf("RunOnMain: ran %d, done %v, updates %d, redraws %d; want it alone", ran, done, w.queued()-queued, w.redraws()-redraws)
		case !quiet && w.queued() != queued+1:
			t.Errorf("no RunOnMain: %d updates queued, want 1", w.queued()-queued)
		}
		w.flush()
		stop()
	}
}
