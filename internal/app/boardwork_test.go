package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/owner"
)

func boardSnap() agentstate.Snapshot {
	return agentstate.Snapshot{
		Orchestrators: []agents.Worker{{Name: "lead", Address: "o1", Role: "orchestrator", Project: "a", Live: true}},
		Workers: []agents.Worker{
			{Name: "p-1", Address: "t1", Role: "planner", Issue: "162", Title: "Plan the board", Project: "a", Live: true, Status: "done"},
			{Name: "b-1", Address: "t2", Role: "builder", Issue: "162", Project: "a", Live: true, Status: "working"},
			{Name: "r-1", Address: "t3", Role: "reviewer", Issue: "#170", Project: "a", Live: true, Status: "blocked", Msg: "ok?"},
			{Name: "b-9", Address: "t4", Role: "builder", Project: "other", Live: true, Status: "idle"},
		},
		Questions: []owner.Question{{ID: "D7", Project: "a", From: "b-1", Question: "Which?"}},
		Todos:     []owner.Todo{{ID: "T3", Project: "a", Kind: "do", Text: "Run it"}},
	}
}

func TestStagesComeFromRoles(t *testing.T) {
	const (
		d, w, n, b, y = icons.Done, icons.Working, icons.NotStarted, icons.Blocked, icons.NeedsYou
	)
	cases := []struct {
		name string
		w    agents.Worker
		want [4]icons.Status
	}{
		{"planner working", agents.Worker{Role: "planner", Live: true, Status: "working"}, [4]icons.Status{w, n, n, n}},
		{"builder working", agents.Worker{Role: "builder", Live: true, Status: "working"}, [4]icons.Status{d, w, n, n}},
		{"reviewer done", agents.Worker{Role: "reviewer", Live: true, Status: "done"}, [4]icons.Status{d, d, d, n}},
		{"reviewer blocked", agents.Worker{Role: "reviewer", Live: true, Status: "blocked"}, [4]icons.Status{d, d, b, y}},
		{"unknown role is build", agents.Worker{Role: "debugger", Live: true, Status: "working"}, [4]icons.Status{d, w, n, n}},
		{"ended counts done", agents.Worker{Role: "builder", Live: false}, [4]icons.Status{d, d, n, n}},
	}
	for _, c := range cases {
		rows := WorkRows(agentstate.Snapshot{Workers: []agents.Worker{c.w}}, "")
		if len(rows) != 1 || rows[0].Stages != c.want {
			t.Errorf("%s: got %v, want %v", c.name, rows, c.want)
		}
	}
}

func TestYouStageLightsForAQuestionNamingTheWorker(t *testing.T) {
	rows := WorkRows(boardSnap(), "a")
	if rows[1].Stages[StageYou] != icons.NeedsYou { // b-1 has D7
		t.Errorf("b-1: %v", rows[1].Stages)
	}
	if rows[0].Stages[StageYou] != icons.NotStarted {
		t.Errorf("p-1: %v", rows[0].Stages)
	}
}

func TestRowsAreThePerProjectWorkersWithoutOrchestrators(t *testing.T) {
	var got []string
	for _, r := range WorkRows(boardSnap(), "a") {
		got = append(got, r.Label+"|"+r.Title+"|"+r.Role+" "+r.Status)
	}
	want := []string{"#162|Plan the board|planner done", "#162|b-1|builder working", "#170|r-1|reviewer blocked"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := len(WorkRows(boardSnap(), "")); n != 4 {
		t.Errorf("all projects: %d rows, want 4", n)
	}
	if rows := WorkRows(boardSnap(), "other"); len(rows) != 1 || rows[0].Label != "b-9" {
		t.Errorf("other: %v", rows)
	}
}

func boardTester(p *BoardPanel) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Font(theme.FontFamily).
			FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() { p.View(c) })
	}, theme.PanelWidth, 900)
}

func newBoardPanel(snap agentstate.Snapshot, project string) (*BoardPanel, *NeedsRow) {
	n := &NeedsRow{Snapshot: func() agentstate.Snapshot { return snap }, Scope: func() string { return project }}
	return &BoardPanel{Snapshot: n.Snapshot, Scope: n.Scope, Needs: n.Tabs}, n
}

