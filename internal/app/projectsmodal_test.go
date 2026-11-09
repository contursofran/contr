package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/projects"
)

type modalRig struct {
	t      *testing.T
	m      *ProjectsModal
	store  *projects.Store
	dirs   []string // all dirs, tree order
	saved  []*projects.Store
	saveEr error
	// rows edits each group the rig returns.
	rows   func(g *spaces.RepoGroup)
	status []string
	tt     *ui.Tester
	// held queues work instead of running it, to test saves that overlap.
	held []func()
}

func newModalRig(t *testing.T) *modalRig {
	t.Helper()
	r := &modalRig{t: t, store: &projects.Store{}, dirs: []string{"/w/a/.git", "/w/b/.git", "/w/c/.git"}}
	r.m = &ProjectsModal{
		Groups: r.groups,
		Store:  func() *projects.Store { return r.store },
		Save: func(s *projects.Store) error {
			r.saved = append(r.saved, s)
			return r.saveEr
		},
		Run:    Runner{Go: func(f func()) { f() }},
		Status: func(s string) { r.status = append(r.status, s) },
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Grow(1).Label("window")
			r.m.View(c)
		})
	}, 1000, 700)
	r.tt.Frame()
	return r
}

// groups are the groups the sidebar shows: stored order, hidden left out.
func (r *modalRig) groups() []spaces.RepoGroup {
	var out []spaces.RepoGroup
	for _, d := range r.store.Order(r.dirs) {
		if r.store.Hidden(d) {
			continue
		}
		name := strings.Split(d, "/")[2]
		g := spaces.RepoGroup{CommonDir: d, Name: name, Rows: []spaces.WorktreeRow{{HoldYou: name == "a"}, {}}}
		if r.rows != nil {
			r.rows(&g)
		}
		out = append(out, g)
	}
	return out
}

func (r *modalRig) names() []string {
	var n []string
	for _, g := range r.groups() {
		n = append(n, g.Name)
	}
	return n
}

