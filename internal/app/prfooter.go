package app

// The PR pane footer (#154, D103): a comment field, `Comment`, `Approve`
// and `Squash and merge`. Every write needs a click (or Shift-Cmd-M for the
// merge) and runs off the UI thread. After a write the pane reads the PR
// again; the footer shows the error of a failed write.

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// Labels of the PR footer.
const (
	LabelPRComment   = "Comment"
	LabelPRApprove   = "Approve"
	LabelPRMerge     = "Squash and merge"
	LabelPRMergeWait = "merge waits for CI"
	// After a merge (D105) the footer shows one of these three lines.
	LabelPRMerged      = "merged · principal told"
	LabelPRMergedBare  = "merged"
	LabelPRMergedNoOne = "merged · no one told"
	LabelPRFooter      = "pr footer"
)

// LabelPRField is the label and placeholder of the comment field.
func LabelPRField(n int) string { return "Leave a comment on #" + strconv.Itoa(n) + "…" }

// ciGate is the one rule for CI and merge (D103). Merge waits while a check
// runs. A failed CI does not stop the merge: the owner decides, and the CI
// summary shows red.
func ciGate(s github.CheckSummary) (mergeWaits, ciRed bool) {
	return s.Pending > 0, s.Fail > 0
}

// ciTextColor is the colour of the CI summary text.
func ciTextColor(s github.CheckSummary) uint32 {
	if _, red := ciGate(s); red {
		return theme.DiffDeleted
	}
	return theme.Text2
}

// footerState is the footer's own state in the pane.
type footerState struct {
	text        string
	field       ui.Handle
	busy        bool
	err         string
	viewer      string // login of the signed-in user; empty until read
	viewerAsked bool
	after       string // the line after a merge from this pane; empty before
}

// footerShown tells whether the footer shows: for an open PR, and after a
// merge from this pane, with the line of the merge (D105).
func (p *PRPane) footerShown() bool {
	return p.pr != nil && (p.pr.State == "OPEN" || p.ft.after != "")
}

// footerOpen tells whether the footer takes writes: the PR is open and was not
// merged here.
func (p *PRPane) footerOpen() bool {
	return p.pr != nil && p.pr.State == "OPEN" && p.ft.after == ""
}

// canApprove hides Approve on the owner's own PR (GitHub refuses it) and
// once the PR is approved. It waits for the login.
func (p *PRPane) canApprove() bool {
	return p.pr.ReviewDecision != "APPROVED" && p.ft.viewer != "" && p.ft.viewer != p.pr.Author
}

// readViewer reads the login once. A failure lets the next Open try again.
func (p *PRPane) readViewer(client github.Client) {
	if p.ft.viewer != "" || p.ft.viewerAsked {
		return
	}
	p.ft.viewerAsked = true
	p.Env.Do(func() func() {
		login, err := client.Viewer(context.Background())
		return func() {
			p.ft.viewerAsked = false
			if err != nil {
				p.ft.err = "Approve unavailable: " + err.Error()
			} else {
				p.ft.viewer = login
			}
			p.Env.Redraw()
		}
	})
}

// write runs a write off the UI thread, then reads the PR again. A result
// of an older pane state is dropped (D59).
// done runs after a write that succeeded, if it is not nil.
func (p *PRPane) write(op func(context.Context, github.Client, github.PR) error, done func()) {
	if p.ft.busy || p.pr == nil {
		return
	}
	p.ft.busy, p.ft.err = true, ""
	client, pr, n := p.client, *p.pr, p.number
	p.reads.Do(p.Env, func() func() {
		ctx := context.Background()
		err := op(ctx, client, pr)
		var fresh github.PR
		var rerr error
		if err == nil {
			fresh, rerr = client.PR(ctx, n)
		}
		return func() {
			p.ft.busy = false
			switch {
			case err != nil:
				p.ft.err = err.Error()
			case rerr != nil:
				p.ft.err = rerr.Error()
			default:
				p.pr = &fresh
			}
			if err == nil && done != nil {
				done()
			}
			p.Env.Redraw()
		}
	})
	p.Env.Redraw()
}

func (p *PRPane) sendComment() {
	body := strings.TrimSpace(p.ft.text)
	if body == "" || p.ft.busy {
		return
	}
	// The text stays until the write succeeds, so a failed write keeps it.
	p.write(func(ctx context.Context, c github.Client, pr github.PR) error {
		return c.Comment(ctx, pr.Number, body)
	}, func() {
		if strings.TrimSpace(p.ft.text) == body {
			p.ft.text = ""
		}
	})
}

