// Package app holds the content of contr's window: the three-column frame
// and the views in its slots. docs/PORT-PLAN.md lists the views that come
// next.
package app

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/github"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/prefs"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
	"github.com/contursofran/contr/internal/widths"
)

// Slot is a view that fills one place of the frame. Later milestones replace
// the placeholders of [PlaceholderSlots] with the real views.
type Slot func(c *ui.Context)

// Slots are the views of the frame. A nil slot is left empty.
type Slots struct {
	Spaces   Slot // sidebar, under its header
	Terminal Slot // the selected hc terminal
	Panel    Slot // board panel, shown while it is open
}

// PlaceholderSlots returns slots that show only their own name in dim text.
func PlaceholderSlots() Slots {
	return Slots{
		Spaces:   placeholder("spaces"),
		Terminal: placeholder("terminal"),
		Panel:    placeholder("panel"),
	}
}

func placeholder(name string) Slot {
	return func(c *ui.Context) {
		ui.Text(c, "["+name+"]").TextColor(theme.Color(theme.TextDim2))
	}
}

// Frame is the window's view. Build it with [NewFrame] and give [Frame.View]
// to the window. Its methods run on the UI thread.
type Frame struct {
	Slots Slots
	// Overlay is the layer above the terminal for the notch and the outline.
	Overlay TerminalOverlay
	// Outline is the message ticks of the orchestrator terminal. [Frame.Attach]
	// makes it when there is an agent source. Nil otherwise.
	Outline *Outline
	// Visibility, Selection and Toasts are the app state. The frame holds
	// them for the views in its slots.
	Visibility *state.Visibility
	Selection  *state.Selection
	Toasts     *state.Toasts
	// Projects is the projects store, loaded after Attach (D109). Nil until then.
	Projects *projects.Store
	// Card is the questions card. The notch and the board
	// panel open it; the notch builds it ([Frame.Attach] makes it).
	Card *QuestionsCard
	// Notch is the questions notch in the terminal overlay ([Frame.Attach] makes it).
	Notch *Notch
	// ProjectsModal is the projects modal (D110), made in Attach.
	ProjectsModal *ProjectsModal
	// Updates is the update service of the Settings modal (D115). Nil shows "updates are off".
	Updates Updates
	// Relaunch restarts contr after an update installs.
	Relaunch func()
	// Settings is the Settings modal, made in Attach.
	Settings *Settings
	// Worker is the panel's worker view. [Frame.Attach] makes it. The panel
	// shows it while a worker is selected.
	Worker *WorkerPanel
	// StartAgents follows the agents. [Frame.Attach] calls it once, with a
	// notify function that is safe from any goroutine, and uses the source
	// and the stop function it returns. Nil means no agents.
	StartAgents func(notify func()) (src AgentSource, stop func())
	// PickFolder and SpawnOrchestrator serve `+ new` (D57): the first asks the
	// owner for a folder (ErrPickCancelled for none), the second starts the
	// orchestrator profile there. Both block, so the frame runs them off the
	// UI thread. Nil makes `+ new` do nothing.
	PickFolder func() (string, error)
	// OpenFolder asks the owner for a folder to add to the projects (D116). It
	// blocks, so the modal calls it off the UI thread.
	OpenFolder func() (string, error)
	// CloneParent, PickCloneParent and CloneRun serve the clone part of the
	// projects modal (D117); see ProjectsModal. An empty CloneParent hides it.
	CloneParent     string
	PickCloneParent func(def string) (string, error)
	CloneRun        git.CloneRunner
	// ProjectKey returns the project key of a folder (D116); nil uses the folder.
	ProjectKey func(path string) string
	// LoadProjects reads the projects store (D109). It runs on a goroutine.
	// Nil means no store: the frame reads no file.
	// Nil reads the file in the user config folder.
	LoadProjects func() (*projects.Store, error)
	// SaveProjects writes a copy of the store (D110). It runs on a goroutine.
	// Nil saves nothing.
	SaveProjects func(*projects.Store) error
	// ModelOf names the model that an agent profile starts, for line 2 of the
	// agent rows. Nil shows no model.
	ModelOf func(profile string) string
	// WatchHead watches a repo's HEAD file for the sidebar's branch (see
	// SpacesList.WatchHead). Nil: the branch is read when the terminals change.
	WatchHead func(ctx context.Context, path string, changed func()) error
	// LoadWidths reads the widths of the sidebar and the panel (D111). It
	// runs on a goroutine. Nil means the defaults and no file.
	LoadWidths func() (widths.Widths, error)
	// SaveWidths writes the widths after a drag. It runs on a goroutine.
	// Nil saves nothing.
	SaveWidths func(widths.Widths) error
	// LoadPrefs reads the settings (D119). [Frame.StartTheme] calls it before the
	// window shows, so it must be quick. Nil means the defaults and no file.
	LoadPrefs func() (prefs.Prefs, error)
	// SavePrefs writes the settings after a change. It runs on a goroutine. Nil
	// saves nothing.
	SavePrefs func(prefs.Prefs) error
	// ThemePlatform is MyGo's theme (mygo.Theme). Nil leaves the palette as it is.
	ThemePlatform     theme.Platform
	SpawnOrchestrator func(dir string) error
	// OpenTerminal serves "+" and Cmd-T (D82): it starts a shell in an hc
	// terminal for the open project, in dir, and returns its address. It
	// blocks, so the frame runs it off the UI thread. Nil makes both do
	// nothing.
	OpenTerminal func(project, dir string) (string, error)
	// TerminalSource gives the source of the hc terminal with an address.
	// The terminal view attaches to the selected agent's terminal through it.
	// Nil shows no terminal.
	TerminalSource func(address string) ghostty.Source
	// Panel is the board panel. [Frame.Attach] makes it and puts its view in
	// Slots.Panel. The board is disabled (D122): the files panel does not show it.
	Panel *BoardPanel
	// Files is the tree of the files panel. [NewFrame] makes it.
	Files *FileTree
	// Diff is the title bar's diff summary. It shows the totals of Files. [NewFrame] makes it.
	Diff *DiffSummary
	// Review is the Review view. [Frame.Attach] makes it. While the Review toggle is on, it
	// replaces the terminal area, and it follows the selected project.
	Review *Review
	// SpacesList is the spaces column body. [Frame.Attach] makes it and puts
	// its view in Slots.Spaces.
	SpacesList *SpacesList
	// PR is the pull request pane. [Frame.Attach] makes it. [Frame.OpenPR] opens it at the
	// right of the terminal area, and the title bar tabs shrink to icon and id (D59).
	PR *PRPane
	// WorkerPRs finds the pull request of each worker's branch (D104). [Frame.Attach] makes it.
	WorkerPRs *WorkerPRs
	// OpenIssueWeb opens issue n of the repository in dir in the browser. [Frame.Attach] sets it;
	// tests replace it.
	OpenIssueWeb func(dir string, n int) error
	// OpenPRWeb opens pull request n of the repository in dir in the browser. [Frame.Attach] sets
	// it; tests replace it.
	OpenPRWeb func(dir string, n int) error
	// Host feeds the This Mac row and Usage the plan meters of the files panel. [Frame.Attach] creates them when they are nil (tests set
	// fakes first) and ends them with its stop function.
	Host  *host.Host
	Usage *usage.Sampler

	// NewOrch is the `+ new` action. [Frame.Attach] makes it when PickFolder
	// and SpawnOrchestrator are set.
	NewOrch *NewOrchestrator
	// NewTerm is the "+" and Cmd-T action. [Frame.Attach] makes it when
	// OpenTerminal is set.
	NewTerm *NewTerminal
	// CloseTerm is the Cmd-W action. [Frame.Attach] makes it when there are
	// agents.
	CloseTerm *CloseTerminal

	bar titleBar
	// agentSnap gives the agents; nil before Attach or without agents. openProject is the
	// project the title bar shows, and tabAgent the address of the agent whose tabs show.
	agentSnap   func() agentstate.Snapshot
	openProject string
	tabAgent    string
	// tabOrder is the order the owner dragged the plain tabs to, as addresses, for the session.
	tabOrder    []string
	sidebarOpen bool
	panelOpen   bool
	reviewOpen  bool
	// sidebarW and panelW are the dragged widths (D111). A hidden side keeps
	// its width. movedNow tells that a divider moved in this frame; saveTimer
	// waits for the dividers to rest before the save.
	sidebarW, panelW float32
	// icons tells that this frame draws the sidebar as an icon column (D128).
	icons                   bool
	movedNow, widthsTouched bool
	// The Appearance setting (D119); see appearance.go.
	appearance   mygo.ThemeSource
	appearanceMu sync.Mutex
	saveMu       sync.Mutex
	follower     *theme.Follower
	lateWin      lateWindow
	themeErr     error
	saveTimer    *time.Timer
	// term is the plugin's terminal of the selected agent and conn its hc
	// connection; both are nil with no terminal. termAddr is the hc terminal
	// they show; termSet is false before the first sync. termState is the
	// connection state; termErr says why the terminal could not start.
	term      *gterm.Terminal
	conn      *ghostty.Conn
	termAddr  string
	termSet   bool
	termState ghostty.State
	termErr   string
	// find is the find bar over the terminal (termfind.go).
	find termFind
	// post and invalidate are the window's; nil before attachTerminal.
	post       func(fn func())
	invalidate func()
	winFocused bool
	// focusTerm asks the next build to give the terminal the focus (Enter in
	// the sidebar list).
	focusTerm bool
	// keyNav is true while the sidebar keys change the selection; autoFocus
	// asks the next terminal view to take the focus. A terminal that a key
	// chose leaves the focus in the list, so the next key still moves it.
	keyNav, autoFocus bool
	// reviewDir is the repo that Review shows now. Empty: none.
	reviewDir string
	reviewSet bool
}