func (r *modalRig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

func (r *modalRig) open() {
	r.m.Toggle()
	r.tt.Frame()
	r.tt.Frame() // the list measures its rows in the first frame and the footer moves
}

func TestProjectsModalOpensAndListsProjects(t *testing.T) {
	r := newModalRig(t)
	if r.has(LabelProjects) {
		t.Fatal("modal shows before it opens")
	}
	r.open()
	for _, l := range []string{LabelProjects, LabelProjectsSearch, LabelProjectsFooter, ProjectsRow("a"), ProjectsRow("b"), ProjectsRow("c")} {
		if !r.has(l) {
			t.Errorf("no %q", l)
		}
	}
	texts := r.tt.Texts()
	for _, want := range []string{"Open · 3", "drag to reorder the sidebar"} {
		if !slices.Contains(texts, want) {
			t.Errorf("no text %q in %v", want, texts)
		}
	}
	if !slices.Contains(texts, "1 need you") {
		t.Errorf("no need-you meta in %v", texts)
	}
	r.m.Toggle()
	r.tt.Frame()
	if r.has(LabelProjects) {
		t.Error("Toggle did not close the modal")
	}
}

func TestProjectsModalClosesWithEsc(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Key(0, ui.KeyEscape)
	if r.has(LabelProjects) || r.m.Opened() {
		t.Error("Esc did not close the modal")
	}
	r.open()
	for _, gone := range []string{"Done", "close"} {
		if r.has(gone) || slices.Contains(r.tt.Texts(), gone) {
			t.Errorf("the modal still has %q", gone)
		}
	}
}

func TestProjectsModalEscClearsTheFieldFirst(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Type("zz")
	r.tt.Key(0, ui.KeyEscape)
	if r.m.query != "" || !r.m.Opened() {
		t.Errorf("first Esc: query %q opened %v", r.m.query, r.m.Opened())
	}
	r.tt.Key(0, ui.KeyEscape)
	if r.m.Opened() {
		t.Error("Esc on an empty field did not close")
	}
}

func TestProjectsModalHideHidesAndSaves(t *testing.T) {
	r := newModalRig(t)
	apply := 0
	r.m.Apply = func() { apply++ }
	r.open()
	r.tt.Key(0, ui.KeyDown) // select b
	r.tt.Frame()
	r.tt.Click(ProjectsHideOf("b"))
	if !r.store.Hidden("/w/b/.git") {
		t.Fatal("project b is not hidden")
	}
	if apply != 1 {
		t.Errorf("Apply ran %d times", apply)
	}
	if len(r.saved) != 1 || !r.saved[0].Hidden("/w/b/.git") {
		t.Fatalf("saved %v", r.saved)
	}
	r.tt.Frame()
	if r.has(ProjectsRow("b")) || !r.has(ProjectsRow("a")) {
		t.Error("the list still shows b, or lost a")
	}
}

func TestProjectsModalSearchFiltersAndUnderlines(t *testing.T) {
	r := newModalRig(t)
	r.dirs = []string{"/w/contr/.git", "/w/contr-rune/.git", "/w/site/.git"}
	r.open()
	r.tt.Type("rune")
	r.tt.Frame()
	if !r.has(ProjectsRow("contr-rune")) || r.has(ProjectsRow("contr")) || r.has(ProjectsRow("site")) {
		t.Errorf("filter wrong: %v", r.tt.Texts())
	}
	texts := r.tt.Texts()
	if !slices.Contains(texts, "Open · 3") {
		t.Errorf("Open count is not that of all open projects: %v", texts)
	}
	if slices.Contains(texts, "drag to reorder the sidebar") {
		t.Error("the hint shows while filtering")
	}
}

func TestNameSpansUnderlinesTheMatch(t *testing.T) {
	sp := nameSpans("contr-rune", "RUNE", ui.Color{})
	if len(sp) != 3 || sp[0].Text != "contr-" || sp[1].Text != "rune" || !sp[1].Underline || sp[0].Underline || sp[2].Text != "" {
		t.Errorf("spans %+v", sp)
	}
	if sp := nameSpans("contr", "zz", ui.Color{}); len(sp) != 1 || sp[0].Underline {
		t.Errorf("no match: %+v", sp)
	}
}

func TestProjectsModalNoMatch(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Type("dashboard")
	r.tt.Frame()
	if want := ProjectsNoMatch("dashboard"); want != `No project matches "dashboard". Paste a GitHub repo or a path to add one.` ||
		!slices.Contains(r.tt.Texts(), want) {
		t.Errorf("no empty text in %v", r.tt.Texts())
	}
}

func TestProjectsModalKeysMoveAndEnter(t *testing.T) {
	r := newModalRig(t)
	r.store.Touch("/w/z/.git", "/w/z", time.Unix(1, 0))
	var switched []string
	r.m.Switch = func(d string) { switched = append(switched, d) }
	r.open()
	r.tt.Key(0, ui.KeyDown)
	r.tt.Frame()
	if r.m.sel != 1 {
		t.Fatalf("sel = %d, want 1", r.m.sel)
	}
	r.tt.Key(0, ui.KeyUp)
	r.tt.Key(0, ui.KeyUp) // stops at the top
	if r.m.sel != 0 {
		t.Fatalf("sel = %d, want 0", r.m.sel)
	}
	r.tt.Key(0, ui.KeyDown)
	r.tt.Key(0, ui.KeyEnter)
	if !slices.Equal(switched, []string{"/w/b/.git"}) || r.m.Opened() {
		t.Errorf("switched %v, opened %v", switched, r.m.Opened())
	}
	// Enter on a recent row adds it to the store.
	r.open()
	for range 5 { // past the last row: stops at z
		r.tt.Key(0, ui.KeyDown)
	}
	r.tt.Key(0, ui.KeyEnter)
	if !slices.Equal(r.store.Folders(), []string{"/w/z/.git"}) || len(r.saved) != 1 {
		t.Errorf("folders = %v, saved = %d", r.store.Folders(), len(r.saved))
	}
}

func TestProjectsModalCmdBackspaceHides(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Key(0, ui.KeyDown)
	r.tt.Key(ui.Cmd, ui.KeyBackspace)
	if !r.store.Hidden("/w/b/.git") || len(r.saved) != 1 || !r.saved[0].Hidden("/w/b/.git") {
		t.Errorf("hidden = %v, saved = %v", r.store.Hidden("/w/b/.git"), r.saved)
	}
}

func TestProjectsModalCmdBackspaceWithTextDoesNotHide(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Type("a")
	r.tt.Frame()
	if slices.Contains(r.tt.Texts(), "hide") {
		t.Error("the footer offers hide while the field has text")
	}
	r.tt.Key(ui.Cmd, ui.KeyBackspace) // deletes the text
	r.tt.Frame()
	if r.store.Hidden("/w/a/.git") || len(r.saved) != 0 || r.m.query != "" {
		t.Errorf("hidden %v, saved %d, query %q", r.store.Hidden("/w/a/.git"), len(r.saved), r.m.query)
	}
}

func TestProjectsModalFooterOffersHideOnOpenRowOnly(t *testing.T) {
	r := newModalRig(t)
	r.store.Touch("/w/z/.git", "/w/z", time.Unix(1, 0))
	r.open()
	if !slices.Contains(r.tt.Texts(), "hide") {
		t.Error("no hide hint on an open row")
	}
	for range 3 {
		r.tt.Key(0, ui.KeyDown)
	}
	r.tt.Frame()
	if slices.Contains(r.tt.Texts(), "hide") {
		t.Error("hide hint on a recent row")
	}
}

func TestProjectsModalOrchestratorDot(t *testing.T) {
	r := newModalRig(t)
	hold := "orchestrator idle"
	r.rows = func(g *spaces.RepoGroup) {
		if g.Name == "b" {
			g.Rows[0].IsMain, g.Rows[0].Hold = true, &hold
		}
	}
	orch := false
	r.m.Orchestrated = func(g spaces.RepoGroup) bool { return orch && g.Name == "b" }
	r.open()
	if r.has(LabelProjectsDot) {
		t.Error("dot without an orchestrator")
	}
	orch = true
	r.tt.Frame()
	if !r.has(LabelProjectsDot) {
		t.Error("no dot with an orchestrator")
	}
	if !slices.Contains(r.tt.Texts(), "orchestrator idle") {
		t.Errorf("no live state in %v", r.tt.Texts())
	}
}

func TestProjectsModalMetaLeavesOutZeros(t *testing.T) {
	r := newModalRig(t)
	r.open()
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, "0 worktrees") || strings.Contains(s, "0 need you") {
			t.Errorf("text %q", s)
		}
	}
}