func (p *PRPane) approve() {
	p.write(func(ctx context.Context, c github.Client, pr github.PR) error {
		return c.Approve(ctx, pr.Number)
	}, nil)
}

// merge squash-merges the head commit that the pane shows.
func (p *PRPane) merge() {
	if !p.footerOpen() || p.ft.busy {
		return
	}
	if wait, _ := ciGate(p.pr.ChecksSummary); wait {
		return
	}
	// The orchestrator is found now, on the UI thread, from the PR's folder.
	var addr string
	if p.Orchestrator != nil {
		addr = p.Orchestrator(p.dir)
	}
	n, sha := p.pr.Number, p.pr.HeadSHA
	p.write(func(ctx context.Context, c github.Client, pr github.PR) error {
		return c.Merge(ctx, pr.Number, pr.HeadSHA)
	}, func() { p.mergedTell(addr, n, sha) })
}

// mergedTell tells the project's orchestrator about a merge (D105). The item
// is the worker's issue, so nothing else is marked. With no orchestrator the
// footer says that no one was told. A failed send shows its error and keeps
// "merged". It runs on the UI thread after the merge; the send runs off it.
func (p *PRPane) mergedTell(addr string, n int, sha string) {
	if addr == "" || p.Send == nil {
		p.ft.after = LabelPRMergedNoOne
		return
	}
	p.ft.after = LabelPRMergedBare
	text := "contr: PR #" + strconv.Itoa(n) + " merged at " + shortSHA(sha)
	send := p.Send
	p.reads.Do(p.Env, func() func() {
		err := send(addr, text)
		return func() {
			if err != nil {
				p.ft.err = "principal not told: " + err.Error()
			} else {
				p.ft.after = LabelPRMerged
			}
			p.Env.Redraw()
		}
	})
}

// shortSHA is the first 7 characters of a commit id.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// footerKeys adds the footer keys to the pane root: `c` focuses the field,
// Shift-Cmd-M merges.
func (p *PRPane) footerKeys(root ui.Element) ui.Element {
	root = root.OnShortcut(0, ui.KeyC, func() {
		if p.footerOpen() {
			p.refocusField = true
			p.Env.Redraw()
		}
	})
	return root.OnShortcut(ui.Cmd|ui.Shift, ui.KeyM, func() {
		if p.footerOpen() {
			p.merge()
		}
	})
}

// footer draws the footer under the body.
func (p *PRPane) footer(c *ui.Context) {
	if !p.footerShown() {
		return
	}
	pr := p.pr
	ui.Column(c).Label(LabelPRFooter).FillWidth().Shrink(0).Gap(8).
		Padding(10, theme.PRPanePad, 12).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		if p.ft.err != "" {
			ui.Text(c, p.ft.err).TextColor(theme.Color(theme.DiffDeleted)).Wrap()
		}
		if p.ft.after != "" {
			ui.Text(c, p.ft.after).TextColor(theme.Color(theme.TextDim)).NoWrap()
			return
		}
		in := ui.TextInput(c, &p.ft.text).Label(LabelPRField(p.number)).Placeholder(LabelPRField(p.number)).
			FillWidth().Border(1, theme.Color(theme.PRFieldBorder)).Radius(8).Padding(7, 10).Bind(&p.ft.field)
		if p.refocusField {
			p.refocusField = false
			p.ft.field.Focus(c)
		}
		if in.Submitted() {
			p.sendComment()
		}
		ui.Row(c).FillWidth().Gap(theme.AnswerButtonGap).AlignItems(ui.Center).Children(func() {
			var comment, approve ClickFn
			if !p.ft.busy {
				if strings.TrimSpace(p.ft.text) != "" {
					comment = p.sendComment
				}
				approve = p.approve
			}
			cardButton(c, LabelPRComment, ButtonOutlined, theme.Current().Ink95, comment)
			if p.canApprove() {
				cardButton(c, LabelPRApprove, ButtonOutlined, theme.Current().Ink95, approve)
			}
			ui.Box(c).Grow(1)
			if wait, _ := ciGate(pr.ChecksSummary); wait {
				icons.Sized(c, icons.Waiting, theme.IconSizeSmall).Shrink(0)
				ui.Text(c, LabelPRMergeWait).TextColor(theme.Color(theme.TextDim)).NoWrap()
				return
			}
			var merge ClickFn
			if !p.ft.busy {
				merge = p.merge
			}
			cardButton(c, LabelPRMerge, ButtonFilled, theme.Current().Ink95, merge)
		})
	})
}
