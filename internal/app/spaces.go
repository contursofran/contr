package app

// The spaces column body: the `Spaces` label, the `Local` host card, then one
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
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/herdr"
)

// RescanAfter is the age at which a scan is old, and the time between rescans
// while the window is shown, so that a changed branch appears (D15). An old
// scan is also redone on the next herdr change and when the window is shown.
const RescanAfter = 10 * time.Second

// Element labels of the spaces column.
const (
	LabelSpacesHeader = "spaces header"
	LabelSpacesNew    = "spaces new"
	LabelHostLocal    = "host-local"
	LabelCardCPU      = "host-cpu"
	LabelCardMem      = "host-mem"
	LabelCardMemFill  = "host-mem-fill"
	LabelHostOnline   = "host-online"
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

// CPUText is the CPU text of the host card: "CPU 38%", or "CPU –" without a
// reading.
func CPUText(s hostSource) string {
	if s != nil {
		if stats, ok := s.Stats(); ok {
			return "CPU " + stats.CPULabel()
		}
	}
	return "CPU " + noReading
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
	// Online: the herdr connection came or went (the host card's dot).
	Online bool
}

// Any tells whether the column needs a redraw.
func (c SpacesChange) Any() bool { return c.Rebuilt || c.Online || len(c.Rows) > 0 }

// SpacesList is the spaces column body. Methods run on the UI thread.
type SpacesList struct {
	src       liveSource
	board     *Board // nil in views built without one
	host      hostSource
	selection *state.Selection
	status    *state.StatusMessage
	focus     FocusFn

	// visible tells whether the window is shown. Nil counts as shown.
	visible func() bool
	// scanFn reads git. It runs on a goroutine, started by start.
	scanFn func([]string) git.Scan
	start  func(func())
	// post runs fn on the UI thread (Window.Update). Nil runs it at once.
	post func(func())
	now  func() time.Time
	// afterFunc arms the rescan timer. Tests replace it with a fake clock.
	afterFunc func(d time.Duration, f func()) stopper

	groups []spaces.RepoGroup
	online bool
	// shown is the id of the selected worktree at the last rebuild.
	shown string

	scan     git.Scan
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

// NewSpacesList returns the list. src gives the tree, b (may be nil) the
// rows' second line and badge, h (may be nil) the host card's reading,
// selection the card. status (may be nil) shows focus errors. visible (may be
// nil) tells whether the window is shown; post (may be nil) runs a func on
// the UI thread, as Window.Update does. A click focuses the pane with
// [FocusInBackground]. The list is empty until [SpacesList.Sync], which the
// owner calls at the start and when the tree, the board or the connection
// changed.
func NewSpacesList(src liveSource, b *Board, h hostSource, selection *state.Selection, status *state.StatusMessage, visible func() bool, post func(func())) *SpacesList {
	l := &SpacesList{
		src: src, board: b, host: h, selection: selection, status: status, focus: FocusInBackground,
		visible: visible, post: post, scanFn: git.ScanCwds, start: func(f func()) { go f() },
		now: time.Now, builds: map[string]int{},
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
	}
	return l
}

// Groups returns the repo groups shown. Empty without a tree.
func (l *SpacesList) Groups() []spaces.RepoGroup { return l.groups }

func (l *SpacesList) shownNow() bool { return l.visible == nil || l.visible() }

// Sync brings the column up to the tree, the board and the connection. It
// starts a scan when a pane sits in another directory, or when the last scan
// is old and the window is shown (D15). With no pane cwd there is nothing to
// scan, and the scan is dropped. It then rebuilds the groups from the scan
// that it has. A selected worktree that no row has any more leaves the
// selection; an agent selection stays.
func (l *SpacesList) Sync() SpacesChange {
	tree := l.tree()
	var cwds []string
	if tree != nil {
		cwds = spaces.PaneCwds(*tree)
	}
	switch {
	case len(cwds) == 0:
		l.forgetScan()
	case !slices.Equal(cwds, l.scanned) || (l.stale() && l.shownNow()):
		l.startScan(cwds)
	}
	return l.rebuild(tree)
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

func (l *SpacesList) tree() *herdr.HerdrTree {
	if l.src == nil {
		return nil
	}
	return l.src.Live().Tree
}

func (l *SpacesList) stale() bool { return !l.hasScan || l.now().Sub(l.lastScan) >= RescanAfter }

func (l *SpacesList) forgetScan() {
	l.gen++
	l.scan, l.scanned, l.hasScan = git.Scan{}, nil, false
}

func (l *SpacesList) startScan(cwds []string) {
	l.gen++
	gen := l.gen
	l.lastScan, l.hasScan, l.scanned = l.now(), true, slices.Clone(cwds)
	l.scans++
	scanFn := l.scanFn
	l.start(func() {
		scan := scanFn(cwds)
		l.later(func() {
			if gen != l.gen {
				return
			}
			// Rebuild only: a slow scan must not start the next one.
			l.scan = scan
			l.rebuild(l.tree())
		})
	})
}

// later runs fn on the UI thread.
func (l *SpacesList) later(fn func()) {
	if l.post != nil {
		l.post(fn)
		return
	}
	fn()
}

func (l *SpacesList) marks() map[string]board.PaneMark {
	if l.board == nil {
		return nil
	}
	snap := l.board.View().Snapshot
	if snap == nil {
		return nil
	}
	return board.PaneMarks(snap)
}

func (l *SpacesList) rebuild(tree *herdr.HerdrTree) SpacesChange {
	var groups []spaces.RepoGroup
	if tree != nil {
		groups = spaces.Build(*tree, l.scan)
		spaces.ApplyMarks(groups, l.marks())
	}
	online := true
	if l.src != nil {
		_, down := l.src.Live().State.Line()
		online = !down
	}
	l.clearGoneSelection(groups)
	selected, _ := l.selectedWorktree()
	change := l.diff(groups, selected)
	change.Online = online != l.online
	l.groups, l.online, l.shown = groups, online, selected
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

// Activate is a click on a row: it becomes the selection, and herdr focuses
// its top holder's pane. A row without a holder is selected and nothing is
// focused. A second click focuses again.
func (l *SpacesList) Activate(row spaces.WorktreeRow) {
	if l.selection != nil {
		l.selection.Select(state.Selected{Kind: state.KindWorktree, ID: row.ID()})
	}
	if row.FocusPane != nil && l.focus != nil {
		var report func(string)
		if l.status != nil {
			report = l.status.Show
		}
		l.focus(*row.FocusPane, report)
	}
}

// View builds the body: the `Spaces` label, the host card, and the groups.
func (l *SpacesList) View(c *ui.Context) {
	selected, _ := l.selectedWorktree()
	ui.Scroll(c).Fill().Children(func() {
		l.spacesHeader(c)
		l.hostCard(c)
		for _, g := range l.groups {
			l.repoLine(c, g)
			for _, row := range g.Rows {
				l.row(c, row, row.ID() == selected)
			}
		}
	})
}

// spacesHeader is the `Spaces` label and `+ new`. The button has no action
// yet (Rust D50), so it has no pointer.
func (l *SpacesList) spacesHeader(c *ui.Context) {
	ui.Row(c).Label(LabelSpacesHeader).FillWidth().Height(theme.SpacesRowHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.SidebarPadX).TextColor(theme.Color(theme.TextDim)).Children(func() {
		ui.Text(c, "Spaces")
		ui.Spacer(c)
		ui.Row(c).Label(LabelSpacesNew).TextColor(theme.Color(theme.TextDim2)).Children(func() { ui.Text(c, "+ new") })
	})
}

// hostCard is `Local`, then the CPU reading, the memory bar and a dot that is
// lit while herdr is connected. Without a reading the CPU shows a dash and
// the bar stays empty.
func (l *SpacesList) hostCard(c *ui.Context) {
	var filled int
	if l.host != nil {
		if stats, ok := l.host.Stats(); ok {
			filled = stats.MemBarPixels()
		}
	}
	ui.Row(c).Label(LabelHostLocal).Height(theme.SpacesRowHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.SidebarPadX-theme.CardInsetX).MarginX(theme.CardInsetX).
		Radius(theme.CardRadius).Background(theme.Color(theme.BgCard)).Border(1, theme.Color(theme.CardBorder)).Children(func() {
		ui.Text(c, "Local").TextColor(theme.Color(theme.Text))
		ui.Spacer(c)
		ui.Row(c).AlignItems(ui.Center).Gap(theme.SpacesGap).Children(func() {
			ui.Row(c).Label(LabelCardCPU).TextColor(theme.Color(theme.TextDim)).Children(func() { ui.Text(c, CPUText(l.host)) })
			ui.Box(c).Label(LabelCardMem).Size(theme.MemBarWidth, theme.MemBarHeight).Shrink(0).
				Radius(theme.UsageBarRadius).Background(theme.Color(theme.Line)).Children(func() {
				ui.Box(c).Label(LabelCardMemFill).Size(float32(filled), theme.MemBarHeight).
					Radius(theme.UsageBarRadius).Background(theme.Color(theme.TextDim))
			})
			ui.Box(c).Label(LabelHostOnline).Size(theme.HostDotSize, theme.HostDotSize).Shrink(0).
				Radius(theme.HostDotSize / 2).Background(theme.Color(onlineColour(l.online)))
		})
	})
}

// onlineColour is the dot's colour: Text2 while herdr is connected, the
// faint icon grey otherwise.
func onlineColour(online bool) uint32 {
	if online {
		return theme.Text2
	}
	return theme.IconFaint
}

// nameColour is a worktree's name colour: white when a tab holds it, dim
// when it is only a checkout.
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
		ui.Text(c, g.Name).TextColor(theme.Color(theme.Text)).NoWrap().Ellipsis("…").MinWidth(0)
		if g.Branch != "" {
			ui.Text(c, g.Branch).TextColor(theme.Color(theme.TextDim)).NoWrap().Ellipsis("…").MinWidth(0)
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
			ui.Text(c, row.Name).Height(theme.LineHeight).TextColor(theme.Color(nameColour(held))).NoWrap().Ellipsis("…").MinWidth(0)
			l.holdLine(c, row)
		})
		if row.Badge != nil {
			badgeChip(c, *row.Badge)
		}
	})
}

// holdLine is the second line: the top holder's tab and state, or the
// board's who. A you line is drawn as the you chip. A row without a holder
// keeps the line, empty.
func (l *SpacesList) holdLine(c *ui.Context, row spaces.WorktreeRow) {
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).TextColor(theme.Color(theme.TextDim2)).Children(func() {
		switch {
		case row.Hold == nil:
		case row.HoldYou:
			youChip(c, LabelHoldYou, *row.Hold)
		default:
			ui.Text(c, *row.Hold).NoWrap().Ellipsis("…").MinWidth(0)
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
func badgeChip(c *ui.Context, b spaces.Badge) {
	if b.Kind == spaces.BadgeYou {
		youChip(c, LabelBadge, BadgeText(b))
		return
	}
	ui.Row(c).Label(LabelBadge).PaddingX(theme.BadgePadX).Shrink(0).Children(func() {
		ui.Text(c, BadgeText(b)).TextColor(theme.Color(theme.TextDim)).NoWrap()
	})
}
