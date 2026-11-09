package viewers

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/ghsource/ghsourcetest"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

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
	}
	for _, c := range cases {
		rows := WorkRows(agentstate.Snapshot{Workers: []agents.Worker{c.w}}, "")
		if len(rows) != 1 || rows[0].Stages != c.want {
			t.Errorf("%s: got %v, want %v", c.name, rows, c.want)
		}
	}
}

func TestYouStageLightsForAQuestionNamingTheWorker(t *testing.T) {
	rows := WorkRows(apptest.BoardSnap(), "a")
	if rows[1].Stages[StageYou] != icons.NeedsYou { // b-1 has D7
		t.Errorf("b-1: %v", rows[1].Stages)
	}
	if rows[0].Stages[StageYou] != icons.NotStarted {
		t.Errorf("p-1: %v", rows[0].Stages)
	}
}

func TestRowsAreThePerProjectWorkersWithoutOrchestrators(t *testing.T) {
	var got []string
	for _, r := range WorkRows(apptest.BoardSnap(), "a") {
		got = append(got, r.Label+"|"+r.Title+"|"+r.Role+" "+r.Status)
	}
	want := []string{"#162|p-1|planner done", "#162|b-1|builder working", "#170|r-1|reviewer blocked"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := len(WorkRows(apptest.BoardSnap(), "")); n != 4 {
		t.Errorf("all projects: %d rows, want 4", n)
	}
	if rows := WorkRows(apptest.BoardSnap(), "other"); len(rows) != 1 || rows[0].Label != "b-9" {
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

func newBoardPanel(snap agentstate.Snapshot, project string) (*BoardPanel, func() []agentstate.NeedTab) {
	get, scope := func() agentstate.Snapshot { return snap }, func() string { return project }
	tabs := agentstate.NeedItems(get, scope)
	return &BoardPanel{Snapshot: get, Scope: scope, Needs: tabs}, tabs
}

func TestPanelShowsWorkRowsAndTheHeads(t *testing.T) {
	p, _ := newBoardPanel(apptest.BoardSnap(), "a")
	tt := boardTester(p)
	tt.Frame()
	for _, label := range []string{LabelWorkSection, LabelWorkHeader, LabelWorkRows,
		WorkRowLabel(WorkRows(apptest.BoardSnap(), "a")[1])} {
		apptest.Box(t, tt, label)
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
	apptest.Box(t, tt, LabelBoardEmpty)
	p2, _ := newBoardPanel(agentstate.Snapshot{}, "")
	tt = boardTester(p2)
	tt.Frame()
	apptest.Box(t, tt, LabelWorkEmpty)
	apptest.Box(t, tt, LabelNeedsEmpty)
}

func TestNeedsSectionEqualsTheRowOrder(t *testing.T) {
	snap := apptest.BoardSnap()
	snap.Orchestrators = nil
	snap.Workers[2].Msg = "ok?"
	p, row := newBoardPanel(snap, "a")
	tt := boardTester(p)
	tt.Frame()
	var labels []string
	for _, tab := range row() {
		labels = append(labels, tab.Key)
		apptest.Box(t, tt, tab.Label()+", "+NeedKindLabel(tab))
	}
	if want := []string{"r-1", "D7", "T3"}; !slices.Equal(labels, want) {
		t.Errorf("order %q, want %q", labels, want)
	}
	apptest.Box(t, tt, NeedsCountLabel(3))
}

func TestResolvedTabIsDoneAndNotCounted(t *testing.T) {
	snap := apptest.BoardSnap()
	snap.Resolved = []string{"D7"}
	p, _ := newBoardPanel(snap, "a")
	tt := boardTester(p)
	tt.Frame()
	apptest.Box(t, tt, NeedsCountLabel(2))
	apptest.Box(t, tt, "D7 Which?, done")
}

func TestClickingARowSelectsAndClickingANeedOpens(t *testing.T) {
	p, _ := newBoardPanel(apptest.BoardSnap(), "a")
	var picked string
	var opened agentstate.NeedTab
	p.Select = func(a string) { picked = a }
	p.Open = func(t agentstate.NeedTab) { opened = t }
	tt := boardTester(p)
	tt.Frame()
	tt.Click(WorkRowLabel(WorkRows(apptest.BoardSnap(), "a")[1]))
	if picked != "t2" {
		t.Errorf("picked %q", picked)
	}
	tt.Click("D7 Which?, decide")
	if opened.Key != "D7" {
		t.Errorf("opened %+v", opened)
	}
}

func TestNoWrapUpControl(t *testing.T) {
	p, _ := newBoardPanel(apptest.BoardSnap(), "a")
	tt := boardTester(p)
	tt.Frame()
	for _, s := range tt.Texts() {
		if s == "wrap up" || s == "keep going" {
			t.Errorf("found %q", s)
		}
	}
}

func TestPanelShowsTheProblemOfTheState(t *testing.T) {
	p, _ := newBoardPanel(agentstate.Snapshot{Problem: "agents: hc: program not found"}, "")
	tt := boardTester(p)
	tt.Frame()
	apptest.Box(t, tt, LabelBoardProblem)
	if !slices.Contains(tt.Texts(), "agents: hc: program not found") {
		t.Errorf("no problem text in %q", tt.Texts())
	}
	p2, _ := newBoardPanel(agentstate.Snapshot{}, "")
	tt = boardTester(p2)
	tt.Frame()
	if slices.Contains(tt.Texts(), "agents: hc: program not found") {
		t.Error("problem shows with none")
	}
}

// #212: the work row names the worker, not the terminal title.
func TestWorkRowNameIsTheWorkerNotTheTitle(t *testing.T) {
	w := agents.Worker{Name: "b212", Address: "t1", Role: "builder", Issue: "212", Live: true, Status: "working", Title: "contr:builder"}
	rows := WorkRows(agentstate.Snapshot{Workers: []agents.Worker{w}}, "")
	if len(rows) != 1 || rows[0].Title != "b212" {
		t.Errorf("rows %+v", rows)
	}
}

// TestEndedWorkersAreNotRows: an ended worker has no Work row (D112).
func TestEndedWorkersAreNotRows(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "gone", Role: "builder", Live: false},
		{Name: "here", Role: "builder", Live: true},
	}}
	rows := WorkRows(snap, "")
	if len(rows) != 1 || rows[0].Title != "here" {
		t.Errorf("rows = %+v, want only here", rows)
	}
}

func TestBoardWorkOpensRealPR(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "b-9", Address: "t1", Role: "builder", Issue: "151", Worktree: "/wt/a", Project: "a", Live: true, Status: "working"},
		{Name: "b-8", Address: "t2", Role: "builder", Project: "a", Live: true},
	}}
	r := ghsourcetest.NewPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	r.W.Sync(snap.Workers)
	r.Flush()
	p, _ := newBoardPanel(snap, "a")
	p.PRs = r.W
	var dir string
	var n int
	var issueOpened bool
	picked := ""
	p.OpenPR = func(d string, num int) { dir, n = d, num }
	p.OpenIssue = func(string, int) { issueOpened = true }
	p.Select = func(a string) { picked = a }
	tt := boardTester(p)
	tt.Frame()
	if err := tt.Click(LabelWorkPR("#307")); err != nil {
		t.Fatal(err)
	}
	if dir != "/wt/a" || n != 307 || picked != "" || issueOpened {
		t.Errorf("OpenPR(%q, %d), picked %q, issue %v", dir, n, picked, issueOpened)
	}
	if _, ok := tt.Find(LabelWorkPR("b-8")); ok {
		t.Error("a worker without an issue has a PR button")
	}
}

func TestBoardWorkWithoutPROpensTheIssue(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "b-9", Address: "t1", Role: "builder", Issue: "151", Worktree: "/wt/a", Project: "a", Live: true, Status: "working"},
	}}
	r := ghsourcetest.NewPRSourceRig(nil)
	r.W.Sync(snap.Workers)
	r.Flush()
	p, _ := newBoardPanel(snap, "a")
	p.PRs = r.W
	var dir string
	var n int
	prOpened := false
	p.OpenPR = func(string, int) { prOpened = true }
	p.OpenIssue = func(d string, num int) { dir, n = d, num }
	tt := boardTester(p)
	tt.Frame()
	if err := tt.Click(LabelWorkIssue("#151")); err != nil {
		t.Fatal(err)
	}
	if dir != "/wt/a" || n != 151 || prOpened {
		t.Errorf("OpenIssue(%q, %d), pr opened %v", dir, n, prOpened)
	}
}
