package app

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/widths"
)

func TestNarrowWindowRule(t *testing.T) {
	for _, tc := range []struct {
		win    float32
		narrow bool
	}{{0, false}, {960, true}, {1099, true}, {1100, false}, {1440, false}} {
		if got := narrowWindow(tc.win); got != tc.narrow {
			t.Errorf("narrowWindow(%v) = %v, want %v", tc.win, got, tc.narrow)
		}
	}
}

func TestFitWidths(t *testing.T) {
	const term = theme.TerminalMinWidth
	for _, tc := range []struct {
		name                 string
		win, sidebar, files  float32
		sidebarOpen, filesOn bool
		wantS, wantF         float32
		wantFiles            bool
	}{
		{"unknown width changes nothing", 0, 312, 280, true, true, 312, 280, true},
		{"roomy window keeps the widths", 1440, 312, 280, true, true, 312, 280, true},
		{"files give way first", 1100, 312, 280, true, true, 312, 276, true},
		{"files stop at their minimum, then the sidebar gives way", 1100, 480, 280, true, true, 1100 - 32 - term - 220, 220, true},
		{"narrow window hides files", 1099, 312, 280, true, true, 312, 280, false},
		{"files off", 1440, 312, 280, true, false, 312, 280, false},
		{"hidden sidebar keeps its width", 1440, 312, 280, false, true, 312, 280, true},
		{"wide sidebar gives way for the terminal", 960, 480, 280, true, true, 960 - 16 - 8 - term, 280, false},
	} {
		s, f, files := fitWidths(tc.win, tc.sidebar, tc.files, tc.sidebarOpen, tc.filesOn)
		if s != tc.wantS || (files && f != tc.wantF) || files != tc.wantFiles {
			t.Errorf("%s: fitWidths(%v, %v, %v) = %v, %v, %v; want %v, %v, %v",
				tc.name, tc.win, tc.sidebar, tc.files, s, f, files, tc.wantS, tc.wantF, tc.wantFiles)
		}
	}
}

// The terminal keeps at least 480 whatever the owner dragged (docs/spec/Window.md).
func TestTerminalKeepsItsMinimumWidth(t *testing.T) {
	f, tt := newTester(t)
	f.sidebarW, f.panelW = theme.SidebarMaxWidth, theme.PanelMaxWidth
	for _, w := range []int{1100, 1200, 1440} {
		tt.SetSize(w, 800)
		if got := apptest.Box(t, tt, LabelPanelTerminal).W; got < theme.TerminalMinWidth {
			t.Errorf("window %d: terminal %v wide, want at least %d", w, got, theme.TerminalMinWidth)
		}
	}
	// The owner's widths stay as they were set.
	if f.sidebarW != theme.SidebarMaxWidth || f.panelW != theme.PanelMaxWidth {
		t.Errorf("widths %v, %v changed by the fit", f.sidebarW, f.panelW)
	}
}

func TestNarrowWindowHidesTheFilesPanel(t *testing.T) {
	f, tt := newTester(t)
	tt.SetSize(1099, 800)
	if !apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the files panel shows in a window under 1100 wide")
	}
	if !f.PanelOpen() {
		t.Error("the narrow window changed the owner's toggle")
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got != 1099-8-312-8-8 {
		t.Errorf("terminal %v wide, want the room the files left", got)
	}
	// The button is there but cannot do anything; the key does nothing either.
	apptest.Box(t, tt, titlebar.LabelFilesToggle)
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	if !f.PanelOpen() {
		t.Error("the toggle changed while the files were hidden by the window width")
	}
	tt.SetSize(1100, 800)
	if apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the files panel should show at 1100")
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got < theme.TerminalMinWidth {
		t.Errorf("terminal %v wide at 1100", got)
	}
}

func TestNarrowWindowKeepsAClosedPanelClosed(t *testing.T) {
	f, tt := newTester(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE) // hide
	tt.SetSize(1000, 800)
	tt.SetSize(1440, 900)
	if f.PanelOpen() || !apptest.Absent(tt, LabelPanelFiles) {
		t.Error("a hidden files panel came back with the window width")
	}
}

func TestSavedFilesWidthShowsInTheFrame(t *testing.T) {
	f := attachWithWidths(t, widths.Widths{Sidebar: 300, Panel: 360}, nil)
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != 300 {
		t.Errorf("sidebar %v, want the saved 300", got)
	}
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != 360 {
		t.Errorf("files %v, want the saved 360", got)
	}
}

// The sheet is glass where the system draws it, and stands on the ground
// otherwise.
func TestSheetIsGlassOnlyWithVibrancy(t *testing.T) {
	_, tt := newTester(t)
	alpha := func() uint32 { _, _, _, a := tt.Image().At(4, 400).RGBA(); return a }
	if alpha() != 0xffff {
		t.Errorf("without vibrancy the window is not solid: alpha %x", alpha())
	}
	tt.SetVibrancy(true)
	if alpha() == 0xffff {
		t.Error("with vibrancy the glass should show the system material through")
	}
}

func TestFrameFollowsTheTheme(t *testing.T) {
	restoreTheme(t)
	_, tt := newTester(t)
	dark := tt.Image().At(4, 400)
	theme.Follow(&fakeTheme{}, &fakeWindow{}, mygo.ThemeLight).Stop()
	tt.Frame()
	if light := tt.Image().At(4, 400); light == dark {
		t.Errorf("the window looks the same in Light and Dark: %v", light)
	}
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != 280 {
		t.Errorf("files %v wide in Light, want 280", got)
	}
}
