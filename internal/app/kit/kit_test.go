package kit

import (
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
	"github.com/egoist/mygo/ui/editor"
)

func TestRowShowsItsPartsAndClicks(t *testing.T) {
	clicks := 0
	long := "plugin/" + strings.Repeat("very-long-folder/", 20) + "pin-panel.ts"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(240).Children(func() {
			Row(c, RowOpts{Label: "file pin", Mark: "M", Text: long, Counts: true, Added: 5, Removed: 1, Selected: true, OnClick: func() { clicks++ }})
			Row(c, RowOpts{Text: "fix-213", Sub: "3 commits"})
		})
	}, 400, 200)
	for _, want := range []string{"M", "+5", "−1", "fix-213", "3 commits"} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	if _, ok := tt.Find(CountsText(5, 1)); !ok {
		t.Error("the counts have no label")
	}
	if _, ok := tt.Find("fix-213"); !ok {
		t.Error("a row without a label is not found by its text")
	}
	r, ok := tt.Find("file pin")
	if !ok {
		t.Fatal("row not found by label")
	}
	txt, _ := tt.Find("file pin text")
	cnt, _ := tt.Find(CountsText(5, 1))
	if txt.X+txt.W > cnt.X {
		t.Errorf("the text (ends at %v) runs under the counts (start at %v)", txt.X+txt.W, cnt.X)
	}
	if r.W > 240 {
		t.Errorf("a long text widened the row to %v; it must clip in 240", r.W)
	}
	if r.H != RowHeight {
		t.Errorf("one-line row height = %v, want %d", r.H, RowHeight)
	}
	if two, _ := tt.Find("fix-213"); two.H <= RowHeight {
		t.Errorf("a row with Sub is %v high, want two lines", two.H)
	}
	if err := tt.Click("file pin"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Errorf("clicks = %d, want 1", clicks)
	}
}

func TestSwitchChangesTheChoice(t *testing.T) {
	choice := 0
	tt := ui.NewTester(func(c *ui.Context) {
		Switch(c, "mode", &choice, "Whole branch", "By commit")
	}, 400, 60)
	if err := tt.Click("By commit"); err != nil {
		t.Fatal(err)
	}
	if choice != 1 {
		t.Errorf("choice = %d, want 1", choice)
	}
}

// before has 40 lines; after changes line 2 and adds a line at the end, so
// 30 unchanged lines lie between the changes.
var before, after = func() (string, string) {
	var b, a strings.Builder
	for i := 1; i <= 40; i++ {
		line := "line " + strings.Repeat("x", i%7) + "\n"
		b.WriteString(line)
		if i == 2 {
			line = "changed\n"
		}
		a.WriteString(line)
	}
	a.WriteString("added\n")
	return b.String(), a.String()
}()

func TestNewCodeShowsTheWholeFile(t *testing.T) {
	d := NewCode("x.txt", before, after)
	if d.Layout != editor.DiffUnified || d.Indicators != editor.DiffClassic {
		t.Errorf("layout %v, indicators %v", d.Layout, d.Indicators)
	}
	if d.Font.Family != "JetBrains Mono" || d.LineHeight != 1.5 {
		t.Errorf("font %+v, line height %v", d.Font, d.LineHeight)
	}
	if add, del := d.Stats(); add != 2 || del != 1 {
		t.Errorf("stats = +%d -%d, want +2 -1", add, del)
	}
	tt := ui.NewTester(func(c *ui.Context) { CodeView(c, d).Fill() }, 400, 600)
	tt.Frame()
	collapsed := func() bool {
		for _, txt := range tt.Texts() {
			if strings.Contains(txt, "unmodified line") {
				return true
			}
		}
		return false
	}
	// Whole file: no unchanged lines fold away.
	if collapsed() {
		t.Errorf("whole file shows a fold; texts %q", tt.Texts())
	}
	if d.Theme == nil || d.Theme.Added == d.Theme.Deleted {
		t.Error("the code view has no contr theme")
	}
	SetWhole(d, false)
	tt.Frame()
	tt.Frame()
	if !collapsed() {
		t.Errorf("only changes: the lines far from a change do not fold; texts %q", tt.Texts())
	}
	SetWhole(d, true)
	tt.Frame()
	tt.Frame()
	if collapsed() {
		t.Error("whole file again: still folded")
	}
}

func TestCodeViewFollowsLightAndDark(t *testing.T) {
	d := NewCode("x.go", "package x\n", "package y\n")
	tt := ui.NewTester(func(c *ui.Context) { CodeView(c, d).Fill() }, 300, 100)
	tt.SetDark(false)
	tt.Frame()
	light := *d.Theme
	tt.SetDark(true)
	tt.Frame()
	if *d.Theme == light {
		t.Error("the code theme did not change with dark mode")
	}
	if d.Theme.Added.R == 0 && d.Theme.Added.G == 0 {
		t.Error("no added color")
	}
}

