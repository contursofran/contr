package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

const legendRowHeight = theme.LineHeight + 8

// Legend is the view of `contr --icons`: the ten status icons with their
// labels and where each shows up (the "G · contr status icons" artboard).
//
// The working icon turns without a build: MyGo paints its frames from the
// last build, so only the painting of the window repeats and no row is built
// again (see icons.Sized).
type Legend struct {
	// rowBuilds counts how often each row was built. Tests read it.
	rowBuilds [len(icons.All)]int
}

// View builds the legend.
func (l *Legend) View(c *ui.Context) {
	ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).
		Font(theme.FontFamily).FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
		TextColor(theme.Color(theme.Text)).Padding(16, 0).Children(func() {
		ui.Text(c, "contr status icons").Padding(0, 16, 8).TextColor(theme.Color(theme.TextDim))
		for i, s := range icons.All {
			l.rowBuilds[i]++
			ui.Row(c.Key(s.Slug())).Height(legendRowHeight).AlignItems(ui.Center).Gap(12).Padding(0, 16).Children(func() {
				icons.Icon(c, s)
				ui.Text(c, s.Label())
				ui.Text(c, legendPlace(s)).TextColor(theme.Color(theme.TextDim))
			})
		}
	})
}

// legendPlace says where the status shows up (docs/DESIGN.md, status icon
// table).
func legendPlace(s icons.Status) string {
	switch s {
	case icons.NeedsYou:
		return "worktree waits on the owner"
	case icons.Decide:
		return "decision (D…)"
	case icons.Do:
		return "to-do (T…)"
	case icons.Blocked:
		return "stuck at a prompt"
	case icons.Working:
		return "running"
	case icons.Waiting:
		return "CI or another agent"
	case icons.Idle:
		return "nothing to do"
	case icons.Done:
		return "stage passed"
	case icons.Fyi:
		return "news"
	default:
		return "stage ahead"
	}
}