func TestProjectsModalFooterEnterLabel(t *testing.T) {
	r := newModalRig(t)
	r.store.Touch("/w/z/.git", "/w/z", time.Unix(1, 0))
	r.open()
	if !slices.Contains(r.tt.Texts(), "switch") || slices.Contains(r.tt.Texts(), "open") {
		t.Errorf("open row: %v", r.tt.Texts())
	}
	for range 3 {
		r.tt.Key(0, ui.KeyDown)
	}
	r.tt.Frame()
	if !slices.Contains(r.tt.Texts(), "open") || slices.Contains(r.tt.Texts(), "switch") {
		t.Errorf("recent row: %v", r.tt.Texts())
	}
}

func TestProjectsModalHoverMovesSelection(t *testing.T) {
	r := newModalRig(t)
	r.open()
	b, ok := r.tt.Find(ProjectsRow("c"))
	if !ok {
		t.Fatal("no row c")
	}
	r.tt.Move(b.X+b.W/2, b.Y+b.H/2)
	r.tt.Frame()
	r.tt.Frame()
	if r.m.sel != 2 {
		t.Errorf("sel = %d, want 2", r.m.sel)
	}
}

func TestProjectsModalClickOnRowSwitches(t *testing.T) {
	r := newModalRig(t)
	var switched []string
	r.m.Switch = func(d string) { switched = append(switched, d) }
	r.open()
	r.tt.Click(ProjectsRow("c"))
	if !slices.Equal(switched, []string{"/w/c/.git"}) || r.m.Opened() {
		t.Errorf("switched %v", switched)
	}
}