// NewFrame returns a frame with the sidebar and the panel open and
// placeholder slots.
func NewFrame() *Frame {
	f := &Frame{Slots: PlaceholderSlots(), sidebarOpen: true, panelOpen: true,
		sidebarW: theme.SidebarWidth, panelW: theme.PanelWidth}
	f.Files = NewFileTree(f.filesDir)
	f.Diff = &DiffSummary{Tree: f.Files}
	f.Files.Open = f.openFileInReview
	f.Files.Reveal = func(abs string) { mygo.Shell.ShowItemInFolder(abs) }
	return f
}

// SidebarOpen tells whether the sidebar shows.
func (f *Frame) SidebarOpen() bool { return f.sidebarOpen }

// PanelOpen tells whether the board panel shows.
func (f *Frame) PanelOpen() bool { return f.panelOpen }

// ToggleSidebar shows or hides the sidebar.
func (f *Frame) ToggleSidebar() { f.sidebarOpen = !f.sidebarOpen }

// TogglePanel shows or hides the board panel.
func (f *Frame) TogglePanel() { f.panelOpen = !f.panelOpen }

// OpenPR shows pull request n of the repository in dir.
// It does nothing while the viewers are hidden (D147, [viewersEnabled]).
func (f *Frame) OpenPR(dir string, n int) {
	if viewersEnabled && f.PR != nil {
		f.PR.Open(dir, n)
		if f.agentSnap != nil {
			f.PR.Item = itemOf(f.agentSnap().Workers, dir)
		}
	}
}

// paneOpener is [Frame.OpenPR] while the viewers are shown, else nil: a button or row that
// would open the PR pane is then not built (D147).
func (f *Frame) paneOpener() func(dir string, n int) {
	if !viewersEnabled {
		return nil
	}
	return f.OpenPR
}

// prOrchestrator gives the address of the orchestrator terminal of the project of a PR's
// folder (D105): the project of the worker with that worktree, else the open project. "" when
// the project has no orchestrator.
func (f *Frame) prOrchestrator(dir string) string {
	if f.agentSnap == nil {
		return ""
	}
	snap := f.agentSnap()
	project := f.openProject
	for _, w := range snap.Workers {
		if w.Worktree != "" && w.Worktree == dir {
			project = w.Project
			break
		}
	}
	for _, o := range snap.Orchestrators {
		if o.Project == project {
			return o.Address
		}
	}
	return ""
}

// OpenIssue opens issue n of the repository in dir in the browser, off the UI thread (D74). A
// failure shows in the status message.
func (f *Frame) OpenIssue(dir string, n int) {
	open := f.OpenIssueWeb
	if open == nil {
		open = func(dir string, n int) error { return github.New(dir).OpenIssue(context.Background(), n) }
	}
	f.openWeb(open, dir, n)
}

// OpenPRInBrowser opens pull request n of the repository in dir in the browser, off the UI
// thread. A failure shows in the status message. It does not open the PR pane (D127).
func (f *Frame) OpenPRInBrowser(dir string, n int) {
	open := f.OpenPRWeb
	if open == nil {
		open = func(dir string, n int) error { return github.New(dir).OpenPR(context.Background(), n) }
	}
	f.openWeb(open, dir, n)
}

