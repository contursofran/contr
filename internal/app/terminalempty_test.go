package app

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/projects"
)

// With no project open the empty terminal says so, with the buttons of ⌘O and ⌘T (D255).
func TestEmptyTerminalWithNoProjectShowsTheFirstRun(t *testing.T) {
	f, _, tt, _ := terminalFrame(t, nil)
	tt.Frame()
	art := apptest.Box(t, tt, LabelTerminalArt)
	if art.W != 600 || art.H != 400 {
		t.Errorf("art %+v, want it to cover the 600×400 slot", art)
	}
	got := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{TerminalNoProjectLine, sidebar.LabelOpenProject, "⌘O", TerminalNewLine, "⌘T"} {
		if !strings.Contains(got, want) {
			t.Errorf("texts %q, want %q", got, want)
		}
	}
	if strings.Contains(got, TerminalOffLine) {
		t.Errorf("texts %q, want no %q while no project is open", got, TerminalOffLine)
	}
	// The buttons are the actions of the keys.
	f.ProjectsModal = &projectsview.ProjectsModal{}
	var opened []string
	f.NewTerm = &NewTerminal{Open: func(project, dir string) (string, error) {
		opened = append(opened, project)
		return "tNew", nil
	}}
	tt.Frame()
	if err := tt.Click(sidebar.LabelOpenProject); err != nil {
		t.Fatal(err)
	}
	if !f.ProjectsModal.Opened() {
		t.Error("Open a project did not open the projects modal")
	}
	if err := tt.Click(TerminalNewLine); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 {
		t.Errorf("New terminal opened %d terminals, want 1", len(opened))
	}
}

// With a project open and no terminal the texts of D247 stay, with no buttons (D255).
func TestEmptyTerminalWithAProjectOpenKeepsItsTexts(t *testing.T) {
	f, _, tt, _ := terminalFrame(t, nil)
	dir := filepath.Join(t.TempDir(), "p")
	store := &projects.Store{}
	store.AddFolder(filepath.Join(dir, ".git"), dir)
	f.SpacesList = sidebar.NewSpacesList(nil, f.Selection, view.Env{})
	f.SpacesList.SetProjects(store)
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: dir})
	tt.Frame()
	got := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{TerminalOffLine, TerminalNewLine, "⌘T"} {
		if !strings.Contains(got, want) {
			t.Errorf("texts %q, want %q", got, want)
		}
	}
	if strings.Contains(got, TerminalNoProjectLine) || strings.Contains(got, sidebar.LabelOpenProject) {
		t.Errorf("texts %q, want no first run while a project is open", got)
	}
}

func TestNoProjectOpenNeedsAListedAndOpenProject(t *testing.T) {
	f := &Frame{Selection: state.NewSelection(nil)}
	if !f.noProjectOpen() {
		t.Error("no sidebar list: want no project open")
	}
	f.SpacesList = sidebar.NewSpacesList(nil, f.Selection, view.Env{})
	if !f.noProjectOpen() {
		t.Error("an empty list: want no project open")
	}
	f.openProject = "p"
	if !f.noProjectOpen() {
		t.Error("a title bar project but no project in the list: want no project open")
	}
	dir := filepath.Join(t.TempDir(), "p")
	store := &projects.Store{}
	store.AddFolder(filepath.Join(dir, ".git"), dir)
	f.SpacesList.SetProjects(store)
	if f.noProjectOpen() {
		t.Error("a listed project in the title bar: want one open")
	}
	f.openProject = ""
	if !f.noProjectOpen() {
		t.Error("a listed project, none open or selected: want no project open")
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: dir})
	if f.noProjectOpen() {
		t.Error("a selected project row: want one open")
	}
}

func TestEmptyTerminalShowsNotWhileTheTerminalIsLiveOrUnavailable(t *testing.T) {
	f, w, tt, _ := terminalFrame(t, nil)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	for deadline := time.Now().Add(3 * time.Second); f.termState.Kind != ghostty.Live && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
	}
	if f.termState.Kind != ghostty.Live {
		t.Fatalf("state %+v, want live", f.termState)
	}
	tt.Frame()
	if !apptest.Absent(tt, LabelTerminalArt) {
		t.Error("the art shows behind a live terminal")
	}
	if got := strings.Join(tt.Texts(), "\n"); strings.Contains(got, TerminalOffLine) || strings.Contains(got, TerminalNewLine) || strings.Contains(got, TerminalNoProjectLine) {
		t.Errorf("a live terminal shows the empty texts: %q", got)
	}

	f.closeTerminal()
	f.termErr = "terminal unavailable: x"
	tt.Frame()
	if !apptest.Absent(tt, LabelTerminalArt) {
		t.Error("the art shows while the terminal is unavailable")
	}
	if got := strings.Join(tt.Texts(), "\n"); !strings.Contains(got, f.termErr) {
		t.Errorf("texts %q, want the error line", got)
	}
}

func TestHandsCoverPutsTheGapAtTheCenterAndCoversThePanel(t *testing.T) {
	const iw, ih float32 = 2400, 1560
	for _, r := range []ui.Rect{
		{X: 10, Y: 20, W: 1400, H: 600}, // wide
		{W: 900, H: 1000},               // tall
		{W: 480, H: 700},                // the terminal's least width
		{W: 1200, H: 780},               // the drawing's own shape
	} {
		d := handsCover(r, iw, ih)
		gapX := d.X + assets.HandsGapX*d.W
		if math.Abs(float64(gapX-(r.X+r.W/2))) > 0.01 {
			t.Errorf("%+v: gap at x %v, want the center %v", r, gapX, r.X+r.W/2)
		}
		if d.X > r.X+0.01 || d.Y > r.Y+0.01 || d.X+d.W < r.X+r.W-0.01 || d.Y+d.H < r.Y+r.H-0.01 {
			t.Errorf("%+v: drawing %+v does not cover the panel", r, d)
		}
		if math.Abs(float64(d.W/d.H-iw/ih)) > 0.001 {
			t.Errorf("%+v: drawing %+v is not to scale", r, d)
		}
	}
	// Room above and below: the gap sits at handsGapTop of the height.
	r := ui.Rect{W: 1400, H: 600}
	d := handsCover(r, iw, ih)
	if gapY := d.Y + assets.HandsGapY*d.H; math.Abs(float64(gapY-r.H*handsGapTop)) > 0.01 {
		t.Errorf("gap at y %v, want %v", gapY, r.H*handsGapTop)
	}
	// Little room: the drawing goes up only until its bottom edge meets the panel's.
	if d := handsCover(ui.Rect{W: 1200, H: 780}, iw, ih); math.Abs(float64(d.Y+d.H-780)) > 0.01 {
		t.Errorf("drawing %+v, want its bottom at the panel's bottom 780", d)
	}
	if d := handsCover(ui.Rect{W: 100}, iw, ih); d != (ui.Rect{}) {
		t.Errorf("an empty panel gets %+v, want nothing", d)
	}
}

func TestHandsDecodeOnceAndDataThatDoesNotDecodeIsDropped(t *testing.T) {
	done := false
	loadHands([]byte("not a png"), func() { done = true })
	if done {
		t.Error("done ran for data that does not decode")
	}
	var b *ui.Bitmap
	for deadline := time.Now().Add(5 * time.Second); b == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b = handsBitmap(nil)
	}
	if b == nil {
		t.Fatal("the drawing never decoded")
	}
	if w, h := ui.ImageSize(b); w != 2400 || h != 1560 {
		t.Errorf("drawing %v×%v, want 2400×1560", w, h)
	}
}
