package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/orchestrate"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
)

// The frame puts the orchestrate pill in the title bar, left of the diff summary and Search, and
// the Chat | Terminal switch layer under the notch layer. The pill's send goes to the selected
// session through the agent source (D234, D253).
func TestFramePillSendsToTheSelectedSession(t *testing.T) {
	r := attachCounting(t)
	names := r.f.Overlay.Names()
	sw, notch := slices.Index(names, chatview.LabelSwitchLayer), slices.Index(names, questions.LabelNotchLayer)
	if sw < 0 || notch < 0 || sw > notch {
		t.Fatalf("layers %v: want the switch layer before the notch layer", names)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "ob"})
	r.w.flush()
	r.tt.Frame()
	pill, search := apptest.Box(t, r.tt, orchestrate.LabelButton), apptest.Box(t, r.tt, titlebar.LabelSearch)
	if pill.Y+pill.H > theme.TitleBarHeight || pill.X+pill.W > search.X {
		t.Errorf("pill %+v, Search %+v: want the pill in the title bar, left of Search", pill, search)
	}
	if drag := apptest.Box(t, r.tt, titlebar.LabelTitleDrag); drag.W <= 0 {
		t.Errorf("title drag %+v: the bar has no drag area", drag)
	}
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

// In a window narrower than FilesHideBelow the title bar's pill is compact: no text, the label
// stays (D253).
func TestFramePillIsCompactInANarrowWindow(t *testing.T) {
	r := attachCounting(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "ob"})
	r.w.flush()
	r.tt.Frame()
	if !hasExact(r.tt, orchestrate.TextOrchestrate) {
		t.Fatalf("wide window: no Orchestrate text; texts %v", r.tt.Texts())
	}
	r.tt.SetSize(theme.FilesHideBelow-1, theme.WindowHeight)
	r.tt.Frame()
	if apptest.Absent(r.tt, orchestrate.LabelButton) || hasExact(r.tt, orchestrate.TextOrchestrate) {
		t.Errorf("narrow window: want the compact button; texts %v", r.tt.Texts())
	}
}

// A worker terminal has no pill in the title bar.
func TestFrameHasNoPillForAWorker(t *testing.T) {
	r := attachCounting(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "wa"})
	r.w.flush()
	r.tt.Frame()
	if !apptest.Absent(r.tt, orchestrate.LabelButton) {
		t.Error("a worker terminal shows the pill")
	}
}

// hasExact tells whether a text or label of the last frame is s.
func hasExact(tt *ui.Tester, s string) bool {
	return slices.Contains(tt.Texts(), s)
}
