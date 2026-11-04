package app

// The spaces column body: the `Spaces` label, the `This computer` device header, then one
// repo line and one row per worktree. The header (window-button inset,
// toggle, wordmark) is the frame's sidebar header. Ported from the Rust
// contr module spaces.
//
// Same approach as the agents list (D12): MyGo builds the whole view on a
// redraw, so a worktree row is a cached layer, the working icon draws its own
// turn, and [SpacesList.Sync] reports which rows changed. The git scan runs on
// a goroutine; its result reaches the list through the post func (D15 says
// when it scans).

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/projects"
)

// RescanAfter is the age at which a scan is old, and the time between rescans
// while the window is shown, so that a changed branch appears (D15). An old
// scan is also redone on the next agents change and when the window is shown.
const RescanAfter = 10 * time.Second

// Element labels of the spaces column.
const (
	LabelSpacesHeader = "spaces header"
	LabelSpacesNew    = "spaces new"
	LabelSpacesProj   = "spaces projects"
	LabelDeviceHeader = "device header"
	LabelHoldYou      = "hold-you"
	LabelBadge        = "badge"
)

// RepoLabel is the label of a repo line.
func RepoLabel(g spaces.RepoGroup) string { return "repo " + g.Name }

// WorktreeRowLabel is the row's label, which is also its accessible label:
// "{name}, {status}", then ", {hold}" when the row has a second line, then
// ", {badge}" and ", selected".
func WorktreeRowLabel(row spaces.WorktreeRow, selected bool) string {
	label := row.Name + ", " + row.Status.Label()
	if row.Hold != nil {
		label += ", " + *row.Hold
	}
	if row.Badge != nil {
		label += ", " + BadgeText(*row.Badge)
	}
	if selected {
		label += ", selected"
	}
	return label
}

// BadgeText is the text a badge shows: "#640" for a pull request, the
// owner's value as it is otherwise.
func BadgeText(b spaces.Badge) string {
	if b.Kind == spaces.BadgePR {
		return "#" + strconv.FormatUint(uint64(b.PR), 10)
	}
	return b.You
}

// RowRef names a worktree row by its place: the repo group and the row in it.
type RowRef struct{ Group, Row int }

// SpacesChange tells what [SpacesList.Sync] changed.
type SpacesChange struct {
	// Rebuilt: the groups have another shape (other repos, or other row
	// counts), so the whole column changes.
	Rebuilt bool
	// Rows are the rows that show other data or another selection mark, when
	// the shape is the same.
	Rows []RowRef
	// Online: the hc connection came or went (the hc problem line).
	Online bool
}

// Any tells whether the column needs a redraw.
func (c SpacesChange) Any() bool { return c.Rebuilt || c.Online || len(c.Rows) > 0 }

// SpacesList is the spaces column body. Methods run on the UI thread.
type SpacesList struct {
	// OpenPR runs when a pull request badge is clicked, with the row's folder and the number.
	OpenPR func(dir string, n int)
	// OnNew runs when `+ new` is clicked. Nil: the button does nothing.
	OnNew func()
	// OnProjects runs when the `projects` pill is clicked (D113). Nil: the
	// pill does nothing.
	OnProjects func()

	snap      func() agentstate.Snapshot
	selection *state.Selection

	// visible tells whether the window is shown. Nil counts as shown.
	visible func() bool
	// scanFn reads git. It runs on a goroutine, started by start.
	scanFn func([]string) git.Scan
	// statsFn runs git diff for the worktrees of a scan (D52). It runs on the
	// same goroutine as scanFn, after the scan has been posted. Nil gives no stats.
	statsFn func(git.Scan) map[string]git.DiffStat
	start   func(func())
	// post runs fn on the UI thread (Window.Update). Nil runs it at once.
	post func(func())
	now  func() time.Time
	// afterFunc arms the rescan timer. Tests replace it with a fake clock.
	afterFunc func(d time.Duration, f func()) stopper

	groups []spaces.RepoGroup
	// projects is the owner's order and hidden groups (D109). Nil keeps the
	// tree order and hides nothing. UI thread only.
	projects *projects.Store
	online   bool
	// line is the hc problem while contr does not follow the core, else "".
	line string
	// shown is the id of the selected worktree at the last rebuild.
	shown string

	scan     git.Scan
	stats    map[string]git.DiffStat
	scanned  []string
	lastScan time.Time
	hasScan  bool
	// gen counts the scans started and the scans dropped. A finished scan of
	// an older generation is dropped, so only the newest result lands.
	gen   uint64
	scans int

	// The rescan timer (D15). It runs only while the window is shown.
	timerShown bool
	shownAt    time.Time
	timer      stopper
	// timerGen counts show and hide changes; a wake-up of an older cycle stops.
	timerGen uint64
	closed   bool
	// builds counts the builds of each row by root. Tests read it.
	builds map[string]int
}

