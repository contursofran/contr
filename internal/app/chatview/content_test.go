package chatview

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
	"github.com/contursofran/contr/internal/app/view"
)

// contentRig shows one text of Claude on an opaque stage in the theme of src.
func contentRig(t *testing.T, src mygo.ThemeSource, text string) (*ui.Tester, *theme.Palette) {
	t.Helper()
	themetest.Use(t, src)
	pal := theme.Current()
	ch := New(view.Env{}, t.TempDir(), nil)
	ch.mdTheme(pal)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(pal.StageBase()).Bind(&ch.panel).Padding(0, 20).Children(func() {
			ch.said(c, "r1", text, "")
		})
	}, 1000, 1400)
	tt.Frame()
	return tt, pal
}

// pixel reads the frame's image, which is drawn once for each tester.
func pixel(tt *ui.Tester, x, y float32) color.RGBA {
	if images[tt] == nil {
		images[tt] = tt.Image()
	}
	return images[tt].RGBAAt(int(x), int(y))
}

var images = map[*ui.Tester]*image.RGBA{}

func rgba(c ui.Color) color.RGBA { return color.RGBA{c.R, c.G, c.B, c.A} }

// near tells two colors apart by at most 3 in each channel.
func near(a, b color.RGBA) bool {
	d := func(x, y uint8) bool { return math.Abs(float64(x)-float64(y)) <= 3 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

// hasInk tells that a pixel of r is near color c: the color of a text's glyphs.
func hasInk(tt *ui.Tester, r ui.Rect, c color.RGBA) bool {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if near(pixel(tt, x, y), c) {
				return true
			}
		}
	}
	return false
}

func find(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("no %q in %v", s, tt.Texts())
	}
	return r
}

const tableText = "| Worker | State | Added | Checks |\n|---|---|---|---|\n" +
	"| b-649 | running | +212 | 2 / 3 |\n| b-665 | building | +64 | — |\n"

// A table has no box: no line at its left or top edge, a hairline under the header. A column of
// numbers sits at the right, in mono; a column of words at the left.
func TestTableHasNoBoxAndNumbersAtTheRight(t *testing.T) {
	for _, src := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight} {
		tt, pal := contentRig(t, src, tableText)
		tb, ok := tt.AccessByRole(ui.RoleTable, "")
		if !ok {
			t.Fatal("no table")
		}
		r := tb.Bounds
		stage := rgba(pal.StageBase())
		head := find(t, tt, "Worker")
		// The left edge, at the middle of the header row, and the top edge above the header text.
		if got := pixel(tt, r.X, head.Y+head.H/2); !near(got, stage) {
			t.Errorf("%s: the left edge is %v, want the stage %v", src, got, stage)
		}
		if got := pixel(tt, head.X+head.W+2, r.Y); !near(got, stage) {
			t.Errorf("%s: the top edge is %v, want the stage %v", src, got, stage)
		}
		// No header fill: the header row's room right of its text is the stage.
		if got := pixel(tt, head.X+head.W+2, head.Y+head.H/2); !near(got, stage) {
			t.Errorf("%s: the header has a fill %v", src, got)
		}
		a, b := find(t, tt, "+212"), find(t, tt, "+64")
		if math.Abs(float64(a.X+a.W-(b.X+b.W))) > 1 || math.Abs(float64(a.X-b.X)) < 2 {
			t.Errorf("%s: numbers are not at the right: %+v %+v", src, a, b)
		}
		x, y := find(t, tt, "running"), find(t, tt, "building")
		if math.Abs(float64(x.X-y.X)) > 1 {
			t.Errorf("%s: words are not at the left: %+v %+v", src, x, y)
		}
		c, d := find(t, tt, "2 / 3"), find(t, tt, "—")
		if math.Abs(float64(c.X+c.W-(d.X+d.W))) > 1 {
			t.Errorf("%s: a dash in a number column is not at the right: %+v %+v", src, c, d)
		}
	}
}