func TestPanelShowsWorkRowsAndTheHeads(t *testing.T) {
	p, _ := newBoardPanel(boardSnap(), "a")
	tt := boardTester(p)
	tt.Frame()
	for _, label := range []string{LabelWorkSection, LabelWorkHeader, LabelWorkRows,
		WorkRowLabel(WorkRows(boardSnap(), "a")[1])} {
		box(t, tt, label)
	}
	for _, head := range StageHeads {
		if !slices.Contains(tt.Texts(), head) {
			t.Errorf("no head %q in %q", head, tt.Texts())
		}
	}
	if slices.Contains(tt.Texts(), "b-9") {
		t.Error("a worker of another project shows")
	}
}

func TestPanelEmptyLines(t *testing.T) {
	p := &BoardPanel{}
	tt := boardTester(p)
	tt.Frame()
	box(t, tt, LabelBoardEmpty)
	p2, _ := newBoardPanel(agentstate.Snapshot{}, "")
	tt = boardTester(p2)
	tt.Frame()
	box(t, tt, LabelWorkEmpty)
	box(t, tt, LabelNeedsEmpty)
}

func TestNeedsSectionEqualsTheRowOrder(t *testing.T) {
	snap := boardSnap()
	snap.Orchestrators = nil
	snap.Workers[2].Msg = "ok?"
	p, row := newBoardPanel(snap, "a")
	tt := boardTester(p)
	tt.Frame()
	var labels []string
	for _, tab := range row.Tabs() {
		labels = append(labels, tab.Key)
		box(t, tt, tab.Label()+", "+NeedKindLabel(tab))
	}
	if want := []string{"r-1", "D7", "T3"}; !slices.Equal(labels, want) {
		t.Errorf("order %q, want %q", labels, want)
	}
	box(t, tt, NeedsCountLabel(3))
}

func TestResolvedTabIsDoneAndNotCounted(t *testing.T) {
	snap := boardSnap()
	snap.Resolved = []string{"D7"}
	p, _ := newBoardPanel(snap, "a")
	tt := boardTester(p)
	tt.Frame()
	box(t, tt, NeedsCountLabel(2))
	box(t, tt, "D7 Which?, done")
}

func TestClickingARowSelectsAndClickingANeedOpens(t *testing.T) {
	p, _ := newBoardPanel(boardSnap(), "a")
	var picked string
	var opened NeedTab
	p.Select = func(a string) { picked = a }
	p.Open = func(t NeedTab) { opened = t }
	tt := boardTester(p)
	tt.Frame()
	tt.Click(WorkRowLabel(WorkRows(boardSnap(), "a")[1]))
	if picked != "t2" {
		t.Errorf("picked %q", picked)
	}
	tt.Click("D7 Which?, decide")
	if opened.Key != "D7" {
		t.Errorf("opened %+v", opened)
	}
}

func TestNoWrapUpControl(t *testing.T) {
	p, _ := newBoardPanel(boardSnap(), "a")
	tt := boardTester(p)
	tt.Frame()
	for _, s := range tt.Texts() {
		if s == "wrap up" || s == "keep going" {
			t.Errorf("found %q", s)
		}
	}
}

func TestFramePanelRowClickShowsTheWorker(t *testing.T) {
	src := &source{snap: boardSnap(), text: "out"}
	f := NewFrame(nil)
	f.Selection = state.NewSelection(nil)
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	f.Worker = &WorkerPanel{Source: src, Selection: f.Selection, Run: Runner{Go: func(f func()) { f() }}}
	n := &NeedsRow{Snapshot: src.Snapshot, Scope: func() string { return f.scope(src) }}
	f.Panel = &BoardPanel{Snapshot: src.Snapshot, Scope: n.Scope, Needs: n.Tabs,
		Select: func(a string) { f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: a}) }}
	f.Slots.Panel = f.Panel.View
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	box(t, tt, LabelBoardRoot)
	tt.Click(WorkRowLabel(WorkRows(src.snap, "a")[1]))
	tt.Frame()
	box(t, tt, LabelWorkerPanel)
}
