package kit

import (
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

// TestAgentRowIsAsTallAsItsHeight is #466: the two lines use the line
// heights of their type styles, so the row is as tall as AgentRowHeight
// (docs/spec/AgentRow.md: padding, 18 for body, 16 for meta).
func TestAgentRowIsAsTallAsItsHeight(t *testing.T) {
	for _, orch := range []bool{true, false} {
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			ui.Column(c).Fill().Width(310).Background(theme.Current().Ground).Children(func() {
				AgentRow(c, AgentRowOpts{Label: "row", Title: "Agent rows on ui.List", Name: "b-241",
					Model: "sonnet", Age: "6m", State: GlyphWorking, Orchestrator: orch})
			})
		}, 310, 120)
		tt.Frame()
		r := find(t, tt, "row")
		if want := AgentRowHeight(orch); r.H != want {
			t.Errorf("orchestrator %v: row is %v tall, want %v", orch, r.H, want)
		}
		title, meta := find(t, tt, "row title"), find(t, tt, "b-241")
		if gap := meta.Y - title.Y; gap != theme.StyleBody.Line {
			t.Errorf("orchestrator %v: line 2 is %v under line 1, want %v", orch, gap, theme.StyleBody.Line)
		}
	}
}

// rowLook draws one AgentRow on the ground and returns the colors at the left
// edge of the row (the ring), inside it (the fill), and the row box.
func rowLook(t *testing.T, p *theme.Palette, o AgentRowOpts) (ring, fill color.RGBA, box ui.Rect) {
	t.Helper()
	usePalette(t, p)
	o.Label, o.Title = "row", "Checks on hc event"
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Fill().Width(310).Background(p.Ground).Children(func() {
			AgentRow(c, o)
		})
	}, 310, 120)
	tt.Frame()
	box = find(t, tt, "row")
	img := tt.Image()
	ring = img.RGBAAt(int(box.X), int(box.Y+box.H/2))
	fill = img.RGBAAt(int(box.X+box.W/2), int(box.Y+2))
	return ring, fill, box
}

func rgba(c ui.Color) color.RGBA { return color.RGBA{c.R, c.G, c.B, 255} }

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
}

// A selected row has the `selected-quiet` fill and no ring; red never fills
// or rings a row, also when it needs the owner (D249). Both in rows and in
// the icon column.
func TestAgentRowFillsForSelectedAndNeedsYou(t *testing.T) {
	for _, dark := range []bool{true, false} {
		p := theme.Dark
		if !dark {
			p = theme.Light
		}
		for _, compact := range []bool{false, true} {
			for _, tc := range []struct {
				name     string
				o        AgentRowOpts
				selected bool
			}{
				{"rest", AgentRowOpts{State: GlyphWorking}, false},
				{"selected", AgentRowOpts{State: GlyphWorking, Selected: true}, true},
				{"needs you", AgentRowOpts{State: GlyphNeedsYou, NeedsYou: true, Reason: "permission"}, false},
				{"needs you, selected", AgentRowOpts{State: GlyphNeedsYou, NeedsYou: true, Selected: true}, true},
			} {
				tc.o.Compact = compact
				ring, fill, _ := rowLook(t, p, tc.o)
				want := rgba(p.Ground)
				if tc.selected {
					want = rgba(theme.Over(p.SelectedQuiet, p.Ground))
				}
				if !near(fill, want) {
					t.Errorf("dark %v, compact %v, %s: fill %v, want %v", dark, compact, tc.name, fill, want)
				}
				// The edge has the fill: no ring.
				if !near(ring, want) {
					t.Errorf("dark %v, compact %v, %s: edge %v, want %v (no ring)", dark, compact, tc.name, ring, want)
				}
			}
		}
	}
}

// Line 2 of a row that needs the owner is "needs you" in `blocked-text`, then
// " · <reason> · <name>" in the meta ink, and the age; the model and CI are
// left out (D249).
func TestAgentRowNeedsYouLineTwo(t *testing.T) {
	for _, tc := range []struct {
		reason, name string
		want         []string
	}{
		{"permission", "b-250", []string{"needs you", " · permission · b-250", "2m"}},
		{"", "b-250", []string{"needs you", " · b-250", "2m"}},
		{"question", "", []string{"needs you", " · question", "2m"}},
		{"", "", []string{"needs you", "2m"}},
	} {
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			AgentRow(c, AgentRowOpts{Label: "row", Title: "Checks on hc event", Name: tc.name, Model: "sonnet",
				CI: true, Age: "2m", State: GlyphNeedsYou, NeedsYou: true, Reason: tc.reason})
		}, 310, 120)
		tt.Frame()
		texts := tt.Texts()
		i := slices.Index(texts, "row title")
		if i < 0 || !slices.Equal(texts[i+1:], tc.want) {
			t.Errorf("reason %q, name %q: texts %q, want line 2 %q", tc.reason, tc.name, texts, tc.want)
		}
	}
}

