package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
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

func refsSource() *source {
	src := sheetSource()
	src.snap.Workers = []agents.Worker{{Name: "b-1", Worktree: "/wt/b-1", Project: "a"}}
	src.snap.Questions[0].Impacts = []string{"simple, one file", "scales, needs a server"}
	src.snap.Questions[0].Refs = []string{"PR 12", "#7", "https://example.com/x", "file:///etc/passwd", "docs/DECISIONS.md"}
	src.snap.Questions[0].Source = "worker: which db should I use?"
	return src
}

func TestSheetShowsImpactsRefsAndSource(t *testing.T) {
	r := newSheetRig(t, refsSource())
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Frame()
	for _, s := range []string{"simple, one file", "scales, needs a server", "worker: which db should I use?", "PR 12", "#7", "docs/DECISIONS.md"} {
		if !r.text(s) {
			t.Errorf("missing %q in %v", s, r.tt.Texts())
		}
	}
}

func TestSheetWithoutTheNewFieldsShowsNoExtras(t *testing.T) {
	r := newSheetRig(t, sheetSource())
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Frame()
	if r.text(SheetRefs) || r.text(SheetSource) {
		t.Errorf("extras without data: %v", r.tt.Texts())
	}
}

func TestSheetRefsOpen(t *testing.T) {
	r := newSheetRig(t, refsSource())
	var prs, issues []int
	var dirs, urls []string
	r.sheet.OpenPR = func(dir string, n int) { dirs = append(dirs, dir); prs = append(prs, n) }
	r.sheet.OpenIssue = func(dir string, n int) { dirs = append(dirs, dir); issues = append(issues, n) }
	r.sheet.OpenURL = func(u string) { urls = append(urls, u) }
	r.tt.Key(ui.Cmd, ui.KeyJ)
	r.tt.Frame()
	for _, l := range []string{"PR 12", "#7", "https://example.com/x"} {
		r.tt.Click(l)
	}
	if len(prs) != 1 || prs[0] != 12 || len(issues) != 1 || issues[0] != 7 || len(urls) != 1 || urls[0] != "https://example.com/x" {
		t.Errorf("prs %v issues %v urls %v", prs, issues, urls)
	}
	if len(dirs) != 2 || dirs[0] != "/wt/b-1" {
		t.Errorf("dirs %v", dirs)
	}
}

func TestSheetRefClickIsNilWhenNothingOpens(t *testing.T) {
	s := &Sheet{OpenPR: func(string, int) {}, OpenURL: func(string) {}}
	for ref, dir := range map[string]string{"file:///etc/passwd": "/wt", "docs/x.md": "/wt", "PR 3": "", "#4": "/wt", "javascript:x": ""} {
		if s.refClick(ref, dir) != nil {
			t.Errorf("%q opens", ref)
		}
	}
	if s.refClick("PR 3", "/wt") == nil || s.refClick("http://a.b", "") == nil {
		t.Error("PR with a folder or URL does not open")
	}
}

func TestParseRef(t *testing.T) {
	for in, want := range map[string]Ref{
		"PR 12": {Kind: RefPR, N: 12}, "pr#3": {Kind: RefPR, N: 3}, "#7": {Kind: RefIssue, N: 7}, "issue 9": {Kind: RefIssue, N: 9}, "5": {Kind: RefIssue, N: 5},
		"https://a.b/c": {Kind: RefURL, URL: "https://a.b/c"}, "HTTP://a.b": {Kind: RefURL, URL: "HTTP://a.b"},
		"file:///x": {}, "ftp://a.b/x": {}, "mailto:a@b.c": {}, "javascript:alert(1)": {}, "docs/x.md": {}, "": {},
	} {
		if got := ParseRef(in); got != want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", in, got, want)
		}
	}
}