func TestProjectsModalReorderStoresOrder(t *testing.T) {
	r := newModalRig(t)
	r.open()
	if r.m.list.Reorder == nil {
		t.Fatal("the list has no Reorder")
	}
	r.m.list.Reorder([]int{3}, 1) // list row 0 is the Open header
	if got := r.names(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Errorf("order %v", got)
	}
	if len(r.saved) != 1 || !slices.Equal(r.saved[0].Order(r.dirs), []string{"/w/c/.git", "/w/a/.git", "/w/b/.git"}) {
		t.Errorf("saved order wrong: %v", r.saved)
	}
}

func TestProjectsModalReorderKeepsHiddenOnesOut(t *testing.T) {
	r := newModalRig(t)
	r.store.Hide("/w/b/.git")
	r.open()
	r.m.list.Reorder([]int{2}, 1) // rows are a, c: c goes first
	if got := r.names(); !slices.Equal(got, []string{"c", "a"}) {
		t.Errorf("order %v", got)
	}
}

func TestProjectsModalFilterTurnsReorderOff(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Type("c")
	r.tt.Frame()
	if !r.has(ProjectsRow("c")) || r.has(ProjectsRow("a")) {
		t.Error("filter did not narrow the list")
	}
	if r.m.list.Reorder != nil {
		t.Error("Reorder stays on while the filter hides rows")
	}
	r.m.reorder([]int{0}, 1)
	if len(r.saved) != 0 {
		t.Error("a reorder under a filter saved")
	}
}

func TestProjectsModalWithoutStoreChangesNothing(t *testing.T) {
	r := newModalRig(t)
	r.m.Store = func() *projects.Store { return nil }
	r.open()
	r.m.hide("/w/a/.git")
	r.m.reorder([]int{0}, 2)
	if len(r.saved) != 0 {
		t.Error("saved without a store")
	}
}

func TestProjectsModalSaveErrorShowsInStatus(t *testing.T) {
	r := newModalRig(t)
	r.saveEr = errors.New("disk full")
	r.open()
	r.tt.Key(0, ui.KeyDown)
	r.tt.Key(ui.Cmd, ui.KeyBackspace)
	if len(r.status) != 1 || !strings.Contains(r.status[0], "disk full") {
		t.Errorf("status %v", r.status)
	}
}

func TestProjectsModalSavesInOrderOffTheUIThread(t *testing.T) {
	r := newModalRig(t)
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.open()
	r.tt.Key(ui.Cmd, ui.KeyBackspace) // hides a
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyBackspace) // hides b, now the first row
	if len(r.saved) != 0 {
		t.Fatal("a save ran on the UI thread")
	}
	if len(r.held) != 1 {
		t.Fatalf("%d saves started, want 1 (the second waits)", len(r.held))
	}
	r.held[0]() // first save done: the newest copy goes next
	if len(r.held) != 2 {
		t.Fatalf("%d saves started, want 2", len(r.held))
	}
	r.held[1]()
	if len(r.saved) != 2 || r.saved[0].Hidden("/w/b/.git") || !r.saved[1].Hidden("/w/b/.git") {
		t.Errorf("saves out of order: %v", r.saved)
	}
}