// agentRowEntry is the entry of the agent rows. Its Open PR opens the browser; it has no
// OpenPR, so the rows cannot reach the PR pane (D127).
func (f *Frame) agentRowEntry() WorkerEntry {
	return WorkerEntry{PRs: f.WorkerPRs, OpenIssue: f.OpenIssue, OpenPRWeb: f.OpenPRInBrowser}
}

// openWeb runs open off the UI thread and shows its error in the status message.
func (f *Frame) openWeb(open func(dir string, n int) error, dir string, n int) {
	run := Runner{}
	if f.WorkerPRs != nil {
		run = f.WorkerPRs.Run
	}
	run.Do(func() func() {
		err := open(dir, n)
		return func() {
			if err != nil && f.Toasts != nil {
				f.Toasts.Show(err.Error())
			}
		}
	})
}

// ClosePR hides the pull request pane.
func (f *Frame) ClosePR() {
	if f.PR != nil {
		f.PR.Close()
	}
}

// PROpen tells whether the pull request pane shows.
func (f *Frame) PROpen() bool { return f.PR != nil && f.PR.IsOpen() }

// filesDir is the folder of the file tree: the worktree of the selected agent
// or worktree row, else the folder of the open project.
func (f *Frame) filesDir() string {
	if dir := f.reviewTarget(); dir != "" {
		return dir
	}
	if f.SpacesList == nil || f.openProject == "" {
		return ""
	}
	for _, g := range f.SpacesList.Groups() {
		if g.Name != f.openProject {
			continue
		}
		if g.Path != "" {
			return g.Path
		}
		for _, r := range g.Rows {
			if r.IsMain {
				return r.Root
			}
		}
		if len(g.Rows) > 0 {
			return g.Rows[0].Root
		}
	}
	return ""
}

// openFileInReview shows the Review view on the repository in dir, at the file rel.
func (f *Frame) openFileInReview(dir, rel string) {
	if f.Review == nil {
		return
	}
	f.reviewOpen = true
	f.reviewDir, f.reviewSet = dir, true
	f.Review.OpenFile(dir, rel)
}

// ReviewOpen tells whether the Review view replaces the terminal area.
func (f *Frame) ReviewOpen() bool { return f.reviewOpen }

// ToggleReview shows or hides the Review view. Showing it reads the selected project's repo.
func (f *Frame) ToggleReview() {
	f.reviewOpen = !f.reviewOpen
	f.syncReview()
}

// reviewTarget is the folder that Review should show: the selected worktree's root, the
// selected hc terminal's worktree, or "" with no selection.
func (f *Frame) reviewTarget() string {
	if f.Selection == nil {
		return ""
	}
	sel, ok := f.Selection.Current()
	if !ok {
		return ""
	}
	switch sel.Kind {
	case state.KindWorktree:
		return sel.ID
	case state.KindOrchestrator, state.KindWorker, state.KindTerminal:
		if f.agentSnap == nil {
			return ""
		}
		s := f.agentSnap()
		for _, ws := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
			for _, w := range ws {
				if w.Address == sel.ID {
					return w.Worktree
				}
			}
		}
		return ""
	}
	return ""
}

// syncReview points Review at the selected project. It does nothing while the view is hidden or
// the window is hidden, so no git runs then; it runs again when either shows. UI thread.
func (f *Frame) syncReview() {
	if f.Review == nil || !f.reviewOpen {
		return
	}
	if f.Visibility != nil && !f.Visibility.Visible() {
		return
	}
	dir := f.reviewTarget()
	if dir == f.reviewDir && f.reviewSet {
		return
	}
	f.reviewDir, f.reviewSet = dir, true
	f.Review.SetRepo(dir)
}

// Element labels. Tests and the e2e driver find the frame's parts by them.
const (
	LabelSidebar       = "sidebar"
	LabelSidebarToggle = "Toggle sidebar"
	LabelSidebarReopen = "Show sidebar"
	LabelTitleBar      = "title bar"
	LabelTerminal      = "terminal area"
	LabelTerminalGrid  = "terminal grid"
	LabelReview        = "review area"
	LabelPanel         = "board panel"
	// LabelFilesToggle is the title bar button that hides and shows the files panel.
	LabelFilesToggle = "Hide files"
	// The three panels of the window (docs/spec/Window.md): screen readers name them so.
	LabelPanelSidebar  = "Sidebar"
	LabelPanelTerminal = "Terminal"
	LabelPanelFiles    = "Files"
)

func color(rgb uint32) ui.Color { return theme.Color(rgb) }

// View builds the frame: one glass sheet with the title bar on top, the sidebar,
// terminal and files panels in a row under it (docs/spec/Window.md). The window
// has no footer (D146); messages are toasts.
func (f *Frame) View(c *ui.Context) {
	theme.Apply(c)
	p := theme.Current()
	// ⌘J opens or closes the questions card from the notch (D125, D147).
	if f.Notch != nil && c.Shortcut(ui.Cmd, ui.KeyJ) {
		f.Notch.Toggle()
	}
	win, _ := c.Size()
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyE) && !narrowWindow(win) {
		f.TogglePanel()
	}
	// The system draws the blur behind the view where the view paints nothing.
	// Without it the sheet stands on the ground.
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(p.Ground)
	}
	ui.Column(c).Fill().Background(p.GlassWindow).TextColor(p.Ink85).Children(func() {
		f.sidebarW = clampWidth(f.sidebarW, theme.SidebarMinWidth, theme.SidebarMaxWidth)
		f.panelW = clampWidth(f.panelW, theme.PanelMinWidth, theme.PanelMaxWidth)
		sidebar, files, showFiles := fitWidths(win, f.sidebarW, f.panelW, f.sidebarOpen, f.panelOpen)
		f.icons = f.sidebarOpen && compactSidebar(win)
		if f.SpacesList != nil {
			f.SpacesList.compact = f.icons
		}
		f.Files.Sync() // the diff summary counts what the tree lists, also while the panel is hidden
		f.titleBar(c, sidebar, narrowWindow(win))
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(0, theme.PanelGap, theme.PanelGap).Children(func() {
			f.panels(c, sidebar, files, showFiles)
		})
		f.saveWidthsAfterDrag()
		if f.ProjectsModal != nil {
			f.ProjectsModal.View(c)
		}
		if f.Settings != nil {
			f.Settings.View(c)
		}
		f.toastView(c)
	})
}

