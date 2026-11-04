package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// prRunner is a fake gh runner: it answers pr view with a canned reply per number.
type prRunner struct {
	reply func(n string) ([]byte, error)
	dirs  []string
}

func (r *prRunner) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	r.dirs = append(r.dirs, dir)
	return r.reply(args[2])
}

const prJSON = `{"number":642,"title":"Fix bag drag target","url":"https://example.test/o/client/pull/642",
"state":"OPEN","body":"## Why\n\nDrops land under the cursor.\n- one\n- two","author":{"login":"b-1140"},
"headRefName":"fix/bag-drag","baseRefName":"main","headRefOid":"abc","additions":86,"deletions":12,"changedFiles":3,
"isDraft":false,"mergeable":"MERGEABLE","mergedAt":null,"closedAt":null,
"comments":[{"author":{"login":"alice"},"body":"Thanks\nsecond line","createdAt":"2026-11-03T11:50:00Z"}],
"reviews":[{"author":{"login":"r-1139"},"body":"","state":"APPROVED","submittedAt":"2026-11-03T11:46:00Z"}],
"commits":[],"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"}]}`

type prRig struct {
	pane   *PRPane
	run    *prRunner
	tt     *ui.Tester
	queue  []func()
	opened []string
	closed int
}

func newPRRig(t *testing.T) *prRig {
	t.Helper()
	r := &prRig{run: &prRunner{reply: func(string) ([]byte, error) { return []byte(prJSON), nil }}}
	r.pane = &PRPane{
		Client:  func(dir string) github.Client { return github.Client{R: r.run, Dir: dir} },
		Run:     Runner{Go: func(f func()) { r.queue = append(r.queue, f) }},
		OpenURL: func(u string) { r.opened = append(r.opened, u) },
		Now:     func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
		OnClose: func() { r.closed++ },
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Font(theme.FontFamily).FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() { r.pane.View(c) })
	}, 900, 700)
	return r
}

// flush runs the queued reads, in order.
func (r *prRig) flush() {
	q := r.queue
	r.queue = nil
	for _, f := range q {
		f()
	}
	r.tt.Frame()
}

func TestPRPaneShowsLoadingWhileTheReadRuns(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.tt.Frame()
	if !r.tt.HasText(LabelPRLoad) || !r.tt.HasText("#642") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	if r.tt.HasText(LabelPRWeb) {
		t.Error("no web link before the read")
	}
}

func TestPRPaneHeaderTabsAndOverview(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	for _, want := range []string{"#642", "Fix bag drag target", LabelPRWeb, LabelPRClose, "client",
		"fix/bag-drag → main", "b-1140", "+86 −12 · 3 files", "open", "CI green · 3 checks",
		"Overview", "Files 3", "Checks 3", "Why", "Drops land under the cursor.", "one", "two",
		LabelPRTimes, "alice", "Thanks", "r-1139", "approved", "14m", "10m"} {
		if !r.tt.HasText(want) {
			t.Errorf("missing %q in %q", want, r.tt.Texts())
		}
	}
	if r.run.dirs[0] != "/w" {
		t.Errorf("gh ran in %q", r.run.dirs[0])
	}
}

func TestPRPaneShowsTheErrorText(t *testing.T) {
	r := newPRRig(t)
	r.run.reply = func(string) ([]byte, error) { return nil, errors.New("not logged in") }
	r.pane.Open("/w", 1)
	r.flush()
	if !r.tt.HasText("gh: not logged in") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	if r.pane.Loaded() != nil {
		t.Error("no PR after an error")
	}
}

func TestPRPaneCloseButtonAndEscapeClose(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	if err := r.tt.Click(LabelPRClose); err != nil {
		t.Fatal(err)
	}
	if r.pane.IsOpen() || r.closed != 1 {
		t.Error("× should close the pane")
	}
	r.pane.Open("/w", 642)
	r.flush()
	r.tt.Key(0, ui.KeyEscape)
	if r.pane.IsOpen() || r.closed != 2 {
		t.Error("Escape should close the pane")
	}
	r.tt.Frame()
	if r.tt.HasText("#642") {
		t.Error("a closed pane draws nothing")
	}
}

func TestPRPaneWebLinkOpensTheAddress(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	if err := r.tt.Click(LabelPRWeb); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.opened, []string{"https://example.test/o/client/pull/642"}) {
		t.Errorf("opened %v", r.opened)
	}
}