// NewSpacesList returns the list. snap (may be nil) gives the hc terminals,
// selection the card. visible (may be
// nil) tells whether the window is shown; post (may be nil) runs a func on
// the UI thread, as Window.Update does. The list is empty until
// [SpacesList.Sync], which the owner calls at the start and when the agents
// changed.
func NewSpacesList(snap func() agentstate.Snapshot, selection *state.Selection, visible func() bool, post func(func())) *SpacesList {
	l := &SpacesList{
		snap: snap, selection: selection,
		visible: visible, post: post, scanFn: git.ScanCwds, statsFn: runStats, start: func(f func()) { go f() },
		now: time.Now, builds: map[string]int{},
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
	}
	return l
}

// Groups returns the repo groups shown. Empty without terminals.
func (l *SpacesList) Groups() []spaces.RepoGroup { return l.groups }

// SetProjects sets the store that orders and hides the groups (D109) and
// rebuilds the list. Call it on the UI thread. It returns what changed.
func (l *SpacesList) SetProjects(s *projects.Store) SpacesChange {
	l.projects = s
	return l.rebuild()
}

// applyProjects puts groups in the stored order and leaves the hidden ones
// out (D109). Without a store it returns groups as they are.
func (l *SpacesList) applyProjects(groups []spaces.RepoGroup) []spaces.RepoGroup {
	if l.projects == nil || len(groups) == 0 {
		return groups
	}
	dirs := make([]string, len(groups))
	byDir := make(map[string]spaces.RepoGroup, len(groups))
	for i, g := range groups {
		dirs[i] = g.CommonDir
		byDir[g.CommonDir] = g
	}
	out := make([]spaces.RepoGroup, 0, len(groups))
	for _, d := range l.projects.Order(dirs) {
		if !l.projects.Hidden(d) {
			out = append(out, byDir[d])
		}
	}
	return out
}

func (l *SpacesList) shownNow() bool { return l.visible == nil || l.visible() }

// Sync brings the column up to the hc terminals and the connection. It
// starts a scan when a terminal sits in another folder, or when the last scan
// is old and the window is shown (D15). With no folder there is nothing to
// scan, and the scan is dropped. It then rebuilds the groups from the scan
// that it has. A selected worktree that no row has any more leaves the
// selection; an agent selection stays.
func (l *SpacesList) Sync() SpacesChange {
	cwds := spaces.Dirs(l.terms())
	switch {
	case len(cwds) == 0:
		l.forgetScan()
	case !slices.Equal(cwds, l.scanned) || (l.stale() && l.shownNow()):
		l.startScan(cwds)
	}
	return l.rebuild()
}

// SetVisible follows the window. Shown: the list syncs at once, which rescans
// an old scan, and the timer wakes one interval after the later of the last
// scan and now. Hidden: the timer stops and nothing else is scheduled, as the
// host sampler does (D11). Call it on the UI thread.
func (l *SpacesList) SetVisible(visible bool) SpacesChange {
	if l.closed || l.timerShown == visible {
		return SpacesChange{}
	}
	l.timerShown = visible
	l.timerGen++
	l.stopTimer()
	if !visible {
		return SpacesChange{}
	}
	l.shownAt = l.now()
	change := l.Sync()
	l.armTimer()
	return change
}

// Close stops the timer for good.
func (l *SpacesList) Close() {
	l.closed = true
	l.timerGen++
	l.stopTimer()
}