func TestColumnAligns(t *testing.T) {
	rows := [][]string{{"Name", "N", "Mixed", "Dashes", "Given"}, {"a", "1", "1", "—", "1"}, {"b", "−12.5%", "x", "-", "2"}}
	header := []bool{true, false, false, false, false}
	given := []parser.Align{parser.AlignNone, parser.AlignNone, parser.AlignNone, parser.AlignNone, parser.AlignLeft}
	got := ColumnAligns(given, rows, header)
	want := []ui.Align{ui.Start, ui.End, ui.Start, ui.Start, ui.Start}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("column %d (%s): %v, want %v", k, rows[0][k], got[k], want[k])
		}
	}
	for _, s := range []string{"212", "+64", "−148", "3.5%", "2 / 3", "`42`", "1,024", "12ms"} {
		if !numericCell(s) {
			t.Errorf("%q is not a number", s)
		}
	}
	for _, s := range []string{"b-649", "v1.2.3", "#667", "two", ""} {
		if numericCell(s) {
			t.Errorf("%q is a number", s)
		}
	}
}

var remarkText = "> [!NOTE]\n> A note.\n\n> [!done]\n> A done.\n\n> [!DECISION]\n> A decision.\n\n" +
	"> [!Warning]\n> A warning.\n\n> [!UNVERIFIED]\n> Not checked.\n\n> [!FOO]\n> An unknown tag.\n"

// Each remark kind draws its bar and its word in its color, and its text in the prose ink.
func TestRemarksDrawTheirColor(t *testing.T) {
	for _, src := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight} {
		tt, pal := contentRig(t, src, remarkText)
		kinds := map[RemarkKind]ui.Color{RemarkNote: pal.ContentInfo, RemarkDone: pal.ContentDone,
			RemarkDecision: pal.ContentFailed, RemarkWarning: pal.ContentWaiting, RemarkUnverified: pal.ContentInfo}
		for k, ink := range kinds {
			want := rgba(theme.Over(ink, pal.StageBase()))
			bar := find(t, tt, LabelRemarkBar+" "+k.Word())
			// The top of the bar: a dash of the dashed bar starts there.
			if got := pixel(tt, bar.X+bar.W/2, bar.Y+1); !near(got, want) {
				t.Errorf("%s: the bar of %s is %v, want %v", src, k.Word(), got, want)
			}
			if !hasInk(tt, find(t, tt, k.Word()), want) {
				t.Errorf("%s: the word %s is not in its color %v", src, k.Word(), want)
			}
		}
		body := find(t, tt, "A done.")
		if hasInk(tt, body, rgba(theme.Over(pal.ContentDone, pal.StageBase()))) {
			t.Errorf("%s: the text of a remark is in the kind's color", src)
		}
		// An unknown tag stays a quote: its tag shows, and it has no remark.
		if !tt.HasText("[!FOO] An unknown tag.") {
			t.Errorf("%s: an unknown tag is not a quote: %v", src, tt.Texts())
		}
		if _, ok := tt.Find(LabelRemark + " "); ok {
			t.Errorf("%s: a remark with no word", src)
		}
	}
}

func TestRemarkOf(t *testing.T) {
	cases := []struct {
		in   string
		kind RemarkKind
		ok   bool
	}{
		{"[!NOTE]\nText", RemarkNote, true},
		{"[!tip]\nText", RemarkNote, true},
		{"[!IMPORTANT]\nText", RemarkNote, true},
		{" [!Done] Text on the line", RemarkDone, true},
		{"[!DECISION]\nText", RemarkDecision, true},
		{"[!WARNING]\nText", RemarkWarning, true},
		{"[!CAUTION]\nText", RemarkWarning, true},
		{"[!UNVERIFIED]\nText", RemarkUnverified, true},
		{"[!NOTE]\n", 0, false},
		{"[!FOO]\nText", 0, false},
		{"Text [!NOTE]", 0, false},
	}
	for _, c := range cases {
		r, ok := RemarkOf(c.in)
		if ok != c.ok || ok && r.Kind != c.kind {
			t.Errorf("%q: %v %v, want %v %v", c.in, r.Kind, ok, c.kind, c.ok)
		}
	}
	if r, _ := RemarkOf("[!NOTE]\nline one\nline two"); r.Body != "line one\nline two" {
		t.Errorf("body %q", r.Body)
	}
	if got := quoteText("> [!NOTE]\n>  two\n  > three\nlazy"); got != "[!NOTE]\n two\nthree\nlazy" {
		t.Errorf("quoteText %q", got)
	}
}

