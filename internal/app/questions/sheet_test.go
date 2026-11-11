package questions

// The QuestionsNotch and OutlineCard parts of the spec sheet (#552, D179). Each one draws a
// terminal-sized panel, as the spec images do, with the part in its place: the notch at the top
// center, the card at the left of the outline ticks. The data copies the spec images.

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/owner"
)

// The size of a part: the spec images are 1000px wide, with the sheet pad around the part.
const sheetW = 1000 - 2*apptest.SheetPad

// sheetTerminal draws a panel that stands for the terminal, with the layers of view over it.
func sheetTerminal(c *ui.Context, h float32, layers func()) {
	p := theme.Current()
	ui.Box(c).Width(sheetW).Height(h).Background(p.Stage).Border(1, p.PanelLine).
		Radius(theme.Radius12).Children(layers)
}

// sheetNotch is the notch of the spec image: two questions.
func sheetNotch(c *ui.Context) {
	snap := agentstate.Snapshot{Questions: []owner.Question{
		{ID: "Q1", Project: "contr", Question: "Ship the notch now?"},
		{ID: "Q2", Project: "contr", Question: "Which name for the release?"},
	}}
	n := &Notch{
		Snapshot: func() agentstate.Snapshot { return snap },
		Project:  func() string { return "contr" },
	}
	sheetTerminal(c, 110, func() { n.View(c) })
}

// sheetOutlineCard is the card of the spec image beside the outline: seven ticks, three of them
// new, and the card of the sixth.
func sheetOutlineCard(c *ui.Context) {
	start := time.Date(2026, 11, 5, 14, 0, 0, 0, time.Local)
	texts := []string{
		"Decide the release name", "Review the sidebar PR",
		"Notch branch is ready\nThe glass float is in.", "Usage meter numbers",
		"CI is green on the outline branch",
		"CI failed on #329\nFlaky hc test; I reran it and it passed.", "Merge order for today",
	}
	// The first four came before start, so they are old; the last three are new. The sixth is
	// the one of the card, at 14:38.
	minutes := []int{-40, -30, -20, -10, 10, 38, 45}
	var todos []owner.Todo
	for i, s := range texts {
		at := start.Add(time.Duration(minutes[i]) * time.Minute)
		todos = append(todos, owner.Todo{ID: "T" + strconv.Itoa(i+1), Project: "contr", Kind: "do", Text: s, Pinned: at})
	}
	snap := agentstate.Snapshot{
		Orchestrators: []agents.Worker{{Address: apptest.OrchAddr, Project: "contr", Role: agents.RoleOrchestrator}},
		Todos:         todos,
	}
	sel := state.NewSelection(nil)
	sel.Select(state.Selected{Kind: state.KindOrchestrator, ID: apptest.OrchAddr})
	o := NewOutline(func() agentstate.Snapshot { return snap }, sel, view.Env{})
	o.start = start
	tick := outlineTicks(todos, "contr")[5]
	sheetTerminal(c, 150, func() {
		o.View(c)
		// The tooltip shows only on hover, so the card is drawn in its place, at the left of the
		// ticks.
		card := ui.Box(c).Absolute().Top(40).Right(outlineInset + outlineHitWidth + theme.Space12 - 6)
		outlineCard(c, card, tick)
	})
}

// sheetOutlineChat is the outline of a long chat (D270): 40 ticks of each kind, so the pitch is
// smaller than 11px; the last six are new, two wait for the owner; and the card of a decision.
func sheetOutlineChat(c *ui.Context) {
	start := time.Date(2026, 11, 5, 14, 0, 0, 0, time.Local)
	kinds := []struct{ kind, text string }{
		{"You", "start builders for 609, 611 and 613"},
		{"Done", "PR 633 is merged and main is green"},
		{"Warning", "CI is slow on the hc tests: 9 minutes a run"},
		{"Not verified", "the e2e run of the switch on a locked screen"},
		{"Pinned", "Test the build of 0.42 on your Mac"},
		{"Needs you", "Background task failed: go vet ./... failed with exit code 1"},
	}
	var ticks []OutlineTick
	for i := range 40 {
		k := kinds[i%len(kinds)]
		ticks = append(ticks, OutlineTick{ID: "m" + strconv.Itoa(i), Kind: k.kind, Headline: k.text,
			Pinned: start.Add(time.Duration(i-34) * 5 * time.Minute), Width: TickWidth(k.text)})
	}
	decision := OutlineTick{ID: "d", Kind: "Decision", Blocked: true, Pinned: start.Add(12 * time.Minute), Width: 10,
		Headline: "Ship the notch now, before the release, or keep it behind the switch until the owner tests the glass on a light wallpaper and the e2e run passes on main"}
	ticks[37] = decision
	ticks[20] = OutlineTick{ID: "q", Kind: "Question", Blocked: true, Headline: "Which name for the release?", Pinned: start.Add(-70 * time.Minute), Width: 8}
	sel := state.NewSelection(nil)
	o := NewOutline(func() agentstate.Snapshot { return agentstate.Snapshot{} }, sel, view.Env{})
	o.start = start
	// The sheet draws one frame: the column's height is the one its first layout gives.
	o.height = 420 - 44 - 90
	o.Chat = func() (ChatTicks, bool) {
		return ChatTicks{Addr: apptest.OrchAddr, Ticks: ticks, Top: 44, Bottom: 90}, true
	}
	sheetTerminal(c, 420, func() {
		o.View(c)
		card := ui.Box(c).Absolute().Top(200).Right(outlineInset + outlineHitWidth + theme.Space12 - 6)
		outlineCard(c, card, decision)
	})
}

// TestSpecSheet draws the parts of package questions (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "QuestionsNotch", W: sheetW, H: 110, View: sheetNotch},
		{Name: "OutlineCard", W: sheetW, H: 150, View: sheetOutlineCard},
		{Name: "Outline", W: sheetW, H: 420, View: sheetOutlineChat},
	}, nil)
}
