package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/owner"
)

var sheetNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func sheetSource() *source {
	return &source{snap: agentstate.Snapshot{Questions: []owner.Question{
		{ID: "D1", Project: "a", From: "b-1", Title: "Which db?", Question: "Which database should it use?",
			Options: []string{"sqlite", "postgres"}, Recommended: recommended(1), Asked: sheetNow.Add(-5 * time.Minute)},
		{ID: "D2", Project: "a", Question: "Ship it?", Options: []string{"yes", "no"}},
	}}}
}

type sheetRig struct {
	t     *testing.T
	sheet *Sheet
	src   *source
	tt    *ui.Tester
}

func newSheetRig(t *testing.T, src *source) *sheetRig {
	t.Helper()
	r := &sheetRig{t: t, src: src}
	r.sheet = &Sheet{Source: src, Run: Runner{Go: func(f func()) { f() }}, now: func() time.Time { return sheetNow }}
	r.tt = ui.NewTester(func(c *ui.Context) {
		if c.Shortcut(ui.Cmd, ui.KeyJ) {
			r.sheet.Toggle()
		}
		ui.Column(c).Fill().Children(func() {
			r.sheet.Anchor = ui.Box(c).FillWidth().Height(0).Shrink(0)
			r.sheet.View(c)
			ui.Box(c).Grow(1).Label("terminal")
		})
	}, 1000, 700)
	r.tt.Frame()
	return r
}

func (r *sheetRig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

func (r *sheetRig) text(s string) bool { return slices.Contains(r.tt.Texts(), s) }

func TestSheetOpensAndClosesWithCmdJ(t *testing.T) {
	r := newSheetRig(t, sheetSource())
	if r.has(LabelSheet) {
		t.Fatal("sheet shows before ⌘J")
	}
	term, _ := r.tt.Find("terminal")
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if !r.has(LabelSheet) {
		t.Fatal("⌘J did not open the sheet")
	}
	// An overlay: the terminal keeps its size (D67).
	if now, _ := r.tt.Find("terminal"); now != term {
		t.Errorf("terminal moved from %v to %v", term, now)
	}
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if r.has(LabelSheet) {
		t.Error("⌘J did not close the sheet")
	}
}

func TestSheetEscapeClosesWithFocus(t *testing.T) {
	r := newSheetRig(t, sheetSource())
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Key(0, ui.KeyEscape)
	if r.has(LabelSheet) {
		t.Error("Esc did not close the focused sheet")
	}
}

func TestSheetShowsTheQuestion(t *testing.T) {
	r := newSheetRig(t, sheetSource())
	r.tt.Key(ui.Cmd, ui.KeyJ)
	for _, want := range []string{"D1  Which db?", "b-1  5m", "1 of 2", "Which database should it use?", SheetRecommend} {
		if !r.text(want) {
			t.Errorf("missing %q in %q", want, r.tt.Texts())
		}
	}
	if r.has("Ask principal") || r.has("Show in terminal") {
		t.Error("the sheet shows an action that does not exist")
	}
}

func TestSheetPagesAndStopsAtTheEnds(t *testing.T) {
	r := newSheetRig(t, sheetSource())
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Click(SheetPrev) // at the start: nothing
	if !r.text("1 of 2") {
		t.Errorf("previous at the start moved: %q", r.tt.Texts())
	}
	r.tt.Click(SheetNext)
	r.tt.Frame()
	if !r.text("2 of 2") || !r.text("Ship it?") {
		t.Errorf("next did not page: %q", r.tt.Texts())
	}
	r.tt.Click(SheetNext) // at the end: nothing
	if !r.text("2 of 2") {
		t.Errorf("next at the end moved: %q", r.tt.Texts())
	}
	r.tt.Click(SheetPrev)
	if !r.text("1 of 2") {
		t.Errorf("previous did not page back: %q", r.tt.Texts())
	}
}

func TestSheetAnswerReachesTheSourceAndFailureStays(t *testing.T) {
	src := sheetSource()
	r := newSheetRig(t, src)
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Click(SheetAnswer + " postgres")
	if len(src.answers) != 1 || src.answers[0] != [2]string{"D1", "postgres"} {
		t.Fatalf("answers %v", src.answers)
	}
	src.actionErr = errors.New("send failed")
	r.tt.Click(SheetAnswer + " sqlite")
	r.tt.Frame()
	if !r.text("send failed") || !r.has(LabelSheet) {
		t.Errorf("failure not shown or sheet closed: %q", r.tt.Texts())
	}
}

func TestSheetClosesWithNoOpenQuestion(t *testing.T) {
	src := sheetSource()
	src.snap.Resolved = []string{"D1", "D2"}
	r := newSheetRig(t, src)
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Frame()
	if r.has(LabelSheet) {
		t.Error("sheet shows with no open question")
	}
}
