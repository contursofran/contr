package app

// The PR pane (#151, D59): a 560 px column at the right of the terminal
// area, opened by a `#N` badge. It shows the header and the Overview tab.
// The Files and Checks tabs come in later slices. The GitHub read runs on a
// goroutine; only the newest read lands.

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// Labels of the PR pane.
const (
	LabelPRPane  = "pull request"
	LabelPRClose = "×"
	LabelPRWeb   = "GitHub ↗"
	LabelPRTabs  = "pr tabs"
	LabelPRLoad  = "Loading…"
	LabelPRTimes = "Timeline"
	LabelPRBody  = "pr body"
)

// PR tab ids, as option indexes of the tab switch.
const (
	PRTabOverview = iota
	PRTabFiles
	PRTabChecks
)

// PRPane is the pull request pane. Methods and View run on the UI thread.
type PRPane struct {
	// Client gives the GitHub client for a folder. Nil: [github.New].
	Client func(dir string) github.Client
	// Run runs the read off the UI thread.
	Run Runner
	// Invalidate asks for a redraw. May be nil.
	Invalidate func()
	// OpenURL opens a web address. May be nil.
	OpenURL func(string)
	// Now is the clock. Nil: time.Now.
	Now func() time.Time
	// OnClose runs after the pane closes. May be nil.
	OnClose func()

	open    bool
	number  int
	dir     string
	pr      *github.PR
	err     string
	tab     int
	gen     uint64
	handle  ui.Handle
	refocus bool
}

// Open shows pull request n of the repository in dir, and reads it.
func (p *PRPane) Open(dir string, n int) {
	p.open, p.number, p.dir = true, n, dir
	p.pr, p.err, p.tab, p.refocus = nil, "", PRTabOverview, true
	p.gen++
	gen := p.gen
	mk := p.Client
	if mk == nil {
		mk = github.New
	}
	client := mk(dir)
	p.Run.Do(func() func() {
		pr, err := client.PR(context.Background(), n)
		return func() {
			if gen != p.gen {
				return // an older read, or the pane closed
			}
			if err != nil {
				p.err = err.Error()
			} else {
				p.pr = &pr
			}
			p.redraw()
		}
	})
	p.redraw()
}

// Close hides the pane. A read in flight is dropped.
func (p *PRPane) Close() {
	if !p.open {
		return
	}
	p.open = false
	p.gen++
	p.pr, p.err = nil, ""
	p.redraw()
	if p.OnClose != nil {
		p.OnClose()
	}
}

// IsOpen tells whether the pane shows.
func (p *PRPane) IsOpen() bool { return p.open }

// Number is the pull request that the pane shows.
func (p *PRPane) Number() int { return p.number }

// Loaded is the pull request read, or nil while loading or after an error.
func (p *PRPane) Loaded() *github.PR { return p.pr }

// Err is the text of the last failed read.
func (p *PRPane) Err() string { return p.err }

func (p *PRPane) redraw() {
	if p.Invalidate != nil {
		p.Invalidate()
	}
}

func (p *PRPane) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// PRTabLabels are the tab names. Files and Checks carry their counts.
func PRTabLabels(pr *github.PR) []string {
	files, checks := 0, 0
	if pr != nil {
		files = pr.ChangedFiles
		s := pr.ChecksSummary
		checks = s.Pass + s.Fail + s.Pending + s.Skip
	}
	return []string{"Overview", "Files " + strconv.Itoa(files), "Checks " + strconv.Itoa(checks)}
}