// panels draws the three panels: the sidebar, the terminal and the files, with
// a draggable divider between neighbors (D111). sidebar and files are the
// drawn widths.
func (f *Frame) panels(c *ui.Context, sidebar, files float32, showFiles bool) {
	terminal := func(c *ui.Context) {
		if !showFiles {
			f.terminalPanel(c)
			return
		}
		f.split(c, &f.panelW, files, theme.PanelMinWidth, theme.PanelMaxWidth, true, false, f.filesPanel, f.terminalPanel)
	}
	if !f.sidebarOpen {
		terminal(c)
		return
	}
	f.split(c, &f.sidebarW, sidebar, theme.SidebarMinWidth, theme.SidebarMaxWidth, false, f.icons, f.sidebarPanel, terminal)
}

func (f *Frame) sidebarPanel(c *ui.Context) {
	kit.Panel(c, kit.PanelOpts{Name: LabelPanelSidebar, Bare: true, Body: func() { f.sidebar(c) }})
}

func (f *Frame) terminalPanel(c *ui.Context) {
	kit.Panel(c, kit.PanelOpts{Name: LabelPanelTerminal, Bare: true, Body: func() { f.content(c) }})
}

// filesPanel is the files slot: the file tree, with the usage meters in its footer. The v0.2 board and worker panel
// stay in the code but are not shown (D122); see panelBox.
func (f *Frame) filesPanel(c *ui.Context) {
	kit.Panel(c, kit.PanelOpts{Name: LabelPanelFiles,
		Head:   func() { f.Files.Head(c) },
		Body:   func() { f.Files.View(c) },
		Footer: func() { kit.UsageMeter(c, usageMeters(f.planUsage(), f.selectedTerminal())) }})
}

// selectedTerminal is the hc terminal of the selected orchestrator, worker or
// plain terminal, or "" for any other selection.
func (f *Frame) selectedTerminal() string {
	if f.Selection == nil {
		return ""
	}
	sel, ok := f.Selection.Current()
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		return sel.ID
	}
	return ""
}

// planUsage is the usage sampler as the meters read it. A nil sampler (a
// frame without one) reads as an interface that holds nothing.
func (f *Frame) planUsage() planPercent {
	if f.Usage == nil {
		return nil
	}
	return f.Usage
}

func (f *Frame) run(c *ui.Context, s Slot) {
	if s != nil {
		s(c)
	}
}

func (f *Frame) sidebar(c *ui.Context) {
	ui.Column(c).Label(LabelSidebar).FillWidth().FillHeight().Children(func() {
		ui.Box(c).Grow(1).MinHeight(0).Children(func() { f.run(c, f.Slots.Spaces) })
	})
}

// clampWidth keeps w between lo and hi.
func clampWidth(w, lo, hi float32) float32 { return min(max(w, lo), hi) }

// split draws first and second side by side with a draggable divider (D111).
// *stored is the width the owner set for first's panel, between lo and hi;
// shown is the width to draw, which may be less in a narrow window. With rtl,
// first is at the right (the files panel). The panels are PanelGap apart: each
// pane holds its panel and its half of the gap, so the pane of first is
// wider than its panel by the half on its side. The divider is a line with no
// color; it shows the grey of a hover or a drag only. The split builds under
// a copy of the theme with those colors, and the panes build under the
// window's theme again, so only the divider takes the copy. With fixed, the
// panel has the width of the window rule (the sidebar as icons, D128): a move
// of the divider does not change *stored.
func (f *Frame) split(c *ui.Context, stored *float32, shown, lo, hi float32, rtl, fixed bool, first, second func(*ui.Context)) {
	p := theme.Current()
	base := c.Theme()
	line := *base
	line.Border = ui.Transparent
	line.Accent = p.Ink22
	// Both halves and the 1px divider make PanelGap: 5 + 1 + 4.
	near, far := float32(theme.PanelGap/2), float32(theme.PanelGap/2-1)
	firstPad, secondPad := [4]float32{0, near, 0, 0}, [4]float32{0, 0, 0, far} // top, right, bottom, left
	off := near
	dir := ui.LeftToRight
	if rtl {
		dir = ui.RightToLeft
		firstPad, secondPad = [4]float32{0, 0, 0, far}, [4]float32{0, near, 0, 0}
		off = far
	}
	pane := func(pad [4]float32, fn func(*ui.Context)) func() {
		return func() {
			ui.Themed(c, base).Background(ui.Transparent).FillWidth().FillHeight().AlignItems(ui.Stretch).
				Padding(pad[0], pad[1], pad[2], pad[3]).Direction(ui.LeftToRight).Children(func() { fn(c) })
		}
	}
	// The direction is set on the wrapper, before the split is built: the
	// split reads it as it builds (right to left puts first at the right and
	// reverses the drag).
	ui.Themed(c, &line).Background(ui.Transparent).Grow(1).MinWidth(0).FillHeight().Direction(dir).Children(func() {
		size := shown + off
		before := size
		sp := ui.Split(c, &size, pane(firstPad, first), pane(secondPad, second)).Grow(1).MinWidth(0)
		if (sp.Changed() || size != before) && !fixed {
			*stored = clampWidth(size-off, lo, hi)
			f.movedNow, f.widthsTouched = true, true
		}
	})
}

// widthsSaveDelay is how long a divider must rest before its widths are
// saved (D111).
var widthsSaveDelay = 400 * time.Millisecond

// saveWidthsAfterDrag saves the widths once the dividers rest for
// widthsSaveDelay, off the UI thread: every move restarts the wait, so a
// drag writes once, after it ends (D111). A frame with no move does nothing.
func (f *Frame) saveWidthsAfterDrag() {
	if !f.movedNow {
		return
	}
	f.movedNow = false
	save := f.SaveWidths
	if save == nil {
		return
	}
	if f.saveTimer != nil {
		f.saveTimer.Stop()
	}
	w := widths.Widths{Sidebar: f.sidebarW, Panel: f.panelW}
	post := f.post
	f.saveTimer = time.AfterFunc(widthsSaveDelay, func() {
		if err := save(w); err != nil && post != nil {
			post(func() { f.Toasts.Show(err.Error()) })
		}
	})
}

// content draws the terminal, and the PR pane while the viewers are shown (D147).
func (f *Frame) content(c *ui.Context) {
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			f.terminal(c)
		})
		if viewersEnabled && f.PR != nil {
			f.PR.View(c)
		}
	})
}

// panelBox draws the v0.2 right panel (board or worker). It draws nothing while the viewers
// are hidden (D147, [viewersEnabled]); the redesign (#333) decides where it goes.
func (f *Frame) panelBox(c *ui.Context) {
	if !viewersEnabled {
		return
	}
	ui.Box(c).Label(LabelPanel).FillWidth().FillHeight().Children(func() { f.panel(c) })
}

// panel draws the worker view while a worker is selected, and the slot (the
// board) otherwise: the panel follows the selected tab.
func (f *Frame) panel(c *ui.Context) {
	if f.Worker != nil && f.Worker.Following() {
		f.Worker.View(c)
		return
	}
	f.run(c, f.Slots.Panel)
}