func (l *SpacesList) stopTimer() {
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

// wait is the time to the next wake-up: one interval after the later of the
// last scan and the moment the window was shown. Without cwds nothing is
// scanned, so it is one interval.
func (l *SpacesList) wait() time.Duration {
	if len(l.scanned) == 0 {
		return RescanAfter
	}
	base := l.shownAt
	if l.hasScan && l.lastScan.After(base) {
		base = l.lastScan
	}
	return max(RescanAfter-l.now().Sub(base), 0)
}

func (l *SpacesList) armTimer() {
	gen := l.timerGen
	l.timer = l.afterFunc(l.wait(), func() { l.later(func() { l.wake(gen) }) })
}

// wake is a timer wake-up, on the UI thread: it rescans the same cwds when
// the scan is old, then arms the next wake-up.
func (l *SpacesList) wake(gen uint64) {
	if gen != l.timerGen || !l.timerShown || l.closed {
		return
	}
	if len(l.scanned) > 0 && l.stale() {
		l.startScan(l.scanned)
	}
	l.armTimer()
}

// snapshot is the agent state, empty without a source.
func (l *SpacesList) snapshot() agentstate.Snapshot {
	if l.snap == nil {
		return agentstate.Snapshot{}
	}
	return l.snap()
}

// terms are the hc terminals: orchestrators, workers, then plain terminals.
func (l *SpacesList) terms() []spaces.Term {
	return spaceTerms(l.snapshot())
}

func spaceTerms(s agentstate.Snapshot) []spaces.Term {
	var out []spaces.Term
	for _, ws := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
		for _, w := range ws {
			if !w.Live && !w.Orchestrator() && !w.Plain() { // an ended worker holds no worktree (D112)
				continue
			}
			out = append(out, spaces.Term{Name: w.Name, Address: w.Address, Dir: w.Worktree, Status: workerIcon(w)})
		}
	}
	return out
}

func (l *SpacesList) stale() bool { return !l.hasScan || l.now().Sub(l.lastScan) >= RescanAfter }

func (l *SpacesList) forgetScan() {
	l.gen++
	l.scan, l.stats, l.scanned, l.hasScan = git.Scan{}, nil, nil, false
}

func (l *SpacesList) startScan(cwds []string) {
	l.gen++
	gen := l.gen
	l.lastScan, l.hasScan, l.scanned = l.now(), true, slices.Clone(cwds)
	l.scans++
	scanFn, statsFn := l.scanFn, l.statsFn
	l.start(func() {
		scan := scanFn(cwds)
		// Post the scan first: a slow git diff must not delay the rows.
		l.later(func() {
			if gen != l.gen {
				return
			}
			// Rebuild only: a slow scan must not start the next one. The old
			// stats stay until the new ones land.
			l.scan = scan
			l.rebuild()
		})
		if statsFn == nil {
			return
		}
		stats := statsFn(scan)
		l.later(func() {
			if gen != l.gen {
				return
			}
			l.stats = stats
			l.rebuild()
		})
	})
}

// runStats reads the diff stats of a scan with the git command.
func runStats(scan git.Scan) map[string]git.DiffStat {
	return git.Stats(context.Background(), git.ExecRunner{}, scan)
}

// later runs fn on the UI thread.
func (l *SpacesList) later(fn func()) {
	if l.post != nil {
		l.post(fn)
		return
	}
	fn()
}

func (l *SpacesList) rebuild() SpacesChange {
	snap := l.snapshot()
	groups := spaces.Build(spaceTerms(snap), l.scan)
	spaces.ApplyStats(groups, l.stats)
	groups = l.applyProjects(groups)
	online, line := true, ""
	if l.snap != nil && snap.CoreVersion == "" {
		online, line = false, snap.Problem
	}
	l.clearGoneSelection(groups)
	selected, _ := l.selectedWorktree()
	change := l.diff(groups, selected)
	change.Online = online != l.online || line != l.line
	l.groups, l.online, l.line, l.shown = groups, online, line, selected
	return change
}

