package app

import (
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// orderWindow is a fakeWindow that logs when the visibility watch and the terminal's focus watch
// end.
type orderWindow struct {
	*fakeWindow
	log *[]string
}

func (w orderWindow) OnShow(fn func()) func() {
	off := w.fakeWindow.OnShow(fn)
	return func() {
		// The menu handlers clear before any other part stops: a menu item must not reach a
		// stopped view.
		if slices.Contains(handlersSet(), true) {
			*w.log = append(*w.log, "handlers still set")
		}
		*w.log = append(*w.log, "unwatch")
		off()
	}
}

func (w orderWindow) OnFocus(fn func()) func() {
	off := w.fakeWindow.OnFocus(fn)
	return func() { *w.log = append(*w.log, "term"); off() }
}

// handlersSet tells which menu handlers are set, in handler id order.
func handlersSet() []bool {
	var set []bool
	for id := range handlerCount {
		set = append(set, handlers[id].Load() != nil)
	}
	return set
}

func hasLayer(o *TerminalOverlay, name string) bool {
	return slices.ContainsFunc(o.layers, func(l *overlayLayer) bool { return l.name == name })
}

// The stop of Attach keeps the order of the parts (#570): the menu handlers clear first, then the
// visibility watch, the terminal, the outline and the agent state end, and the sidebar list
// closes last.
func TestAttachStopOrder(t *testing.T) {
	var log []string
	w := orderWindow{fakeWindow: &fakeWindow{visible: true}, log: &log}
	f := NewFrame(Deps{
		OpenTerminal: func(string, string) (string, error) { return "", nil },
	})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	f.deps.StartAgents = func(func()) (AgentSource, func()) {
		src := &source{snap: agentstate.Snapshot{Workers: []agents.Worker{{Name: "b1", Address: "a", Live: true}}}}
		return src, func() {
			log = append(log, "agents")
			if hasLayer(&f.Overlay, LabelOutline) {
				t.Error("the agent state stopped before the outline was removed")
			}
			if f.SpacesList.closed {
				t.Error("the sidebar list closed before the agent state stopped")
			}
		}
	}
	stop := f.Attach(w)
	if got := handlersSet(); slices.Contains(got, false) {
		t.Fatalf("after Attach the menu handlers set are %v, want all", got)
	}
	if !hasLayer(&f.Overlay, LabelOutline) {
		t.Fatal("Attach should add the outline layer")
	}
	stop()
	if got := handlersSet(); slices.Contains(got, true) {
		t.Errorf("after stop the menu handlers set are %v, want none", got)
	}
	if want := []string{"unwatch", "term", "agents"}; !slices.Equal(log, want) {
		t.Errorf("the stop order is %v, want %v", log, want)
	}
	if !f.SpacesList.closed {
		t.Error("stop should close the sidebar list")
	}
}