func TestProjectInitials(t *testing.T) {
	for in, want := range map[string]string{"contr": "C", "herdr-agents": "HA", "a_b_c": "AB", "": ""} {
		if got := projectInitials(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func (r *modalRig) pick(path string, err error) {
	r.m.PickFolder = func() (string, error) { return path, err }
	r.m.Resolve = func(p string) string { return p + "/.git" }
	r.m.Now = func() time.Time { return time.Unix(50, 0) }
}

func TestProjectsModalOpenFolderAddsToStoreAndRecent(t *testing.T) {
	r := newModalRig(t)
	r.pick("/w/d", nil)
	r.open()
	if !r.has(ProjectsOpenFolder) {
		t.Fatal("no open folder button")
	}
	r.tt.Click(ProjectsOpenFolder)
	if got := r.store.Folders(); !slices.Equal(got, []string{"/w/d/.git"}) || r.store.Path("/w/d/.git") != "/w/d" {
		t.Fatalf("folders = %v", got)
	}
	if rec := r.store.Recent(); len(rec) != 1 || rec[0].Path != "/w/d" {
		t.Fatalf("recent = %v", rec)
	}
	if len(r.saved) != 1 {
		t.Errorf("saved %d times", len(r.saved))
	}
	if got := r.store.Order([]string{"/w/d/.git", "/w/a/.git", "/w/b/.git", "/w/c/.git"}); got[3] != "/w/d/.git" {
		t.Errorf("new project is not last: %v", got)
	}
}

func TestProjectsModalOpenFolderCancelDoesNothing(t *testing.T) {
	r := newModalRig(t)
	r.pick("", ErrPickCancelled)
	r.open()
	r.tt.Click(ProjectsOpenFolder)
	if len(r.store.Folders()) != 0 || len(r.store.Recent()) != 0 || len(r.saved) != 0 || len(r.status) != 0 {
		t.Errorf("cancel changed state: %v %v %v", r.store.Recent(), r.saved, r.status)
	}
}

func TestProjectsModalOpenFolderErrorShowsStatus(t *testing.T) {
	r := newModalRig(t)
	r.pick("", errors.New("dialog broke"))
	r.open()
	r.tt.Click(ProjectsOpenFolder)
	if len(r.status) != 1 || !strings.Contains(r.status[0], "dialog broke") || len(r.saved) != 0 {
		t.Errorf("status = %v", r.status)
	}
}

func TestProjectsModalNoPickerHidesOpenFolder(t *testing.T) {
	r := newModalRig(t)
	r.open()
	if r.has(ProjectsOpenFolder) {
		t.Error("button shows without a picker")
	}
}

func TestProjectsModalRecentOpensAProjectAgain(t *testing.T) {
	r := newModalRig(t)
	r.store.Touch("/w/z/.git", "/w/z", time.Unix(1, 0))
	r.store.Touch("/w/a/.git", "/w/a", time.Unix(2, 0)) // open already: not listed
	r.open()
	if r.has(ProjectsRow("a")) && slices.Contains(r.tt.Texts(), "Recent") && len(r.m.recent()) != 1 {
		t.Error("an open project is in the recent list")
	}
	if !slices.Contains(r.tt.Texts(), "Recent") || !r.has(ProjectsRow("z")) {
		t.Fatalf("no recent row for z: %v", r.tt.Texts())
	}
	r.tt.Click(ProjectsRow("z"))
	if !slices.Equal(r.store.Folders(), []string{"/w/z/.git"}) || r.store.Recent()[0].Dir != "/w/z/.git" || len(r.saved) != 1 {
		t.Errorf("folders = %v, recent = %v, saved = %d", r.store.Folders(), r.store.Recent(), len(r.saved))
	}
}

// The open modal reads the palette on every build: after a switch to Light its card is the Light
// ground and its selected row the Light selected fill (#403, D157).
func TestProjectsModalChangesWithTheTheme(t *testing.T) {
	restoreTheme(t)
	r := newModalRig(t)
	r.open()
	pixel := func(label string, dx, dy float32) [3]uint8 {
		t.Helper()
		b, ok := r.tt.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
		p := pixelAt(r.tt.Image(), b.X+dx, b.Y+dy)
		return [3]uint8{p.R, p.G, p.B}
	}
	rgb := func(c ui.Color) [3]uint8 { return [3]uint8{c.R, c.G, c.B} }
	check := func(p *theme.Palette) {
		t.Helper()
		// The card is glass-float over the backdrop: dark in Dark, light in Light.
		if got := pixel(LabelProjects, 6, 6); (got[0] > 128) == p.Dark {
			t.Errorf("dark=%v card = %v", p.Dark, got)
		}
		if got := pixel(ProjectsRow("a"), 3, 3); got == rgb(p.Ground) {
			t.Errorf("dark=%v the selected row has no fill", p.Dark)
		}
	}
	check(theme.Dark)
	theme.Follow(&fakeTheme{}, &fakeWindow{}, mygo.ThemeLight).Stop()
	r.tt.Frame()
	check(theme.Light)
}