// titleBar draws the 46px row at the top of the glass: a drag area that keeps
// the room of the system traffic lights, with the sidebar toggle in it. The tabs
// start above the terminal panel; the diff summary and the files toggle are at
// the right edge (docs/spec/TitleBar.md). sidebarW is the drawn width of the
// sidebar; narrow tells that the files panel is hidden by the window width.
func (f *Frame) titleBar(c *ui.Context, sidebarW float32, narrow bool) {
	ui.Row(c).Label(LabelTitleBar).FillWidth().Height(theme.TitleBarHeight).Shrink(0).AlignItems(ui.Center).
		PaddingEnd(theme.TitleBarPadEnd).DragWindow().Children(func() {
		if f.sidebarOpen {
			// The window buttons sit over this corner. The row is as wide as the
			// sidebar plus its gaps, so the tabs start at the terminal panel.
			// An icon column is narrower than the window buttons and the toggle.
			corner := max(sidebarW+2*theme.PanelGap, theme.TrafficLightInset+theme.TitleButtonSize+theme.PanelGap)
			ui.Row(c).Width(corner).FillHeight().Shrink(0).AlignItems(ui.Center).
				PaddingStart(theme.TrafficLightInset).Children(func() {
				toggleButton(c, LabelSidebarToggle, true, f.ToggleSidebar)
			})
		} else {
			ui.Box(c).Width(theme.TrafficLightInset).Shrink(0)
			toggleButton(c, LabelSidebarReopen, false, f.ToggleSidebar)
		}
		f.bar.setTabs(f.titleTabs())
		f.bar.selectTab, f.bar.closeTab = f.selectTab, f.closeTab
		f.bar.closeOthers, f.bar.moveTab = f.closeOtherTabs, f.moveTab
		f.bar.newTab = nil
		if f.NewTerm != nil {
			f.bar.newTab = f.newTerminal
		}
		f.bar.diff, f.bar.openReview = f.Diff, f.openReviewView
		f.bar.search = f.openFind
		f.bar.build(c, f.panelOpen && !narrow, narrow, f.TogglePanel)
	})
}

// openReviewView shows the Review view. A second click on the diff summary does nothing: a tab
// brings the terminal back.
func (f *Frame) openReviewView() {
	if !f.reviewOpen {
		f.ToggleReview()
	}
}

// terminal shows the terminal slot, or the Review view.
func (f *Frame) terminal(c *ui.Context) {
	if f.reviewOpen && f.Review != nil {
		ui.Box(c).Label(LabelReview).Grow(1).MinWidth(0).FillHeight().
			Background(color(theme.BgWindow)).Children(func() { f.Review.View(c) })
		return
	}
	// No background: the terminal is clear over the panel (docs/spec/Terminal.md).
	col := ui.Column(c).Label(LabelTerminal).Grow(1).MinWidth(0).Center()
	col.Children(func() {
		f.run(c, f.Slots.Terminal)
	})
}

// titleTabs returns the tabs of the selected agent. The open project follows the selected
// orchestrator, worker or plain terminal, and keeps its last value for any other selection; the
// agent keeps its last value while a plain terminal or a worktree is selected.
func (f *Frame) titleTabs() []Tab {
	if f.agentSnap == nil {
		return nil
	}
	snap := f.agentSnap()
	var sel state.Selected
	var ok bool
	if f.Selection != nil {
		sel, ok = f.Selection.Current()
	}
	f.openProject = OpenProject(snap, sel, ok, f.openProject)
	f.tabAgent = TabAgent(snap, sel, ok, f.openProject, f.tabAgent)
	selected := ""
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		selected = sel.ID
	}
	return OrderTabs(TitleTabs(snap, f.openProject, f.tabAgent, selected), f.tabOrder)
}

// newTerminal opens a plain terminal in the open project (D82). Its folder is
// the worktree of the project's first orchestrator, else the home folder.
// UI thread.
func (f *Frame) newTerminal() {
	if f.NewTerm == nil {
		return
	}
	project := f.openProject
	f.NewTerm.Start(project, f.terminalDir(project))
}

// closeSelectedTerminal stops the selected plain terminal (S4, D85). A worker
// or orchestrator tab, or no selection, does nothing. UI thread.
func (f *Frame) closeSelectedTerminal() {
	if f.CloseTerm == nil || f.Selection == nil {
		return
	}
	if sel, ok := f.Selection.Current(); ok && sel.Kind == state.KindTerminal {
		f.CloseTerm.Start(sel.ID)
	}
}

// terminalClosed moves the selection off a closed terminal: to the first other
// tab of the open project, else to nothing. A selection that moved already
// stays. UI thread.
func (f *Frame) terminalClosed(addr string) {
	sel, ok := f.Selection.Current()
	if !ok || sel.Kind != state.KindTerminal || sel.ID != addr {
		return
	}
	for _, t := range f.titleTabs() {
		if t.Address != addr {
			f.selectTab(t)
			return
		}
	}
	f.Selection.Clear()
}

