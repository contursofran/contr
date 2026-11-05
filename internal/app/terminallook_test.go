package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
)

func TestTerminalThemeHasThePaletteOfEachTheme(t *testing.T) {
	for name, p := range map[string]*theme.Palette{"dark": theme.Dark, "light": theme.Light} {
		got := terminalTheme(p)
		if got.Palette != p.ANSI {
			t.Errorf("%s: palette %v, want the theme's ANSI colors %v", name, got.Palette, p.ANSI)
		}
		if got.Foreground != p.TerminalForeground() || got.Background != p.PanelBase() || got.Cursor != p.Ink95 {
			t.Errorf("%s: foreground %v, background %v, cursor %v", name, got.Foreground, got.Background, got.Cursor)
		}
	}
	if terminalTheme(theme.Dark).Palette == terminalTheme(theme.Light).Palette {
		t.Error("dark and light have the same palette")
	}
	light, dark := terminalColors()
	if light.Palette != theme.Light.ANSI || dark.Palette != theme.Dark.ANSI {
		t.Error("terminalColors swaps light and dark")
	}
}

func TestTerminalOptionsAreClearAndFollowTheTheme(t *testing.T) {
	o := terminalOptions(nil, nil)
	light, dark := terminalColors()
	if !o.Transparent {
		t.Error("the terminal must be transparent so the panel shows")
	}
	if o.Theme == nil || o.DarkTheme == nil || o.Theme.Palette != light.Palette || o.DarkTheme.Palette != dark.Palette {
		t.Error("the options carry both themes for the plugin to pick")
	}
}

// themedTerminal builds the frame's terminal slot under the current palette,
// as the frame's View does (theme.Apply).
func themedTerminal(f *Frame, w, h int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { theme.Apply(c); f.buildTerminal(c) }, w, h)
}

// hasPixel tells whether a frame has a pixel of the color.
func hasPixel(tt *ui.Tester, want ui.Color) bool {
	img := tt.Image()
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if uint8(r>>8) == want.R && uint8(g>>8) == want.G && uint8(bl>>8) == want.B {
				return true
			}
		}
	}
	return false
}

func TestThemeChangeSwapsThePaletteWithoutReconnecting(t *testing.T) {
	restoreTheme(t)
	f, w, _, srcs := terminalFrame(t, nil)
	w.focused = true
	plat, win := &fakeTheme{dark: true}, &fakeWindow{}
	fol := theme.Follow(plat, win, mygo.ThemeDark)
	defer fol.Stop()
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	tt := themedTerminal(f, 600, 400)
	for deadline := time.Now().Add(3 * time.Second); f.termState.Kind != ghostty.Live && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
	}
	if f.termState.Kind != ghostty.Live {
		t.Fatalf("state %+v, want live", f.termState)
	}
	term, conn := f.term, f.conn
	// Red full blocks: the plugin draws them as solid cells of palette color 1.
	f.term.Feed([]byte("\x1b[31m████████"))
	time.Sleep(50 * time.Millisecond)
	tt.Frame()
	if !hasPixel(tt, theme.Dark.ANSI[1]) {
		t.Fatal("no dark red on the dark frame")
	}
	for _, tc := range []struct {
		name string
		src  mygo.ThemeSource
		want *theme.Palette
		not  *theme.Palette
	}{
		{"light", mygo.ThemeLight, theme.Light, theme.Dark},
		{"dark", mygo.ThemeDark, theme.Dark, theme.Light},
	} {
		fol.SetSource(tc.src)
		tt.Frame()
		tt.Frame()
		if !hasPixel(tt, tc.want.ANSI[1]) || hasPixel(tt, tc.not.ANSI[1]) {
			t.Errorf("%s: the red is not the %s red", tc.name, tc.name)
		}
		if f.term != term || f.conn != conn {
			t.Errorf("%s: the terminal was replaced", tc.name)
		}
		if attach, closed := srcs["t2"].counts(); attach != 1 || closed != 0 {
			t.Errorf("%s: attaches %d, closes %d; a theme change must not reconnect", tc.name, attach, closed)
		}
	}
}

