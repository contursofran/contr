package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
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

// prChecks is the state of the Checks tab. It is reset when a PR opens.
type prChecks struct {
	rows     []github.Check
	err      string
	loaded   bool // a read ended, with rows or with an error
	started  bool // the tab asked for its first read
	inflight bool
	// stamp is the hc event state of the worker terminal at the last read.
	// known is false until the first agents sync after the tab opened takes it.
	stamp termStamp
	known bool
}

// termStamp is what shows a new hc event for a terminal: the time of its last
// event, its turn and its status.
type termStamp struct {
	out    time.Time
	turn   uint64
	status string
}

func stampOf(w agents.Worker) termStamp {
	return termStamp{out: w.LastOutput, turn: w.Turn, status: w.Status}
}

// checksTrigger says why the Checks tab reads.
type checksTrigger int

const (
	checksTabOpened   checksTrigger = iota // the tab shows for the first time
	checksWorkerEvent                      // the worker terminal of the PR folder has a new hc event
)

// checksDue is the one place that decides when the Checks tab reads again
// (owner answer D23). The tab reads when it opens and on a new hc event of the
// terminal of the worker whose worktree is the PR folder. There is no polling,
// and no read starts while one runs.
func checksDue(s prChecks, t checksTrigger) bool {
	if s.inflight {
		return false
	}
	switch t {
	case checksTabOpened:
		return !s.started
	case checksWorkerEvent:
		return s.started
	}
	return false
}

// OnAgentsSync re-reads the checks when the Checks tab shows and the worker of
// the PR folder has a new hc event since the last read: its LastOutput, Turn or
// Status changed. The frame calls it after each agents sync with the workers.
func (p *PRPane) OnAgentsSync(workers []agents.Worker) {
	if !p.open || p.pr == nil || p.tab != PRTabChecks {
		return
	}
	dir := filepath.Clean(p.dir)
	for _, w := range workers {
		if w.Worktree == "" || filepath.Clean(w.Worktree) != dir {
			continue
		}
		cur := stampOf(w)
		if !p.checks.known {
			p.checks.stamp, p.checks.known = cur, true
			return
		}
		if cur != p.checks.stamp {
			p.readChecks(checksWorkerEvent, cur)
		}
		return
	}
}

// readChecks reads the checks off the UI thread when checksDue allows it.
// The result is dropped when the pane opened another PR or closed (D59).
func (p *PRPane) readChecks(t checksTrigger, stamp termStamp) {
	if !checksDue(p.checks, t) {
		return
	}
	p.checks.started, p.checks.inflight = true, true
	if t == checksWorkerEvent {
		p.checks.stamp = stamp
	}
	n, client := p.number, p.client
	p.reads.Do(p.Env, func() func() {
		rows, err := client.Checks(context.Background(), n)
		return func() {
			p.checks.inflight, p.checks.loaded = false, true
			if err != nil {
				p.checks.err = err.Error()
			} else {
				p.checks.rows, p.checks.err = rows, ""
			}
			p.Env.Redraw()
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
	p.readChecks(checksTabOpened, termStamp{})
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
		p.Env.Open(ch.Link)
	}).Children(func() { ui.Text(c, LabelPRCheckLog) })
}