// A long name is clipped with an ellipsis; "needs you" and the age stay
// whole, and the age stays inside the row (D249).
func TestAgentRowNeedsYouClipsTheName(t *testing.T) {
	long := "b-250-a-very-long-worker-name-that-does-not-fit"
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Width(240).Children(func() {
			AgentRow(c, AgentRowOpts{Label: "row", Title: "Checks", Name: long, Age: "2m",
				State: GlyphNeedsYou, NeedsYou: true, Reason: "permission"})
		})
	}, 240, 80)
	tt.Frame()
	row, need, age := find(t, tt, "row"), find(t, tt, "needs you"), find(t, tt, "2m")
	rest := find(t, tt, " · permission · "+long)
	if rest.X+rest.W > age.X {
		t.Errorf("the rest of line 2 ends at %v, past the age at %v", rest.X+rest.W, age.X)
	}
	if age.X+age.W > row.X+row.W || need.X < row.X {
		t.Errorf("line 2 leaves the row: needs you %v, age %v, row %v", need, age, row)
	}
}

// The item count is a plain number in `blocked-text`: no pill (D240, D249).
func TestItemBadgeIsAPlainNumber(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		usePalette(t, p)
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			ui.Column(c).Fill().Width(310).Background(p.Ground).Children(func() {
				AgentRow(c, AgentRowOpts{Label: "row", Title: "lead", Age: "2m", State: GlyphWorking, Orchestrator: true, Items: 3})
			})
		}, 310, 120)
		tt.Frame()
		b := find(t, tt, LabelBadge)
		img := tt.Image()
		// The badge box has the ground at its edges: no pill.
		for _, pt := range [][2]float32{{b.X + 1, b.Y + 1}, {b.X + 1, b.Y + b.H - 2}} {
			if got := img.RGBAAt(int(pt[0]), int(pt[1])); !near(got, rgba(p.Ground)) {
				t.Errorf("dark %v: badge edge %v, want the ground %v", p.Dark, got, rgba(p.Ground))
			}
		}
		// The number is drawn in blocked-text: some pixel in the box is near it.
		hit := false
		for y := int(b.Y); y < int(b.Y+b.H) && !hit; y++ {
			for x := int(b.X); x < int(b.X+b.W); x++ {
				if near(img.RGBAAt(x, y), rgba(p.BlockedText)) {
					hit = true
					break
				}
			}
		}
		if !hit {
			t.Errorf("dark %v: no pixel of blocked-text %v in the badge", p.Dark, p.BlockedText)
		}
	}
}

// usePalette makes p the current palette for the test.
func usePalette(t *testing.T, p *theme.Palette) {
	t.Helper()
	src := mygo.ThemeDark
	if !p.Dark {
		src = mygo.ThemeLight
	}
	themetest.Use(t, src)
}

// On an arrival the count springs but does not glow (D249): during the
// effect the pixels around the number stay the ground.
func TestItemBadgeHasNoGlowOnArrival(t *testing.T) {
	var start time.Time
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		if start.IsZero() {
			start = c.Now()
		}
		ui.Column(c).Fill().Width(310).Background(theme.Current().Ground).Children(func() {
			AgentRow(c, AgentRowOpts{Label: "row", Title: "lead", Age: "2m", State: GlyphIdle, Orchestrator: true, Items: 3,
				Arrival: Arrival{Run: 1, At: start}})
		})
	}, 310, 120)
	tt.Frame()
	tt.Advance(600 * time.Millisecond) // the spring is over; a glow would still show
	b := find(t, tt, LabelBadge)
	img := tt.Image()
	ground := rgba(theme.Current().Ground)
	for _, pt := range [][2]int{{int(b.X) - 4, int(b.Y + b.H/2)}, {int(b.X + b.W/2), int(b.Y) - 3}} {
		if got := img.RGBAAt(pt[0], pt[1]); !near(got, ground) {
			t.Errorf("pixel %v next to the count is %v, want the ground %v", pt, got, ground)
		}
	}
}
