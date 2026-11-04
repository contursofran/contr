package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// Labels of the Checks tab.
const (
	LabelPRChecks   = "pr checks"
	LabelPRCheckLog = "log"
	LabelPRNoChecks = "No checks."
)

// checksMinGap is the least time between two re-reads that the frame asks
// for. A read on tab open ignores it.
const checksMinGap = 10 * time.Second

// prChecks is the state of the Checks tab. It is reset when a PR opens.
type prChecks struct {
	rows     []github.Check
	err      string
	loaded   bool // a read ended, with rows or with an error
	started  bool // the tab asked for its first read
	inflight bool
	last     time.Time
}

// checksTrigger says why the Checks tab reads.
type checksTrigger int

const (
	checksTabOpened  checksTrigger = iota // the tab shows for the first time
	checksAgentsSync                      // the frame synced the agents
)

// checksDue is the one place that decides when the Checks tab reads again
// (owner question D23). The tab reads when it opens, and again on an agents
// sync, but not while a read runs and not more often than checksMinGap.
// Change the rule here.
func checksDue(s prChecks, t checksTrigger, now time.Time) bool {
	if s.inflight {
		return false
	}
	switch t {
	case checksTabOpened:
		return !s.started
	case checksAgentsSync:
		return s.started && now.Sub(s.last) >= checksMinGap
	}
	return false
}

// OnAgentsSync re-reads the checks when the Checks tab shows. The frame
// calls it after each agents sync.
func (p *PRPane) OnAgentsSync() {
	if p.open && p.pr != nil && p.tab == PRTabChecks {
		p.readChecks(checksAgentsSync)
	}
}

// readChecks reads the checks off the UI thread when checksDue allows it.
// The result is dropped when the pane opened another PR or closed (D59).
func (p *PRPane) readChecks(t checksTrigger) {
	if !checksDue(p.checks, t, p.now()) {
		return
	}
	p.checks.started, p.checks.inflight = true, true
	p.checks.last = p.now()
	gen, n, client := p.gen, p.number, p.client
	p.Run.Do(func() func() {
		rows, err := client.Checks(context.Background(), n)
		return func() {
			if gen != p.gen {
				return
			}
			p.checks.inflight, p.checks.loaded = false, true
			if err != nil {
				p.checks.err = err.Error()
			} else {
				p.checks.rows, p.checks.err = rows, ""
			}
			p.redraw()
		}
	})
}

func checkIcon(result string) icons.Status {
	switch result {
	case "pass":
		return icons.Done
	case "fail":
		return icons.Blocked
	case "pending":
		return icons.Working
	}
	return icons.Idle
}

// checkTime is the dim time column of a row.
func checkTime(ch github.Check) string {
	switch ch.Result {
	case "pending":
		return "running"
	case "skip":
		return "skipped"
	case "cancel":
		return "cancelled"
	}
	d := ch.Duration()
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// checksTab draws the Checks tab: loading, the error, or one row per check.
func (p *PRPane) checksTab(c *ui.Context) {
	p.readChecks(checksTabOpened)
	dim := theme.Color(theme.TextDim)
	switch {
	case p.checks.err != "":
		ui.Text(c, p.checks.err).TextColor(dim).Padding(theme.PRPanePad)
		return
	case !p.checks.loaded:
		ui.Text(c, LabelPRLoad).TextColor(dim).Padding(theme.PRPanePad)
		return
	case len(p.checks.rows) == 0:
		ui.Text(c, LabelPRNoChecks).TextColor(dim).Padding(theme.PRPanePad)
		return
	}
	ui.Scroll(c).Label(LabelPRChecks).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(theme.PRPanePad).Children(func() {
			for _, ch := range p.checks.rows {
				ui.Row(c).FillWidth().Gap(10).PaddingY(8).AlignItems(ui.Center).
					BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
					icons.Sized(c, checkIcon(ch.Result), theme.IconSize).Shrink(0)
					ui.Text(c, ch.Name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").
						TextColor(theme.Color(theme.Text2))
					ui.Text(c, checkTime(ch)).Shrink(0).NoWrap().TextColor(theme.Color(theme.TextDim2))
					p.logButton(c, ch)
				})
			}
		})
	})
}

func (p *PRPane) logButton(c *ui.Context, ch github.Check) {
	if ch.Link == "" {
		return
	}
	ui.ButtonBase(c).Label(LabelPRCheckLog).Tooltip("Open the log of " + ch.Name).Cursor(ui.CursorPointer).Shrink(0).
		TextColor(theme.Color(theme.TextDim)).OnClick(func() {
		if p.OpenURL != nil {
			p.OpenURL(ch.Link)
		}
	}).Children(func() { ui.Text(c, LabelPRCheckLog) })
}