// terminalDir is the start folder of a new terminal in project.
func (f *Frame) terminalDir(project string) string {
	if f.agentSnap != nil {
		for _, o := range f.agentSnap().Orchestrators {
			if o.Project == project && o.Worktree != "" {
				return o.Worktree
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("no home folder for the new terminal", "err", err)
		return ""
	}
	return home
}

// selectTab selects the terminal of a title bar tab. A tab shows its terminal, so it closes the
// Review view.
func (f *Frame) selectTab(t Tab) {
	if f.Selection == nil {
		return
	}
	f.reviewOpen = false
	kind := state.KindWorker
	switch {
	case t.Orchestrator:
		kind = state.KindOrchestrator
	case t.Plain:
		kind = state.KindTerminal
	}
	f.Selection.Select(state.Selected{Kind: kind, ID: t.Address})
}

// closeTab closes a plain terminal tab (D85). An agent tab has no close button.
func (f *Frame) closeTab(t Tab) {
	if f.CloseTerm != nil && t.Closable() {
		f.CloseTerm.Start(t.Address)
	}
}

// closeOtherTabs closes every plain terminal tab except keep. The agent's own tab never closes
// (D85). UI thread.
func (f *Frame) closeOtherTabs(keep Tab) {
	for _, t := range f.bar.tabs {
		if t.Address != keep.Address {
			f.closeTab(t)
		}
	}
}

// moveTab puts the plain tab from in the place of the plain tab to. The agent's own tab does
// not move. The order lasts for the session. UI thread.
func (f *Frame) moveTab(from, to string) {
	order, ok := MoveTab(f.bar.tabs, from, to)
	if !ok {
		return
	}
	// Keep the order of the other projects' tabs; it only matters among tabs of one project.
	for _, a := range f.tabOrder {
		if !slices.Contains(order, a) {
			order = append(order, a)
		}
	}
	f.tabOrder = order
}

// toggleButton draws the sidebar toggle: a small outline with one filled
// part on the left, which shows while the sidebar is open.
func toggleButton(c *ui.Context, label string, open bool, onClick func()) {
	ui.ButtonBase(c).Label(label).Tooltip("Show or hide the sidebar").Size(theme.TitleButtonSize, theme.TitleButtonSize).Shrink(0).
		Cursor(ui.CursorPointer).OnClick(onClick).Children(func() {
		ui.Row(c).Size(theme.TitleGlyphWidth, theme.TitleGlyphHeight).
			Border(1, color(theme.TextDim)).Radius(theme.SidebarToggleRadius).Children(func() {
			if open {
				ui.Box(c).Width(theme.TitleGlyphFill).FillHeight().Background(color(theme.TextDim))
			}
		})
	})
}

// Window is the part of a MyGo window that [Frame.Attach] uses.
type Window interface {
	state.WindowEvents
	IsFocused() bool
	OnFocus(fn func()) (off func())
	OnBlur(fn func()) (off func())
	Invalidate()
	Update(fn func())
}

// scope is the project the board shows: the selected orchestrator's,
// or "" (all projects) when none is selected.
func (f *Frame) scope(src AgentSource) string {
	sel, ok := f.Selection.Current()
	if !ok || sel.Kind != state.KindOrchestrator {
		return ""
	}
	for _, o := range src.Snapshot().Orchestrators {
		if o.Address == sel.ID {
			return o.Project
		}
	}
	return ""
}

func (f *Frame) syncAgents() {
	if f.Worker != nil {
		f.Worker.Sync()
	}
	if f.SpacesList != nil {
		f.SpacesList.Sync()
	}
	if f.PR != nil && f.agentSnap != nil {
		f.PR.OnAgentsSync(f.agentSnap().Workers)
	}
	f.syncWorkerPRs()
}

// loadProjects reads the projects store off the UI thread and gives it to the
// sidebar (D109). A read error shows in the status message; the store is then
// empty, so the sidebar keeps the tree order, and the file stays as it is (D63).
func (f *Frame) loadProjects(w Window) {
	load := f.LoadProjects
	if load == nil {
		return
	}
	go func() {
		store, err := load()
		w.Update(func() {
			if err != nil {
				f.Toasts.Show(err.Error())
			}
			if store != nil {
				f.Projects = store
				f.SpacesList.Adopt = err == nil
				f.SpacesList.SetProjects(store)
			}
		})
	}()
}

// loadWidths reads the saved widths off the UI thread (D111). A bad file shows
// in the status message and the defaults stay. A width the owner already
// dragged is not replaced.
func (f *Frame) loadWidths(w Window) {
	load := f.LoadWidths
	if load == nil {
		return
	}
	go func() {
		saved, err := load()
		w.Update(func() {
			if err != nil {
				f.Toasts.Show(err.Error())
			}
			if f.widthsTouched {
				return
			}
			if saved.Sidebar > 0 {
				f.sidebarW = clampWidth(saved.Sidebar, theme.SidebarMinWidth, theme.SidebarMaxWidth)
			}
			if saved.Panel > 0 {
				f.panelW = clampWidth(saved.Panel, theme.PanelMinWidth, theme.PanelMaxWidth)
			}
			w.Invalidate()
		})
	}()
}

// syncWorkerPRs starts the pull request reads for the workers now (D104).
func (f *Frame) syncWorkerPRs() {
	if f.WorkerPRs != nil && f.agentSnap != nil {
		f.WorkerPRs.Sync(f.agentSnap().Workers)
	}
}

// Attach connects the frame's state to the window. A change of any of them
// redraws the window; Visibility follows the window. Call it once, on the UI
// thread, after the window exists. The returned function ends the watching.
func (f *Frame) Attach(w Window) (stop func()) {
	f.Selection = state.NewSelection(func() { w.Invalidate(); f.syncReview(); f.syncTerminal() })
	f.Review = NewReview(w.Update, w.Invalidate)
	f.Files.Post, f.Files.Invalidate = w.Update, w.Invalidate
	f.Toasts = state.NewToasts(w.Invalidate)
	f.Files.Status = f.Toasts.Show
	f.PR = &PRPane{Run: Runner{Post: w.Update}, Invalidate: w.Invalidate, OpenURL: openURL}
	f.WorkerPRs = &WorkerPRs{Run: Runner{Post: w.Update}, Invalidate: w.Invalidate}
	var agentSnap func() agentstate.Snapshot
	var src AgentSource
	stopAgents := func() {}
	if f.StartAgents != nil {
		src, stopAgents = f.StartAgents(func() { w.Update(f.syncAgents) })
	}
	if src != nil {
		agentSnap = src.Snapshot
		f.agentSnap = agentSnap
		run := Runner{Post: w.Update}
		f.Card = &QuestionsCard{Source: src, Run: run, Invalidate: w.Invalidate,
			Select: func(address string, orchestrator bool) {
				kind := state.KindWorker
				if orchestrator {
					kind = state.KindOrchestrator
				}
				f.Selection.Select(state.Selected{Kind: kind, ID: address})
			}}
		f.CloseTerm = &CloseTerminal{Stop: src.Close, Run: run, Status: f.Toasts.Show, Closed: f.terminalClosed}
		setCloseTerminalHandler(func() { w.Update(f.closeSelectedTerminal) })
		entry := WorkerEntry{PRs: f.WorkerPRs, OpenPR: f.paneOpener(), OpenIssue: f.OpenIssue}
		f.Card.Entry = entry
		f.Worker = &WorkerPanel{Entry: entry, Source: src, Selection: f.Selection, Run: run, Status: f.Toasts.Show, Invalidate: w.Invalidate}
		f.PR.Orchestrator, f.PR.Send = f.prOrchestrator, src.Send
	}
	removeOutline := func() {}
	if agentSnap != nil {
		f.Outline = NewOutline(agentSnap, f.Selection, w.Invalidate)
		removeOutline = f.Overlay.Add(LabelOutline, f.Outline.View)
	}
	if f.Host == nil {
		f.Host = host.NewSystem(w.Invalidate)
	}
	// The git scan runs on a goroutine and lands through Window.Update.
	if f.SpacesList == nil {
		f.SpacesList = NewSpacesList(agentSnap, f.Selection, w.Update)
	}
	f.SpacesList.Host = f.Host
	f.SpacesList.ByKey = func(do func()) { f.keyNav = true; do(); f.keyNav = false }
	f.SpacesList.OnOpen = func() { f.focusTerm = true; w.Invalidate() }
	f.SpacesList.ModelOf = f.ModelOf
	f.SpacesList.Run, f.SpacesList.Status = Runner{Post: w.Update}, f.Toasts.Show
	f.SpacesList.Entry = f.agentRowEntry()
	f.SpacesList.Ages = &AgeClock{Tick: w.Invalidate}
	if src != nil {
		f.SpacesList.Source = src
	}
	f.SpacesList.WatchHead = f.WatchHead
	f.SpacesList.OnError = func(err error) { f.Toasts.Show(err.Error()) }
	if f.PickFolder != nil && f.SpawnOrchestrator != nil {
		f.NewOrch = &NewOrchestrator{Pick: f.PickFolder, Spawn: f.SpawnOrchestrator,
			Run: Runner{Post: w.Update}, Status: f.Toasts.Show}
		f.SpacesList.OnNew = f.NewOrch.Start
		f.SpacesList.OnNewIn = f.NewOrch.StartIn
	}
	if f.OpenTerminal != nil {
		f.NewTerm = &NewTerminal{Open: f.OpenTerminal, Run: Runner{Post: w.Update}, Status: f.Toasts.Show,
			Select: func(addr string) { f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: addr}) }}
		setNewTerminalHandler(func() { w.Update(f.newTerminal) })
	}
	f.ProjectsModal = &ProjectsModal{
		Groups: f.SpacesList.Groups,
		Store:  func() *projects.Store { return f.Projects },
		Apply: func() {
			if f.Projects != nil {
				f.SpacesList.SetProjects(f.Projects)
			}
		},
		PickFolder: f.OpenFolder, Resolve: f.ProjectKey,
		CloneParent: f.CloneParent, PickParent: f.PickCloneParent, CloneRun: f.CloneRun,
		Save: f.SaveProjects, Run: Runner{Post: w.Update}, Status: f.Toasts.Show, Invalidate: w.Invalidate,
	}
	f.SpacesList.OnProjects = f.ProjectsModal.Toggle
	f.SpacesList.OnReordered = f.ProjectsModal.changed
	f.SpacesList.OnAdopted = f.ProjectsModal.changed
	f.lateWin.set(w)
	if f.themeErr != nil {
		f.Toasts.Show(f.themeErr.Error())
		f.themeErr = nil
	}
	f.Settings = &Settings{Updates: f.Updates, Version: Version, CoreVersion: f.coreVersion, Run: Runner{Post: w.Update},
		Relaunch: f.Relaunch, Invalidate: w.Invalidate, Visible: func() bool { return f.Visibility == nil || f.Visibility.Visible() },
		Status: f.Toasts.Show, Appearance: f.Appearance, SetAppearance: f.SetAppearance}
	setSettingsHandler(func() { w.Update(f.Settings.Toggle) })
	f.SpacesList.OnSettings = func() { f.Settings.Toggle() }
	f.SpacesList.UpdateReady = f.Settings.Ready
	stopChecks := f.Settings.StartChecks()
	f.Slots.Spaces = f.SpacesList.View
	f.SpacesList.Sync()
	f.loadProjects(w)
	f.loadWidths(w)
	var scope func() string
	if src != nil {
		scope = func() string { return f.scope(src) }
	}
	f.Notch = &Notch{Snapshot: agentSnap, Project: func() string { return f.openProject }}
	if src != nil {
		f.Notch.Open = f.Card.Open
		f.Notch.Close = f.Card.Close
		f.Notch.Opened = f.Card.IsOpen
		f.Notch.Panel = f.Card.View
	}
	f.Overlay.Add(LabelNotchLayer, f.Notch.View)
	f.Panel = &BoardPanel{Snapshot: agentSnap, Needs: NeedItems(agentSnap, scope)}
	f.Panel.OpenPR = f.paneOpener()
	f.Panel.OpenIssue = f.OpenIssue
	f.Panel.PRs = f.WorkerPRs
	f.syncWorkerPRs()
	if src != nil {
		f.Panel.Scope = scope
		f.Panel.Open = f.Card.Open
		f.Panel.Select = func(address string) {
			f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: address})
		}
	}
	f.Slots.Panel = f.Panel.View
	if f.Usage == nil {
		f.Usage = usage.NewFileSampler(w.Invalidate)
	}
	f.Visibility = state.NewVisibility(func(visible bool) {
		f.SpacesList.Ages.SetVisible(visible)
		f.Host.SetVisible(visible)
		f.Usage.SetVisible(visible)
		w.Update(func() {
			if f.conn != nil {
				f.conn.SetVisible(visible)
			}
		})
		w.Update(func() { f.Settings.SetVisible(visible) })
		if visible {
			w.Update(f.syncReview)
		}
	})
	// Visibility starts as visible, so a window that starts hidden is the only change that Watch
	// reports. The samplers start hidden: tell them the window state now.
	f.Host.SetVisible(w.IsVisible())
	f.Usage.SetVisible(w.IsVisible())
	f.SpacesList.Ages.SetVisible(w.IsVisible())
	unwatch := f.Visibility.Watch(w)
	term := f.attachTerminal(w)
	return func() {
		setNewTerminalHandler(nil)
		setCloseTerminalHandler(nil)
		setSettingsHandler(nil)
		stopChecks()
		f.Files.Close()
		unwatch()
		term()
		removeOutline()
		stopAgents()
		f.SpacesList.Close()
		f.Host.Close()
		f.Usage.Close()
	}
}

