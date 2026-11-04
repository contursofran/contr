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
	"github.com/egoist/mygo/ui/markdown"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// Labels of the PR pane.
const (
	LabelPRPane   = "pull request"
	LabelPRClose  = "×"
	LabelPRWeb    = "GitHub ↗"
	LabelPRTabs   = "pr tabs"
	LabelPRLoad   = "Loading…"
	LabelPRTimes  = "Timeline"
	LabelPRNoDesc = "No description."
	LabelPRBody   = "pr body"
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
	// Orchestrator gives the address of the orchestrator terminal of the project of the
	// PR's folder, "" when there is none (D105). It runs on the UI thread. May be nil.
	Orchestrator func(dir string) string
	// Send types text into the terminal at an address and presses Enter. It runs off the UI
	// thread. May be nil.
	Send func(address, text string) error
	// OnClose runs after the pane closes. May be nil.
	OnClose func()
	// Item is what the pull request is for, like "#268": the issue of the worker that opened it.
	// [PRPane.Open] clears it; set it after. Empty: no line.
	Item string

	md      markdown.Options
	open    bool
	number  int
	dir     string
	pr      *github.PR
	err     string
	tab     int
	gen     uint64
	client  github.Client
	checks  prChecks
	handle  ui.Handle
	refocus bool

	ft           footerState
	refocusField bool
	filesState   prFiles // the Files tab (prfiles.go)
}

// Open shows pull request n of the repository in dir, and reads it.
func (p *PRPane) Open(dir string, n int) {
	p.open, p.number, p.dir = true, n, dir
	p.pr, p.err, p.tab, p.refocus, p.Item = nil, "", PRTabOverview, true, ""
	p.filesState = prFiles{}
	p.gen++
	gen := p.gen
	mk := p.Client
	if mk == nil {
		mk = github.New
	}
	client := mk(dir)
	p.client, p.checks = client, prChecks{}
	p.ft.text, p.ft.err, p.ft.busy, p.ft.after = "", "", false, ""
	p.readViewer(client)
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

// prPaneEnter is the enter motion (DESIGN "Motion"): in from the left 24 px
// with a fade, 0.25 s, cubic-bezier(0.2, 0.8, 0.2, 1). Opening another PR
// keeps the root, so it does not play again.
var prPaneEnter = ui.ElementTransition{
	Duration: 250 * time.Millisecond,
	Ease:     ui.CubicBezier(0.2, 0.8, 0.2, 1),
	Enter:    &ui.Motion{X: -24},
}

// View builds the pane, or nothing while it is closed.
func (p *PRPane) View(c *ui.Context) {
	if !p.open {
		return
	}
	root := ui.Column(c).Label(LabelPRPane).Width(theme.PRPaneWidth).Shrink(0).FillHeight().
		Background(theme.Color(theme.BgWindow)).BorderStartWidth(1).BorderColor(theme.Color(theme.Line)).
		Focusable().FocusRing(false).Bind(&p.handle)
	root = p.footerKeys(root.OnShortcut(0, ui.KeyEscape, p.Close))
	if !c.Preferences().ReduceMotion {
		root = root.Transition(prPaneEnter) // Exit is instant: no Exit motion
	}
	if p.refocus {
		p.refocus = false
		p.handle.Focus(c)
	}
	root.Children(func() {
		p.header(c)
		if p.pr != nil {
			p.tabs(c)
			p.body(c)
			p.footer(c)
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
			ui.Text(c, CIText(pr.ChecksSummary)).TextColor(theme.Color(ciTextColor(pr.ChecksSummary))).NoWrap()
			if p.Item != "" {
				ui.Text(c, "for "+p.Item).TextColor(theme.Color(theme.TextDim)).NoWrap()
			}
		})
	})
}

func (p *PRPane) tabs(c *ui.Context) {
	ui.Row(c).FillWidth().Shrink(0).Padding(theme.PRPanePad, theme.PRPanePad, 0).Children(func() {
		kit.Switch(c, LabelPRTabs, &p.tab, PRTabLabels(p.pr)...)
	})
}

func (p *PRPane) body(c *ui.Context) {
	switch p.tab {
	case PRTabFiles:
		p.filesTab(c)
		return
	case PRTabChecks:
		p.checksTab(c)
		return
	}
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

// mdTheme is the markdown look in contr's greys (D76): mono font, no hue,
// headings near body size. Links are grey; ui.Link underlines them.
func mdTheme() *markdown.Theme {
	text, dim, line := theme.Color(theme.Text2), theme.Color(theme.TextDim), theme.Color(theme.Line)
	th := &markdown.Theme{
		FontSize: theme.FontSize, Font: theme.FontFamily, CodeFont: theme.FontFamily,
		Text: text, Muted: dim, Link: theme.Color(theme.Text), Strong: theme.Color(theme.Text),
		Code: text, CodeBackground: theme.Color(theme.Line),
		QuoteBar: line, QuoteText: dim, Rule: line,
		CodeBlockBackground: theme.Color(theme.Line), CodeBlockBorder: line,
		TableBorder: line, TableHeader: theme.Color(theme.Line),
		Mark: theme.Color(theme.Line), Checkbox: dim,
	}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{Size: 1.1 - 0.02*float32(i), Weight: 600, Color: theme.Color(theme.Text), Top: 1, Bottom: 0.4}
	}
	return th
}

// description draws the PR text as markdown. Links open through OpenURL,
// only for http and https addresses. An empty text shows a dim note.
func (p *PRPane) description(c *ui.Context) {
	if strings.TrimSpace(p.pr.Body) == "" {
		ui.Text(c, LabelPRNoDesc).TextColor(theme.Color(theme.TextDim))
		return
	}
	if p.md.Theme == nil {
		p.md.Theme = mdTheme()
	}
	p.md.OnLink = func(url string) {
		u := strings.ToLower(url)
		if p.OpenURL != nil && (strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) {
			p.OpenURL(url)
		}
	}
	ui.Column(c).FillWidth().TextColor(theme.Color(theme.Text2)).Children(func() {
		markdown.View(c, p.pr.Body, &p.md)
	})
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
