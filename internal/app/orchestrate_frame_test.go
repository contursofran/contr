package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/orchestrate"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/state"
)

// The frame puts the orchestrate pill under the notch, and its send goes to the selected session
// through the agent source (D234).
func TestFramePillSendsToTheSelectedSession(t *testing.T) {
	r := attachCounting(t)
	names := r.f.Overlay.Names()
	pill, notch := slices.Index(names, orchestrate.LabelLayer), slices.Index(names, questions.LabelNotchLayer)
	if pill < 0 || notch < 0 || pill > notch {
		t.Fatalf("layers %v: want the pill layer before the notch layer", names)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "ob"})
	r.w.flush()
	r.tt.Frame()
	if err := r.tt.Click(orchestrate.LabelButton); err != nil {
		t.Fatal(err)
	}
	r.tt.Type("ship it")
	r.tt.Key(0, ui.KeyEnter)
	r.w.flush()
	r.tt.Frame()
	if want := [][2]string{{"ob", "/contr:orchestrate ship it"}}; !slices.Equal(r.src.Told, want) {
		t.Errorf("sent %v, want %v", r.src.Told, want)
	}
	if !apptest.Absent(r.tt, orchestrate.LabelPopover) {
		t.Error("the popover stays open after the send")
	}
}
