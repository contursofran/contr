package kit

import (
	"image/color"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
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
func rowLook(t *testing.T, o AgentRowOpts) (ring, fill color.RGBA, box ui.Rect) {
	t.Helper()
	o.Label, o.Title = "row", "Checks on hc event"
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Fill().Width(310).Background(theme.Current().Ground).Children(func() {
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

// A selected row has the `selected` fill and the `shadow-selected` ring of the
// selected tab; a row that needs the owner has `blocked-fill` and a
// `blocked-line` ring, also when selected (D249). Both in rows and in the icon
// column.
func TestAgentRowFillsForSelectedAndNeedsYou(t *testing.T) {
	ground := theme.Current().Ground
	for _, compact := range []bool{false, true} {
		for _, tc := range []struct {
			name       string
			o          AgentRowOpts
			fill, line func(p *theme.Palette) ui.Color
		}{
			{"rest", AgentRowOpts{State: GlyphWorking}, nil, nil},
			{"selected", AgentRowOpts{State: GlyphWorking, Selected: true},
				func(p *theme.Palette) ui.Color { return p.Selected },
				func(p *theme.Palette) ui.Color { return p.ShadowSelected[0].Color }},
			{"needs you", AgentRowOpts{State: GlyphNeedsYou, NeedsYou: true, Reason: "permission"},
				func(p *theme.Palette) ui.Color { return p.BlockedFill },
				func(p *theme.Palette) ui.Color { return p.BlockedLine }},
			{"needs you, selected", AgentRowOpts{State: GlyphNeedsYou, NeedsYou: true, Selected: true},
				func(p *theme.Palette) ui.Color { return p.BlockedFill },
				func(p *theme.Palette) ui.Color { return p.BlockedLine }},
		} {
			tc.o.Compact = compact
			ring, fill, _ := rowLook(t, tc.o)
			p := theme.Current()
			wantFill, wantRing := rgba(ground), rgba(ground)
			if tc.fill != nil {
				f := theme.Over(tc.fill(p), ground)
				wantFill, wantRing = rgba(f), rgba(theme.Over(tc.line(p), f))
			}
			if !near(fill, wantFill) {
				t.Errorf("compact %v, %s: fill %v, want %v", compact, tc.name, fill, wantFill)
			}
			if !near(ring, wantRing) {
				t.Errorf("compact %v, %s: ring %v, want %v", compact, tc.name, ring, wantRing)
			}
		}
	}
}

// Line 2 of a row that needs the owner is "needs you ·" in `blocked`, the
// reason and the age; the name, model and CI are left out (D249). With no
// reason it is "needs you" alone. The title is ink-100.
func TestAgentRowNeedsYouLineTwo(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   []string
	}{
		{"permission", []string{"needs you ·", "permission", "2m"}},
		{"", []string{"needs you", "2m"}},
	} {
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			AgentRow(c, AgentRowOpts{Label: "row", Title: "Checks on hc event", Name: "b-250", Model: "sonnet",
				CI: true, Age: "2m", State: GlyphNeedsYou, NeedsYou: true, Reason: tc.reason})
		}, 310, 120)
		tt.Frame()
		texts := tt.Texts()
		i := slices.Index(texts, "row title")
		if i < 0 || !slices.Equal(texts[i+1:], tc.want) {
			t.Errorf("reason %q: texts %q, want line 2 %q", tc.reason, texts, tc.want)
		}
	}
}