// prAge is a short age: "now", "14m", "3h", "2d".
func prAge(now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case at.IsZero() || d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// StateText is the word of the state badge.
func StateText(pr github.PR) string {
	switch {
	case pr.State == "MERGED":
		return "merged"
	case pr.State == "CLOSED":
		return "closed"
	case pr.IsDraft:
		return "draft"
	case pr.ReviewDecision == "APPROVED":
		return "approved"
	}
	return "open"
}

// stateBadge draws the state badge (DESIGN.md, PR viewer header line 3):
// padding 1×8, radius 4; `merged` in the `you` style, `approved` with a
// #5A5A5A border, other states with a #3A3A3A border and secondary text.
func stateBadge(c *ui.Context, state string) {
	b := ui.Row(c).PaddingY(1).PaddingX(8).Radius(theme.BadgeRadius).Shrink(0).Label("state " + state)
	var t uint32 = theme.Text2
	switch state {
	case "merged":
		b = b.Background(theme.Color(theme.YouBg))
		t = theme.YouText
	case "approved":
		b = b.BorderWidth(1).BorderColor(theme.Color(theme.BadgeApprovedBorder))
	default:
		b = b.BorderWidth(1).BorderColor(theme.Color(theme.LineStrong))
	}
	b.Children(func() {
		txt := ui.Text(c, state).TextColor(theme.Color(t)).NoWrap()
		if state == "merged" {
			txt.Bold()
		}
	})
}

// CIText is the one-line summary of the checks.
func CIText(s github.CheckSummary) string {
	total := s.Pass + s.Fail + s.Pending + s.Skip
	switch {
	case total == 0:
		return "no checks"
	case s.Fail > 0:
		return fmt.Sprintf("CI failing · %d of %d", s.Fail, total)
	case s.Pending > 0:
		return "CI running"
	}
	return fmt.Sprintf("CI green · %d checks", total)
}

func ciIcon(s github.CheckSummary) icons.Status {
	switch {
	case s.Fail > 0:
		return icons.Blocked
	case s.Pending > 0:
		return icons.Working
	case s.Pass+s.Skip == 0:
		return icons.NotStarted
	}
	return icons.Done
}

// repoName is the repository name from a pull request address.
func repoName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// StatText is "+86 −12 · 3 files".
func StatText(pr github.PR) string {
	noun := "files"
	if pr.ChangedFiles == 1 {
		noun = "file"
	}
	return fmt.Sprintf("+%d −%d · %d %s", pr.Additions, pr.Deletions, pr.ChangedFiles, noun)
}

func eventIcon(e github.Event) icons.Status {
	switch e.Kind {
	case "reviewed":
		switch e.Text {
		case "APPROVED":
			return icons.Done
		case "CHANGES_REQUESTED":
			return icons.Blocked
		}
		return icons.Idle
	case "committed", "merged":
		return icons.Done
	case "closed":
		return icons.Blocked
	}
	return icons.Idle
}

// eventText is the sentence of a timeline row.
func eventText(e github.Event) string {
	switch e.Kind {
	case "reviewed":
		switch e.Text {
		case "APPROVED":
			return "approved"
		case "CHANGES_REQUESTED":
			return "requested changes"
		case "COMMENTED":
			return "reviewed"
		}
		return strings.ToLower(e.Text)
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	case "committed":
		return "pushed: " + e.Text
	}
	return firstLine(e.Text)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// View builds the pane, or nothing while it is closed.
func (p *PRPane) View(c *ui.Context) {
	if !p.open {
		return
	}
	root := ui.Column(c).Label(LabelPRPane).Width(theme.PRPaneWidth).Shrink(0).FillHeight().
		Background(theme.Color(theme.BgWindow)).BorderStartWidth(1).BorderColor(theme.Color(theme.Line)).
		Focusable().FocusRing(false).Bind(&p.handle)
	root = root.OnShortcut(0, ui.KeyEscape, p.Close)
	if p.refocus {
		p.refocus = false
		p.handle.Focus(c)
	}
	root.Children(func() {
		p.header(c)
		if p.pr != nil {
			p.tabs(c)
			p.body(c)
			return
		}
		text := LabelPRLoad
		if p.err != "" {
			text = p.err
		}
		ui.Text(c, text).TextColor(theme.Color(theme.TextDim)).Padding(theme.PRPanePad)
	})
}

func (p *PRPane) header(c *ui.Context) {
	pr := p.pr
	ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(theme.PRPanePad).
		BorderWidth(0, 0, 1, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, "#"+strconv.Itoa(p.number)).Bold().NoWrap()
			title := ""
			if pr != nil {
				title = pr.Title
			}
			ui.Text(c, title).Bold().Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if pr != nil && pr.URL != "" {
				ui.ButtonBase(c).Label(LabelPRWeb).Cursor(ui.CursorPointer).Shrink(0).
					TextColor(theme.Color(theme.TextDim)).OnClick(func() {
					if p.OpenURL != nil {
						p.OpenURL(pr.URL)
					}
				}).Children(func() { ui.Text(c, LabelPRWeb) })
			}
			ui.ButtonBase(c).Label(LabelPRClose).Tooltip("Close the pull request pane").Cursor(ui.CursorPointer).Shrink(0).
				Size(theme.TitleButtonSize, theme.TitleButtonSize).Center().
				TextColor(theme.Color(theme.TextDim)).OnClick(p.Close).Children(func() { ui.Text(c, "×") })
		})
		if pr == nil {
			return
		}
		ui.Row(c).FillWidth().Gap(10).Wrap().TextColor(theme.Color(theme.TextDim)).Children(func() {
			for _, s := range []string{repoName(pr.URL), pr.HeadRef + " → " + pr.BaseRef, pr.Author, StatText(*pr)} {
				if s != "" && s != " → " {
					ui.Text(c, s).NoWrap()
				}
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			stateBadge(c, StateText(*pr))
			icons.Sized(c, ciIcon(pr.ChecksSummary), theme.IconSizeSmall)
			ui.Text(c, CIText(pr.ChecksSummary)).TextColor(theme.Color(theme.Text2)).NoWrap()
		})
	})
}