// TerminalOffLine is shown while no agent is selected.
const TerminalOffLine = "select an agent to see its terminal"

// attachTerminal puts the terminal view in its slot and follows the window
// focus. The returned function stops both. The connection runs while the
// window is hidden, as in the Rust app; MyGo holds the frames (D9).
func (f *Frame) attachTerminal(w Window) (stop func()) {
	// Update posts and returns: a connection goroutine never waits for the
	// main thread, which may be in Conn.Close waiting for it (#204).
	f.post, f.invalidate = w.Update, w.Invalidate
	f.winFocused = w.IsFocused()
	f.syncTerminal()
	f.Slots.Terminal = f.buildTerminal
	removeFind := f.Overlay.Add(LabelFindLayer, f.findView)
	offFocus := w.OnFocus(func() { f.setTerminalFocus(true) })
	offBlur := w.OnBlur(func() { f.setTerminalFocus(false) })
	return func() {
		offFocus()
		offBlur()
		removeFind()
		f.closeTerminal()
	}
}

func (f *Frame) setTerminalFocus(focused bool) {
	f.winFocused = focused
	f.syncOutlineFocus()
	if f.conn != nil {
		if err := f.conn.SetFocus(focused); err != nil {
			slog.Debug("terminal focus not sent", "err", err)
		}
	}
}

