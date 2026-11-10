package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
)

func TestAttachStartsTheWindowHiddenWithNoClock(t *testing.T) {
	w := &fakeWindow{visible: false}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	defer stop()
	f.SpacesList.Ages.SetShown(true)
	if f.SpacesList.Ages.Running() {
		t.Error("the clock runs in a window that started hidden")
	}
}

func TestTheFrameTiesTheClockToTheWindowVisibility(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	defer stop()
	ages := f.SpacesList.Ages
	if ages == nil || ages.Running() {
		t.Fatalf("clock %v: want one that waits for an age", ages)
	}
	ages.SetShown(true)
	if !ages.Running() {
		t.Fatal("the clock does not run with an age on screen")
	}
	w.hide()
	if ages.Running() {
		t.Error("the clock runs in a hidden window")
	}
	w.show()
	if !ages.Running() {
		t.Error("the clock did not return with the window")
	}
}

func TestFilesPanelRuleStaysAt1100(t *testing.T) {
	_, tt := newTester(t)
	tt.SetSize(1099, 800)
	if !apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the files panel shows at 1099")
	}
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar %v wide at 1099, want it whole", got)
	}
}

func TestNarrowWindowDrawsTheSidebarAsIconsAndRestoresIt(t *testing.T) {
	f, tt := newTester(t)
	tt.SetSize(760, 800)
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != theme.SidebarIconWidth {
		t.Errorf("sidebar %v wide at 760, want the icon column %d", got, theme.SidebarIconWidth)
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got < theme.TerminalMinWidth {
		t.Errorf("terminal %v wide at 760", got)
	}
	// The stored width (D111) is not changed by the collapse.
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("stored width %v after the collapse, want %d", f.sidebarW, theme.SidebarWidth)
	}
	// A drag of the divider in the icon column does not store a width either.
	drag(tt, sidebarDivider(t, tt), sidebarDivider(t, tt)+90)
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("a drag in the icon column stored %v", f.sidebarW)
	}
	tt.SetSize(900, 800)
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar %v wide at 900, want the stored %d", got, theme.SidebarWidth)
	}
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("stored width %v after the window grew", f.sidebarW)
	}
}

func TestSidebarIconRuleBothWays(t *testing.T) {
	for _, tc := range []struct {
		win     float32
		compact bool
	}{{0, false}, {760, true}, {799, true}, {800, false}, {1099, false}, {1440, false}} {
		if got := compactSidebar(tc.win); got != tc.compact {
			t.Errorf("compactSidebar(%v) = %v, want %v", tc.win, got, tc.compact)
		}
	}
	// Under 800 an open sidebar is the icon column, whatever width was stored;
	// from 800 up the stored width is drawn; a closed sidebar keeps its width.
	for _, tc := range []struct {
		win, stored float32
		open        bool
		want        float32
	}{
		{799, 312, true, theme.SidebarIconWidth},
		{600, 480, true, theme.SidebarIconWidth},
		{800, 312, true, 296}, // the sidebar gives way for the terminal minimum, as before
		{900, 312, true, 312},
		{799, 312, false, 312},
	} {
		got, _, files := fitWidths(tc.win, tc.stored, 280, tc.open, true)
		if got != tc.want || (tc.win < theme.FilesHideBelow && files) {
			t.Errorf("fitWidths(%v, %v, open %v) = %v, files %v; want %v, no files", tc.win, tc.stored, tc.open, got, files, tc.want)
		}
	}
}

// In the icon column the tabs still start clear of the window buttons.
func TestTabsStayClearOfTheWindowButtonsInTheIconColumn(t *testing.T) {
	_, tt := newTester(t)
	tt.SetSize(760, 800)
	if r := apptest.Box(t, tt, titlebar.LabelTabs); r.X < theme.TrafficLightInset {
		t.Errorf("tabs %+v are over the window buttons", r)
	}
}

// The foot row is centered in its area (#495): the meters' left inset equals the
// gear's right inset, and the content has as much room above (to the top of
// the foot row) as below (to the sidebar's bottom edge). No hairline sits over
// the foot (D258).
func TestSidebarFootIsCentered(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.list.Plans = func() []kit.Meter { return planMeters(fakeLeft{usage.PlanClaude: 62}) }
	h.tt.Frame()
	foot, row := apptest.Box(t, h.tt, "sidebar foot"), apptest.Box(t, h.tt, "sidebar foot row")
	gear, plans := apptest.Box(t, h.tt, sidebar.LabelSettingsOpen), apptest.Box(t, h.tt, "plan usage")
	right, left := foot.X+foot.W-(gear.X+gear.W), plans.X-foot.X
	if right != left || left <= 0 {
		t.Errorf("gear right inset %v, meters left inset %v", right, left)
	}
	if _, ok := h.tt.Find("sidebar foot divider"); ok {
		t.Error("the sidebar foot has a divider; D258 removes it")
	}
	bottom := float32(theme.WindowHeight)
	if above, below := gear.Y-row.Y, bottom-(gear.Y+gear.H); above != below {
		t.Errorf("gear has %v above and %v below", above, below)
	}
	if mid, gm := plans.Y+plans.H/2, gear.Y+gear.H/2; mid != gm {
		t.Errorf("meters center %v, gear center %v", mid, gm)
	}

	// The icon column keeps the button centered (D147).
	h.list.Compact = true
	h.tt.Frame()
	foot, gear = apptest.Box(t, h.tt, "sidebar foot"), apptest.Box(t, h.tt, sidebar.LabelSettingsOpen)
	if l, r := gear.X-foot.X, foot.X+foot.W-(gear.X+gear.W); l != r {
		t.Errorf("icon column: gear insets %v and %v", l, r)
	}
}