func TestRowDrawsItsLeadAndHeightOfMatches(t *testing.T) {
	led := 0
	o := RowOpts{Label: "r", Text: "r", Sub: "s", Lead: func() { led++ }}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(240).Children(func() { Row(c, o) })
	}, 400, 200)
	if led == 0 {
		t.Error("the lead was not drawn")
	}
	if r, _ := tt.Find("r"); r.H != HeightOf(o) || r.H <= RowHeight {
		t.Errorf("row is %v high, HeightOf says %v", r.H, HeightOf(o))
	}
	if HeightOf(RowOpts{Text: "x"}) != RowHeight {
		t.Error("a one-line row should be RowHeight")
	}
}

// TestSwitchLook: the chosen segment has the selected fill, the other one
// shows the hover track, both over the ground.
func TestSwitchLook(t *testing.T) {
	choice := 1
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(theme.Dark.Ground).Children(func() {
			Switch(c, "mode", &choice, "Whole branch", "By commit")
		})
	}, 400, 60)
	tt.Frame()
	img := tt.Image()
	at := func(label string) uint32 {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
		scale := float32(img.Bounds().Dx()) / 400
		px := img.RGBAAt(int((r.X+2)*scale), int((r.Y+2)*scale))
		return uint32(px.R)<<16 | uint32(px.G)<<8 | uint32(px.B)
	}
	track := theme.Over(theme.Dark.Hover, theme.Dark.Ground)
	hex := func(c ui.Color) uint32 {
		o := theme.Over(c, track)
		return uint32(o.R)<<16 | uint32(o.G)<<8 | uint32(o.B)
	}
	if got, want := at("By commit"), hex(theme.Dark.Selected); got != want {
		t.Errorf("chosen segment = %06X, want %06X", got, want)
	}
	if got, want := at("Whole branch"), hex(ui.Color{}); got != want {
		t.Errorf("other segment = %06X, want %06X", got, want)
	}
}

func TestCountsWithShowsOnlyTheCountsAskedFor(t *testing.T) {
	p := theme.Dark
	look := CountsLook{Added: p.Ink40, Removed: p.Ink24, Gap: theme.Space6, Style: theme.StyleMeta}
	build := func(only CountsOnly) *ui.Tester {
		look.Only = only
		return ui.NewTester(func(c *ui.Context) { CountsWith(c, 7, 2, look) }, 200, 40)
	}
	both := build(CountsBoth)
	if !both.HasText("+7") || !both.HasText("−2") {
		t.Errorf("both: %q", both.Texts())
	}
	added := build(CountsAddedOnly)
	if !added.HasText("+7") || added.HasText("−2") {
		t.Errorf("added only: %q", added.Texts())
	}
	if _, ok := added.Find("+7"); !ok {
		t.Error("added only: no label +7")
	}
	removed := build(CountsRemovedOnly)
	if removed.HasText("+7") || !removed.HasText("−2") {
		t.Errorf("removed only: %q", removed.Texts())
	}
}

func pix(img *image.RGBA, x, y float32) [3]uint8 {
	p := img.RGBAAt(int(x), int(y))
	return [3]uint8{p.R, p.G, p.B}
}

// A selected row and a Switch read the palette on every build: in Light the selected fill and the
// segment colours are the Light ones, not the Dark ones (#403, D157).
func TestRowAndSwitchFollowTheTheme(t *testing.T) {
	choice := 0
	view := func(c *ui.Context) {
		ui.Column(c).Width(240).Background(theme.Current().Ground).Children(func() {
			Row(c, RowOpts{Label: "picked", Text: "fix-213", Selected: true})
			Switch(c, "mode", &choice, "One", "Two")
		})
	}
	fill := func() ([3]uint8, [3]uint8) {
		tt := ui.NewTester(view, 300, 200)
		tt.Frame()
		r, ok := tt.Find("picked")
		if !ok {
			t.Fatal("no row")
		}
		s, ok := tt.Find("mode")
		if !ok {
			t.Fatal("no switch")
		}
		img := tt.Image()
		return pix(img, r.X+r.W-6, r.Y+r.H/2), pix(img, s.X+s.W-3, s.Y+s.H/2)
	}
	want := func(p *theme.Palette) ([3]uint8, [3]uint8) {
		over := func(top ui.Color) [3]uint8 {
			c := theme.Over(top, p.Ground)
			return [3]uint8{c.R, c.G, c.B}
		}
		return over(p.Selected), over(p.Hover)
	}
	themetest.Use(t, mygo.ThemeDark)
	row, track := fill()
	wr, wt := want(theme.Dark)
	if row != wr || track != wt {
		t.Fatalf("dark row %v track %v, want %v %v", row, track, wr, wt)
	}
	themetest.Use(t, mygo.ThemeLight)
	row, track = fill()
	wr, wt = want(theme.Light)
	if row != wr || track != wt {
		t.Errorf("light row %v track %v, want %v %v", row, track, wr, wt)
	}
}