func TestTerminalLinesUseTheirTokens(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		restoreTheme(t)
		src := mygo.ThemeLight
		if p.Dark {
			src = mygo.ThemeDark
		}
		theme.Follow(&fakeTheme{dark: p.Dark}, &fakeWindow{}, src).Stop()
		f, w, _, _ := terminalFrame(t, nil)
		if _, c, ok := f.terminalLine(); !ok || c != p.Ink42 {
			t.Errorf("dark=%v: empty text colour %v, want ink-42 %v", p.Dark, c, p.Ink42)
		}
		f.termErr = "terminal unavailable: x"
		if _, c, _ := f.terminalLine(); c != p.Ink42 {
			t.Errorf("dark=%v: error line colour %v, want ink-42", p.Dark, c)
		}
		f.termErr = ""
		f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
		for deadline := time.Now().Add(3 * time.Second); f.term == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			w.flush()
		}
		f.termState = ghostty.State{Kind: ghostty.NotRunning}
		if line, c, ok := f.terminalLine(); !ok || line != TerminalNotRunningLine || c != p.Ink38 {
			t.Errorf("dark=%v: ended line %q colour %v, want ink-38 %v", p.Dark, line, c, p.Ink38)
		}
		f.termState = ghostty.State{Kind: ghostty.Incompatible, Message: "core too old"}
		if line, c, ok := f.terminalLine(); !ok || line != "core too old" || c != p.Ink42 {
			t.Errorf("dark=%v: incompatible line %q colour %v, want ink-42", p.Dark, line, c)
		}
	}
}

// endStream is an attached terminal that never sends output and ends when it
// closes, as hc's does: the Conn attaches again with input when the owner types.
type endStream struct {
	quietStream
	once *sync.Once
}

func (s endStream) Close() error { s.once.Do(func() { close(s.ev) }); return nil }

// keySource counts attaches. The second one, which the first key causes (D91),
// tells that a key reached the terminal.
type keySource struct{ *scriptSource }

func (s keySource) Attach(ctx context.Context, p hc.AttachParams) (ghostty.Stream, error) {
	if _, err := s.scriptSource.Attach(ctx, p); err != nil {
		return nil, err
	}
	return endStream{quietStream{make(chan hc.StreamEvent)}, new(sync.Once)}, nil
}

// keyFrame returns a frame with the terminal of t2 live and focused.
func keyFrame(t *testing.T) (*Frame, *fakeWindow, *ui.Tester, *scriptSource) {
	t.Helper()
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Selection = state.NewSelection(f.syncTerminal)
	src := &scriptSource{address: "t2"}
	f.TerminalSource = func(string) ghostty.Source { return keySource{src} }
	t.Cleanup(f.attachTerminal(w))
	tt := ui.NewTester(f.buildTerminal, 600, 400)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	for deadline := time.Now().Add(3 * time.Second); f.termState.Kind != ghostty.Live && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
	}
	if f.termState.Kind != ghostty.Live {
		t.Fatalf("state %+v, want live", f.termState)
	}
	tt.SetFocused(true)
	tt.Frame()
	return f, w, tt, src
}

// attachesAfter lets the frame run until the source has n attaches or time ends.
func attachesAfter(w *fakeWindow, tt *ui.Tester, src *scriptSource, n int) int {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
		if a, _ := src.counts(); a >= n {
			break
		}
	}
	a, _ := src.counts()
	return a
}

func TestEmptyOverlayBuildsNothingAndKeysReachTheTerminal(t *testing.T) {
	f, w, tt, src := keyFrame(t)
	if !f.Overlay.Empty() {
		t.Fatal("a new frame has an overlay layer")
	}
	if !absent(tt, LabelTerminalOverlay) {
		t.Error("an empty overlay draws an element")
	}
	tt.Type("x")
	if n := attachesAfter(w, tt, src, 2); n < 2 {
		t.Errorf("attaches = %d: a key did not reach the terminal through the empty overlay", n)
	}
}

func TestOverlayLayerTakesKeysOnlyWhileFocused(t *testing.T) {
	f, w, tt, src := keyFrame(t)
	var field string
	remove := f.Overlay.Add(LabelTerminalOverlay, func(c *ui.Context) {
		ui.TextInput(c, &field).Label("overlay field").Absolute().Top(8).Right(8).Width(120)
	})
	tt.Frame()
	if f.Overlay.Empty() || absent(tt, "overlay field") {
		t.Fatal("the layer did not build")
	}
	// Focused: the field takes the keys and the terminal gets none.
	if err := tt.Click("overlay field"); err != nil {
		t.Fatal(err)
	}
	tt.Type("bc")
	if n := attachesAfter(w, tt, src, 2); n != 1 || field != "bc" {
		t.Errorf("focused layer: field %q, attaches %d; want %q and 1", field, n, "bc")
	}
	remove()
	tt.Frame()
	if !f.Overlay.Empty() || !absent(tt, "overlay field") {
		t.Error("remove left the layer")
	}
}

func TestOverlayLayerWithoutFocusLeavesKeysToTheTerminal(t *testing.T) {
	f, w, tt, src := keyFrame(t)
	var field string
	f.Overlay.Add(LabelTerminalOverlay, func(c *ui.Context) {
		ui.TextInput(c, &field).Label("overlay field").Absolute().Top(8).Right(8).Width(120)
	})
	tt.Frame()
	tt.Type("a")
	if n := attachesAfter(w, tt, src, 2); n < 2 || field != "" {
		t.Errorf("unfocused layer: field %q, attaches %d; want empty and 2", field, n)
	}
}