// adoptFrame attaches a frame to a core with one unlabeled Claude Code in a
// scratch repo. It returns the frame, its window and the repo's common dir.
func adoptFrame(t *testing.T, load func() (*projects.Store, error), save func(*projects.Store) error) (*Frame, *fakeWindow, func()) {
	t.Helper()
	repo := filepath.Join(apptest.CanonicalTmp(t), "scratch")
	apptest.MainRepo(t, repo, "ref: refs/heads/main\n")
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.deps.Projects.Load, f.deps.Projects.Save = load, save
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) {
		return &source{Snap: agentstate.Snapshot{CoreVersion: "0.1.18",
			Orchestrators: []agents.Worker{apptest.ForeignTerm("t1", agents.RoleOrchestrator, "scratch", repo)}}}, func() {}
	}
	return f, w, f.Attach(w)
}

func TestFrameDoesNotAdoptOverACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	saves := 0
	_, w, stop := adoptFrame(t,
		func() (*projects.Store, error) { return projects.Load(path) },
		func(s *projects.Store) error { saves++; return s.Save(path) })
	defer stop()
	for i := 0; i < 50; i++ {
		w.flush()
		time.Sleep(time.Millisecond)
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" || saves != 0 {
		t.Errorf("the corrupt file was written over (saves %d)", saves)
	}
}

func TestFrameSavesAnAdoptedProjectOffTheUIThread(t *testing.T) {
	saved := make(chan *projects.Store, 4)
	f, w, stop := adoptFrame(t,
		func() (*projects.Store, error) { return &projects.Store{}, nil },
		func(s *projects.Store) error { saved <- s; return nil })
	defer stop()
	var got *projects.Store
	for deadline := time.Now().Add(5 * time.Second); got == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		w.flush()
		select {
		case got = <-saved:
		default:
		}
	}
	if got == nil {
		t.Fatal("the adopted project was not saved")
	}
	if len(got.Folders()) != 1 || filepath.Base(got.Path(got.Folders()[0])) != "scratch" {
		t.Errorf("saved folders %v", got.Folders())
	}
	if len(f.SpacesList.Groups()) != 1 {
		t.Errorf("groups %v", f.SpacesList.Groups())
	}
}

func TestFrameShowsLoadErrorAndKeepsDefaultOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.deps.Projects.Load = func() (*projects.Store, error) { return projects.Load(path) }
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) {
		return &source{Snap: agentstate.Snapshot{}}, func() {}
	}
	stop := f.Attach(w)
	defer stop()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		w.flush()
		if _, ok := lastToast(f); ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if text, _ := lastToast(f); !strings.HasPrefix(text, "projects: ") {
		t.Errorf("message %q", text)
	}
	if f.Projects == nil {
		t.Error("a corrupt file gave no empty store")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Error("the corrupt file was overwritten")
	}
}

func TestFrameWithoutLoaderReadsNoStore(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	stop := f.Attach(w)
	defer stop()
	w.flush()
	if f.Projects != nil {
		t.Error("a store appeared without a loader")
	}
}

// spacesRig is a sidebar for the tests of this package that need one next to a part of the frame:
// the list and a tester. The tests of the sidebar itself, with the full harness, are in
// internal/app/sidebar.
type spacesRig struct {
	list *sidebar.SpacesList
	tt   *ui.Tester
}

// newSpaces builds the sidebar over live, with reads that run at once and no HEAD watches, on the
// window background.
func newSpaces(t *testing.T, live agentstate.Snapshot) *spacesRig {
	t.Helper()
	l := sidebar.NewSpacesList(func() agentstate.Snapshot { return live }, state.NewSelection(nil), view.Env{})
	l.Env.Run.Go = func(f func()) { f() }
	l.WatchHead = func(context.Context, string, func()) error { return nil }
	l.Sync()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Children(func() { l.View(c) })
	}, theme.SidebarWidth, theme.WindowHeight)
	return &spacesRig{list: l, tt: tt}
}

// addItem opens the sidebar's add menu and clicks item.
func addItem(tt *ui.Tester, item string) error {
	if err := tt.Click(sidebar.LabelProjectsAdd); err != nil {
		return err
	}
	return tt.Click(item)
}
