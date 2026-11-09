package viewers

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

func TestWorkerPanelOutputIsTheLastRead(t *testing.T) {
	src := &apptest.Source{Snap: apptest.BoardSnap(), Text: "building"}
	sel := state.NewSelection(nil)
	sel.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	p := &WorkerPanel{Source: src, Selection: sel}
	if got := p.Output(); got != "" {
		t.Errorf("output %q before a read", got)
	}
	tt := ui.NewTester(p.View, theme.PanelWidth, theme.WindowHeight)
	tt.Frame()
	if got := p.Output(); got != "building" {
		t.Errorf("output %q, want building", got)
	}
	src.Text = "done"
	p.Sync()
	if got := p.Output(); got != "done" {
		t.Errorf("output %q after a sync, want done", got)
	}
}