func (l *SpacesList) diff(groups []spaces.RepoGroup, selected string) SpacesChange {
	var change SpacesChange
	if len(groups) != len(l.groups) {
		change.Rebuilt = true
		return change
	}
	for g := range groups {
		if len(groups[g].Rows) != len(l.groups[g].Rows) {
			change.Rebuilt = true
			return change
		}
	}
	marked := func(id, sel string) bool { return sel != "" && id == sel }
	for g := range groups {
		for r, row := range groups[g].Rows {
			old := l.groups[g].Rows[r]
			if !reflect.DeepEqual(old, row) || marked(old.ID(), l.shown) != marked(row.ID(), selected) {
				change.Rows = append(change.Rows, RowRef{g, r})
			}
		}
	}
	return change
}

// selectedWorktree is the id of the selected worktree, if a worktree is
// selected.
func (l *SpacesList) selectedWorktree() (string, bool) {
	if l.selection == nil {
		return "", false
	}
	if sel, ok := l.selection.Current(); ok && sel.Kind == state.KindWorktree {
		return sel.ID, true
	}
	return "", false
}

func (l *SpacesList) clearGoneSelection(groups []spaces.RepoGroup) {
	id, ok := l.selectedWorktree()
	if !ok {
		return
	}
	for _, g := range groups {
		for _, r := range g.Rows {
			if r.ID() == id {
				return
			}
		}
	}
	l.selection.Clear()
}

// Activate is a click on a row: it becomes the selection, and Review
// follows it.
func (l *SpacesList) Activate(row spaces.WorktreeRow) {
	if l.selection != nil {
		l.selection.Select(state.Selected{Kind: state.KindWorktree, ID: row.ID()})
	}
}

func (l *SpacesList) openPR(dir string, n uint32) {
	if l.OpenPR != nil {
		l.OpenPR(dir, int(n))
	}
}

// View builds the body: the `Spaces` label, the device header, and the groups.
func (l *SpacesList) View(c *ui.Context) {
	selected, _ := l.selectedWorktree()
	ui.Scroll(c).Fill().Children(func() {
		l.spacesHeader(c)
		l.deviceHeader(c)
		l.coreLine(c)
		for _, g := range l.groups {
			l.repoLine(c, g)
			for _, row := range g.Rows {
				l.row(c, row, row.ID() == selected)
			}
		}
	})
}

// spacesHeader is the `Spaces` label, the `projects` pill and `+ new`. `+ new`
// starts an orchestrator (D57); the pill opens the projects modal (D113).
// Without its func, a button has no action and no pointer.
func (l *SpacesList) spacesHeader(c *ui.Context) {
	ui.Row(c).Label(LabelSpacesHeader).FillWidth().Height(theme.SpacesRowHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.SidebarPadX).TextColor(theme.Color(theme.TextDim)).Children(func() {
		ui.Text(c, "Spaces")
		ui.Spacer(c)
		pill := ui.Row(c).Label(LabelSpacesProj).TextColor(theme.Color(theme.TextDim2)).MarginEnd(theme.SidebarPadX)
		if l.OnProjects != nil {
			pill = pill.Cursor(ui.CursorPointer).OnClick(l.OnProjects)
		}
		pill.Children(func() { ui.Text(c, "projects") })
		btn := ui.Row(c).Label(LabelSpacesNew).TextColor(theme.Color(theme.TextDim2))
		if l.OnNew != nil {
			btn = btn.Cursor(ui.CursorPointer).OnClick(l.OnNew)
		}
		btn.Children(func() { ui.Text(c, "+ new") })
	})
}

// deviceHeader is the group header `This computer`: a monitor icon and the
// name, 11 px, dim (D27, D113). Only the local Mac shows (D68). CPU and memory
// are in the footer.
func (l *SpacesList) deviceHeader(c *ui.Context) {
	ui.Row(c).Label(LabelDeviceHeader).FillWidth().Height(theme.SpacesRowHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.SpacesGap).PaddingX(theme.SidebarPadX).
		TextColor(theme.Color(theme.TextDim)).Children(func() {
		icons.Monitor(c, theme.DeviceIconSize).Shrink(0)
		ui.Text(c, "This computer").SingleLine().FontSize(theme.DeviceFontSize)
	})
}

// coreLine is the hc problem, under the device header, while contr does not
// follow the core.
func (l *SpacesList) coreLine(c *ui.Context) {
	if l.online || l.line == "" {
		return
	}
	ui.Text(c, l.line).TextColor(theme.Color(theme.TextDim)).
		PaddingX(theme.SidebarPadX).PaddingY(theme.SpacesGap)
}

