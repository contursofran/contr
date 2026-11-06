package kit

import (
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