// syncOutlineFocus tells the outline which terminal has the focus: the shown
// one while the window has the focus.
func (f *Frame) syncOutlineFocus() {
	addr := ""
	if f.winFocused {
		addr = f.termAddr
	}
	f.Outline.Focus(addr)
}

// terminalOptions are the plugin's options: contr's colors, JetBrains Mono at
// the design's size and row height, and the design's padding.
func terminalOptions(conn *ghostty.Conn, open func(string)) gterm.Options {
	light, dark := terminalColors()
	return gterm.Options{
		Conn: conn,
		// The screen takes the core's size, which the Conn reads from the
		// stream, so a second viewer keeps its size while contr watches (D91).
		ResizeByConn: true,
		// Cmd-click opens http, https and mailto links only (D94).
		OpenLink: terminalOpenLink(open),
		// The colors follow the theme without a reconnect: the plugin takes
		// Theme in a light window and DarkTheme in a dark one.
		Theme:     light,
		DarkTheme: dark,
		// The glass shows through: the plugin draws no background.
		Transparent: true,
		Font: gterm.Font{
			Family: theme.FontFamily, Size: theme.TerminalFontSize,
			LineHeight: theme.TerminalLineHeightScale,
			Features:   []string{"-calt", "-clig", "-liga"},
		},
		// The padding is the view's (terminalGrid): the spec's sides and top
		// differ from its bottom, and the plugin has one value for each axis.
		Padding: gterm.Padding{X: -1, Y: -1},
	}
}

// closeTerminal closes the terminal, which closes its connection. The program
// keeps running in hc.
func (f *Frame) closeTerminal() {
	if f.term != nil {
		_ = f.term.Close()
	} else if f.conn != nil {
		_ = f.conn.Close()
	}
	f.term, f.conn = nil, nil
}

// syncTerminal points the terminal view at the hc terminal of the selected
// agent row (D12). Any other selection, or no source, shows no terminal. It
// does nothing when the terminal stays the same. UI thread.
func (f *Frame) syncTerminal() {
	if f.post == nil {
		return
	}
	addr := ""
	if f.Selection != nil && f.TerminalSource != nil {
		if sel, ok := f.Selection.Current(); ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
			addr = sel.ID
		}
	}
	if addr == f.termAddr && f.termSet {
		return
	}
	f.closeTerminal()
	f.resetFind()
	f.termAddr, f.termSet = addr, true
	f.syncOutlineFocus()
	f.termState, f.termErr = ghostty.State{Kind: ghostty.Connecting}, ""
	f.invalidate()
	if addr == "" {
		return
	}
	post, invalidate := f.post, f.invalidate
	var conn *ghostty.Conn
	conn = ghostty.NewConn(f.TerminalSource(addr), ghostty.Options{Watch: true, OnState: func(s ghostty.State) {
		post(func() {
			if f.conn == conn {
				f.termState = s
				invalidate()
			}
		})
	}})
	if f.Visibility != nil && !f.Visibility.Visible() {
		conn.SetVisible(false)
	}
	opts := terminalOptions(conn, openURL)
	opts.OnSearch = f.onSearch
	t, err := gterm.New(opts)
	if err != nil {
		_ = conn.Close()
		slog.Error("terminal not started", "err", err)
		f.termErr = fmt.Sprintf("terminal unavailable: %v", err)
		return
	}
	conn.SetScreen(t)
	f.term, f.conn = t, conn
	f.autoFocus = !f.keyNav
	f.setTerminalFocus(f.winFocused)
}

// TerminalNotRunningLine shows over the grid while the terminal's program is
// not running, or the connection to hc is lost.
const TerminalNotRunningLine = "not running"

// coreVersion is the hc core's version while the agents follow the core.
func (f *Frame) coreVersion() (string, bool) {
	if f.agentSnap == nil {
		return "", false
	}
	v := f.agentSnap().CoreVersion
	return v, v != ""
}

// staleOpacity is the grid's opacity while the connection is not live and the
// screen on show may be out of date.
const staleOpacity = 0.5

// terminalLine is the line shown instead of, or over, the grid, with its
// colour. ok is false when the grid speaks for itself. The empty text and the
// reasons the terminal cannot show are ink-42; the line of an ended process
// is ink-38 (docs/spec/Terminal.md).
func (f *Frame) terminalLine() (line string, colour ui.Color, ok bool) {
	p := theme.Current()
	if f.term == nil {
		if f.termErr != "" {
			return f.termErr, p.Ink42, true
		}
		return TerminalOffLine, p.Ink42, true
	}
	switch f.termState.Kind {
	case ghostty.NotRunning:
		return TerminalNotRunningLine, p.Ink38, true
	case ghostty.Incompatible:
		return f.termState.Message, p.Ink42, true
	}
	return "", ui.Color{}, false
}

// buildTerminal is the frame's terminal slot: the plugin's view of the
// selected agent's terminal, dimmed and with a line while the connection is
// down, and the overlay layer above them. The grid sits inside the spec's
// padding: 18 above, 26 at the sides, 10 below.
func (f *Frame) buildTerminal(c *ui.Context) {
	line, colour, showLine := f.terminalLine()
	ui.Box(c).Fill().Center().Children(func() {
		if f.term != nil {
			ui.Box(c).Absolute().Fill().Padding(theme.Space18, theme.Space26, theme.Space10).Children(func() {
				grid := gterm.View(c, f.term).Label(LabelTerminalGrid).Fill()
				if f.autoFocus {
					f.autoFocus = false
					grid.AutoFocus()
				}
				if f.find.refocus {
					// The find bar closed: the keys go to the terminal again.
					f.find.refocus = false
					grid.Focus()
				}
				if f.termState.Kind != ghostty.Live {
					grid.Opacity(staleOpacity)
				}
				if f.focusTerm {
					f.focusTerm = false
					grid.Focus()
				}
			})
		}
		if showLine {
			ui.Text(c, line).TextColor(colour)
		}
		f.Overlay.View(c)
	})
}

// openURL opens a web address in the default browser, off the UI thread.
func openURL(u string) {
	go func() {
		if err := exec.Command("open", u).Run(); err != nil {
			log.Printf("open %s: %v", u, err)
		}
	}()
}