func TestPRPaneStaleResultDropped(t *testing.T) {
	r := newPRRig(t)
	r.run.reply = func(n string) ([]byte, error) {
		return []byte(strings.Replace(prJSON, "Fix bag drag target", "title "+n, 1)), nil
	}
	r.pane.ft.viewer = "owner" // the login is read: only the PR reads queue
	r.pane.Open("/w", 1)
	r.pane.Open("/w", 2)
	if len(r.queue) != 2 {
		t.Fatalf("queued %d", len(r.queue))
	}
	r.queue[1]() // the newer read lands first
	r.queue[0]() // the older read lands late
	r.queue = nil
	r.tt.Frame()
	if !r.tt.HasText("title 2") || r.tt.HasText("title 1") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRPaneReadAfterCloseIsDropped(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.pane.Close()
	r.flush()
	if r.pane.Loaded() != nil {
		t.Error("a read that ends after close should not land")
	}
}

func TestPRPaneTabsSwitch(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	if err := r.tt.Click("Files 3"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.tt.HasText(LabelPRTimes) {
		t.Error("Files tab should not draw the timeline")
	}
}

func TestPRTextHelpers(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		d    time.Duration
		want string
	}{{10 * time.Second, "now"}, {14 * time.Minute, "14m"}, {3 * time.Hour, "3h"}, {72 * time.Hour, "3d"}} {
		if got := prAge(now, now.Add(-c.d)); got != c.want {
			t.Errorf("%v: %q", c.d, got)
		}
	}
	if got := CIText(github.CheckSummary{Pass: 2, Fail: 1}); got != "CI failing · 1 of 3" {
		t.Error(got)
	}
	if got := CIText(github.CheckSummary{Pass: 2, Pending: 1}); got != "CI running" {
		t.Error(got)
	}
	if got := CIText(github.CheckSummary{}); got != "no checks" {
		t.Error(got)
	}
	if got := StateText(github.PR{State: "OPEN", IsDraft: true}); got != "draft" {
		t.Error(got)
	}
	if got := StateText(github.PR{State: "MERGED"}); got != "merged" {
		t.Error(got)
	}
	if got := StateText(github.PR{State: "OPEN", ReviewDecision: "APPROVED"}); got != "approved" {
		t.Error(got)
	}
	if got := StateText(github.PR{State: "OPEN", IsDraft: true, ReviewDecision: "APPROVED"}); got != "draft" {
		t.Error(got)
	}
	if got := StateText(github.PR{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED"}); got != "open" {
		t.Error(got)
	}
}

func TestTabsCollapseWhilePRIsOpen(t *testing.T) {
	tabs := tabsOf()
	for i := range tabs {
		tabs[i].ID = []string{"lead", "#5", "b2"}[i]
	}
	b := newBar(t, tabs, true)
	b.tt.Frame()
	if !slices.Contains(b.tt.Texts(), "#5 · fix-x") {
		t.Fatalf("expanded tab text missing: %q", b.tt.Texts())
	}
	b.bar.collapsed = true
	b.tt.Frame()
	if slices.Contains(b.tt.Texts(), "#5 · fix-x") || !slices.Contains(b.tt.Texts(), "#5") || !slices.Contains(b.tt.Texts(), orchestratorMark+"lead") {
		t.Errorf("collapsed texts %q", b.tt.Texts())
	}
}

func TestTabIDIsTheIssueOrTheName(t *testing.T) {
	if got := tabID(agents.Worker{Name: "b-1", Issue: "151", Role: "builder"}); got != "#151" {
		t.Error(got)
	}
	if got := tabID(agents.Worker{Name: "b-1"}); got != "b-1" {
		t.Error(got)
	}
}

func TestSidebarBadgeOpensPR(t *testing.T) {
	h, wt1, _ := heldWorktree(t)
	var gotDir string
	var gotN int
	h.list.OpenPR = func(dir string, n int) { gotDir, gotN = dir, n }
	h.list.groups[0].Rows[0].Badge = &spaces.Badge{Kind: spaces.BadgePR, PR: 640}
	h.tt.Frame()
	if err := h.tt.Click(LabelBadge); err != nil {
		t.Fatal(err)
	}
	if gotDir != wt1 || gotN != 640 {
		t.Errorf("OpenPR(%q, %d)", gotDir, gotN)
	}
}

func TestBoardWorkLabelOpensPR(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "b-9", Address: "t1", Role: "builder", Issue: "151", Worktree: "/wt/a", Project: "a", Live: true, Status: "working"},
		{Name: "b-8", Address: "t2", Role: "builder", Project: "a", Live: true},
	}}
	p, _ := newBoardPanel(snap, "a")
	var dir string
	var n int
	picked := ""
	p.OpenPR = func(d string, num int) { dir, n = d, num }
	p.Select = func(a string) { picked = a }
	tt := boardTester(p)
	tt.Frame()
	if err := tt.Click(LabelWorkPR("#151")); err != nil {
		t.Fatal(err)
	}
	if dir != "/wt/a" || n != 151 || picked != "" {
		t.Errorf("OpenPR(%q, %d), picked %q", dir, n, picked)
	}
	if _, ok := tt.Find(LabelWorkPR("b-8")); ok {
		t.Error("a worker without an issue has no PR button")
	}
}