const diffText = "```diff\n@@ -1,2 +1,2 @@\n keep\n-old\n+new\n```\n"

// A diff block tints an added and a removed line with their color, across the block, and draws
// their text in it; a context line has no tint.
func TestDiffBlockTintsItsLines(t *testing.T) {
	for _, src := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight} {
		tt, pal := contentRig(t, src, diffText)
		block := rgba(theme.Over(pal.Hover, pal.StageBase()))
		code, ok := tt.AccessByRole(ui.RoleGroup, "Code, diff")
		if !ok {
			t.Fatal("no diff block")
		}
		right := code.Bounds.X + code.Bounds.W - 6
		for kind, ink := range map[string]ui.Color{"added": pal.ContentDone, "removed": pal.ContentFailed} {
			line := find(t, tt, LabelDiffLine+" "+kind)
			want := rgba(theme.Over(theme.DiffTint(ink), theme.Over(pal.Hover, pal.StageBase())))
			if got := pixel(tt, right, line.Y+line.H/2); !near(got, want) {
				t.Errorf("%s: the %s line at the block's right is %v, want the tint %v", src, kind, got, want)
			}
		}
		line := find(t, tt, LabelDiffLine+" context")
		if got := pixel(tt, right, line.Y+line.H/2); !near(got, block) {
			t.Errorf("%s: a context line is %v, want the block %v", src, got, block)
		}
		text := find(t, tt, "+new")
		if !hasInk(tt, text, rgba(pal.ContentDone)) && !hasInk(tt, text, rgba(theme.Over(pal.ContentDone, pal.StageBase()))) {
			t.Errorf("%s: an added line's text is not content-done", src)
		}
	}
}

func TestDiffKind(t *testing.T) {
	for line, want := range map[string]DiffLineKind{"+x": DiffAdded, "-x": DiffRemoved, " x": DiffContext, "": DiffContext,
		"@@ -1 +1 @@": DiffHunk, "+++ b/x": DiffHeader, "--- a/x": DiffHeader, "diff --git a b": DiffHeader} {
		if got := DiffKind(line); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

// A numbered list shows its numbers and a task list its check and ring; their text is Markdown of
// its own, nested lists and code included.
func TestListsAndItemText(t *testing.T) {
	tt, _ := contentRig(t, mygo.ThemeDark, "1. one `code`\n2. two\n   - nested\n\n- [x] done\n- [ ] open\n")
	for _, s := range []string{"1.", "2.", "nested", "done", "open"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %v", s, tt.Texts())
		}
	}
	if tt.HasText("[x] done") || tt.HasText("[ ] open") {
		t.Errorf("the check box shows as text: %v", tt.Texts())
	}
	src := "- [x] task\n  more\n12. twelve\n    under\n"
	d := parser.Parse(src)
	var items []*parser.Block
	for i := 0; i < d.Len(); i++ {
		if b := d.Block(i); b.Kind == parser.Item {
			items = append(items, b)
		}
	}
	if len(items) != 2 {
		t.Fatalf("%d items", len(items))
	}
	if got := ItemText(src, items[0]); !strings.HasPrefix(got, "task\nmore") {
		t.Errorf("task item %q", got)
	}
	if got := ItemText(src, items[1]); !strings.HasPrefix(got, "twelve\nunder") {
		t.Errorf("numbered item %q", got)
	}
}