func (p *PRPane) tabs(c *ui.Context) {
	ui.Row(c).FillWidth().Shrink(0).Padding(theme.PRPanePad, theme.PRPanePad, 0).Children(func() {
		kit.Switch(c, LabelPRTabs, &p.tab, PRTabLabels(p.pr)...)
	})
}

func (p *PRPane) body(c *ui.Context) {
	if p.tab != PRTabOverview {
		ui.Text(c, "Not built yet.").TextColor(theme.Color(theme.TextDim)).Padding(theme.PRPanePad)
		return
	}
	ui.Scroll(c).Label(LabelPRBody).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(theme.PRPanePad).Gap(14).Children(func() {
			p.description(c)
			p.timeline(c)
		})
	})
}

// description draws the PR text. It shows markdown lightly: headings are
// bold, list items get a bullet, and a blank line is a gap.
func (p *PRPane) description(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(4).TextColor(theme.Color(theme.Text2)).Children(func() {
		for _, l := range descLines(p.pr.Body) {
			if l.blank {
				ui.Box(c).Height(theme.ReportBlankHeight)
				continue
			}
			t := ui.Text(c, l.text).FillWidth()
			if l.bold {
				t = t.Bold().TextColor(theme.Color(theme.Text))
			}
		}
	})
}

type descLine struct {
	text        string
	bold, blank bool
}

// descLines turns markdown text into lines to draw.
func descLines(body string) []descLine {
	var out []descLine
	inCode := false
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		switch {
		case inCode:
			out = append(out, descLine{text: line})
		case strings.TrimSpace(line) == "":
			if len(out) > 0 && !out[len(out)-1].blank {
				out = append(out, descLine{blank: true})
			}
		case strings.HasPrefix(line, "#"):
			out = append(out, descLine{text: strings.TrimSpace(strings.TrimLeft(line, "#")), bold: true})
		default:
			t := strings.TrimSpace(line)
			for _, m := range []string{"- ", "* "} {
				if strings.HasPrefix(t, m) {
					t = "• " + strings.TrimSpace(t[2:])
					break
				}
			}
			out = append(out, descLine{text: t})
		}
	}
	for len(out) > 0 && out[len(out)-1].blank {
		out = out[:len(out)-1]
	}
	return out
}

func (p *PRPane) timeline(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Text(c, LabelPRTimes).TextColor(theme.Color(theme.TextDim)).Padding(0, 0, 6)
		now := p.now()
		for _, e := range p.pr.Timeline {
			ui.Row(c).FillWidth().Gap(10).PaddingY(6).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
				icons.Sized(c, eventIcon(e), theme.IconSizeSmall).Shrink(0)
				ui.Text(c, e.Who).Bold().Width(theme.PRWhoWidth).Shrink(0).SingleLine().Ellipsis("…")
				ui.Text(c, eventText(e)).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").
					TextColor(theme.Color(theme.Text2))
				ui.Text(c, prAge(now, e.At)).Shrink(0).NoWrap().TextColor(theme.Color(theme.TextDim2))
			})
		}
	})
}