func TestFrameOpenPRShowsThePaneAndCollapsesTabs(t *testing.T) {
	f := NewFrame()
	r := newPRRig(t)
	f.PR = r.pane
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if f.PROpen() {
		t.Fatal("closed at start")
	}
	f.OpenPR("/w", 642)
	r.flush()
	tt.Frame()
	if !f.PROpen() || !tt.HasText("Fix bag drag target") {
		t.Errorf("pane missing: %q", tt.Texts())
	}
	if rect := box(t, tt, LabelPRPane); rect.W != theme.PRPaneWidth {
		t.Errorf("pane width %v", rect.W)
	}
	f.ClosePR()
	tt.Frame()
	if f.PROpen() || tt.HasText("Fix bag drag target") {
		t.Error("ClosePR should hide the pane")
	}
}

// TestPRPaneEscapeOnlyWithFocus: Esc closes the pane only while the pane has
// the focus (D72). With the focus on another part (the terminal, in the
// app), Esc must reach that part and the pane stays open.
func TestPRPaneEscapeOnlyWithFocus(t *testing.T) {
	r := newPRRig(t)
	var other ui.Handle
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Font(theme.FontFamily).FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() {
			ui.Box(c).Label("other").Width(200).FillHeight().Focusable().Bind(&other)
			r.pane.View(c)
		})
	}, 900, 700)
	r.pane.Open("/w", 642)
	r.flush()
	if err := r.tt.Click("other"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.tt.Focused("other") {
		t.Fatal("the other part did not take the focus")
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if !r.pane.IsOpen() || r.closed != 0 {
		t.Error("Escape closed the pane while another part had the focus")
	}
}

// TestPRPaneStateBadge: an approved PR shows `approved`; a merged PR shows
// `merged` in the `you` style (white background).
func TestPRPaneStateBadge(t *testing.T) {
	r := newPRRig(t)
	r.run.reply = func(string) ([]byte, error) {
		return []byte(strings.Replace(prJSON, `"isDraft":false,`, `"isDraft":false,"reviewDecision":"APPROVED",`, 1)), nil
	}
	r.pane.Open("/w", 642)
	r.flush()
	if _, ok := r.tt.Find("state approved"); !ok || !r.tt.HasText("approved") {
		t.Errorf("no approved badge; texts %v", r.tt.Texts())
	}

	r.run.reply = func(string) ([]byte, error) {
		return []byte(strings.Replace(prJSON, `"state":"OPEN"`, `"state":"MERGED"`, 1)), nil
	}
	r.pane.Open("/w", 642)
	r.flush()
	b, ok := r.tt.Find("state merged")
	if !ok {
		t.Fatalf("no merged badge; texts %v", r.tt.Texts())
	}
	img := r.tt.Image()
	scale := float32(img.Bounds().Dx()) / 900
	px := img.RGBAAt(int((b.X+2)*scale), int((b.Y+b.H/2)*scale))
	if got := uint32(px.R)<<16 | uint32(px.G)<<8 | uint32(px.B); got != theme.YouBg {
		t.Errorf("merged badge background = %06X, want %06X", got, theme.YouBg)
	}
}

func TestPRPaneDescriptionRendersMarkdown(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	for _, want := range []string{"Why", "Drops land under the cursor.", "one", "two"} {
		if !r.tt.HasText(want) {
			t.Errorf("missing %q in %q", want, r.tt.Texts())
		}
	}
	if r.tt.HasText("## Why") {
		t.Error("heading marks must not show")
	}
	if r.tt.HasText("No description.") {
		t.Error("body exists")
	}
}

func TestPRPaneMarkdownThemeIsGrey(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	th := r.pane.md.Theme
	if th == nil {
		t.Fatal("pane passes no markdown theme")
	}
	if th.Font != theme.FontFamily || th.CodeFont != theme.FontFamily {
		t.Errorf("fonts %q, %q", th.Font, th.CodeFont)
	}
	if th.Link != theme.Color(theme.Text) || th.Text != theme.Color(theme.Text2) {
		t.Errorf("link %v text %v", th.Link, th.Text)
	}
	for _, c := range []ui.Color{th.Link, th.Text, th.Mark, th.Checkbox, th.Strong, th.Code} {
		if c.R != c.G || c.G != c.B {
			t.Errorf("colour %v has a hue", c)
		}
	}
	for i, h := range th.Headings {
		if h.Size > 1.2 {
			t.Errorf("heading %d size %v", i+1, h.Size)
		}
	}
}

func TestPRPaneEmptyDescription(t *testing.T) {
	r := newPRRig(t)
	r.run.reply = func(string) ([]byte, error) {
		return []byte(strings.Replace(prJSON, `"body":"## Why\n\nDrops land under the cursor.\n- one\n- two"`, `"body":"  "`, 1)), nil
	}
	r.pane.Open("/w", 642)
	r.flush()
	if !r.tt.HasText("No description.") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRPaneDescriptionLinksOpenOnlyWeb(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	h := r.pane.md.OnLink
	if h == nil {
		t.Fatal("no link handler")
	}
	h("https://example.test/x")
	h("javascript:alert(1)")
	h("file:///etc/passwd")
	if len(r.opened) != 1 || r.opened[0] != "https://example.test/x" {
		t.Errorf("opened %q", r.opened)
	}
}