// nameColour is a worktree's name colour: white when a terminal holds it,
// dim when it is only a checkout.
func nameColour(held bool) uint32 {
	if held {
		return theme.Text
	}
	return theme.TextDim
}

// repoLine is a repo's name, then its branch when it has one.
func (l *SpacesList) repoLine(c *ui.Context, g spaces.RepoGroup) {
	ui.Row(c).Label(RepoLabel(g)).FillWidth().Height(theme.SpacesRowHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.SpacesGap).PaddingX(theme.SidebarPadX).Children(func() {
		ui.Text(c, g.Name).TextColor(theme.Color(theme.Text)).SingleLine().Ellipsis("…").MinWidth(0)
		if g.Branch != "" {
			ui.Text(c, g.Branch).TextColor(theme.Color(theme.TextDim)).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
}

// row builds one worktree row, two lines: the name with the status icon, and
// the hold line. The selected row shows the card.
func (l *SpacesList) row(c *ui.Context, row spaces.WorktreeRow, selected bool) {
	l.builds[row.Root]++
	held := len(row.Holders) > 0
	// The border is always there, so the card does not move the content.
	border := uint32(theme.BgBars)
	if selected {
		border = theme.CardBorder
	}
	card := ui.ButtonBase(c).Label(WorktreeRowLabel(row, selected)).Height(theme.WorktreeRowHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.SpacesGap).
		PaddingStart(theme.SidebarPadX+theme.RowIndent-theme.CardInsetX-1).PaddingEnd(theme.SidebarPadX-theme.CardInsetX-1).
		MarginX(theme.CardInsetX).Radius(theme.CardRadius).Border(1, theme.Color(border)).
		Cursor(ui.CursorPointer).Cache().OnClick(func() { l.Activate(row) })
	if selected {
		card = card.Background(theme.Color(theme.BgCard))
	}
	card.Children(func() {
		icons.Sized(c, row.Status, theme.IconSizeSmall).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, row.Name).Height(theme.LineHeight).TextColor(theme.Color(nameColour(held))).SingleLine().Ellipsis("…").MinWidth(0)
			l.holdLine(c, row)
		})
		if row.Diff != nil && (row.Diff.Added > 0 || row.Diff.Removed > 0) {
			kit.Counts(c, row.Diff.Added, row.Diff.Removed)
		}
		if row.Badge != nil {
			badgeChip(c, *row.Badge, func(n uint32) { l.openPR(row.Root, n) })
		}
	})
}

// holdLine is the second line: the top holder's name and state, or the
// board's who. A you line is drawn as the you chip. A row without a holder
// keeps the line, empty.
func (l *SpacesList) holdLine(c *ui.Context, row spaces.WorktreeRow) {
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).TextColor(theme.Color(theme.TextDim2)).Children(func() {
		switch {
		case row.Hold == nil:
		case row.HoldYou:
			youChip(c, LabelHoldYou, *row.Hold)
		default:
			ui.Text(c, *row.Hold).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
}

// youChip is text in the you style: the owner's turn.
func youChip(c *ui.Context, label, text string) {
	ui.Row(c).Label(label).PaddingX(theme.BadgePadX).Radius(theme.BadgeRadius).Shrink(0).
		Background(theme.Color(theme.YouBg)).Children(func() {
		ui.Text(c, text).Bold().TextColor(theme.Color(theme.YouText)).NoWrap()
	})
}

// badgeChip is the row's mark: `#640` for a pull request, or a white chip
// for a value that waits on the owner.
// A click on a pull request chip runs open with its number.
func badgeChip(c *ui.Context, b spaces.Badge, open func(uint32)) {
	if b.Kind == spaces.BadgeYou {
		youChip(c, LabelBadge, BadgeText(b))
		return
	}
	ui.ButtonBase(c).Label(LabelBadge).PaddingX(theme.BadgePadX).Shrink(0).Cursor(ui.CursorPointer).
		OnClick(func() { open(b.PR) }).Children(func() {
		ui.Text(c, BadgeText(b)).TextColor(theme.Color(theme.TextDim)).NoWrap()
	})
}
