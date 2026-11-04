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

	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
)

// Slot is a view that fills one place of the frame. Later milestones replace
// the placeholders of [PlaceholderSlots] with the real views.
type Slot func(c *ui.Context)

// Slots are the views of the frame. A nil slot is left empty.
type Slots struct {
	Spaces    Slot // sidebar, under its header
	Agents    Slot // sidebar, at the bottom
	Terminal  Slot // the selected hc terminal
	Needs     Slot // needs-you row, under the terminal; draws nothing when empty
	Card      Slot // answer card, floating at the bottom of the terminal area
	Panel     Slot // board panel, shown while it is open
	StatusBar Slot
}

// PlaceholderSlots returns slots that show only their own name in dim text.
func PlaceholderSlots() Slots {
	return Slots{
		Spaces:    placeholder("spaces"),
		Agents:    placeholder("agents"),
		Terminal:  placeholder("terminal"),
		Panel:     placeholder("panel"),
		StatusBar: nil,
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
	// Visibility, Selection and Status are the app state. The frame holds
	// them for the views in its slots.
	Visibility *state.Visibility
	Selection  *state.Selection
	Status     *state.StatusMessage
	// Projects is the projects store, loaded after Attach (D109). Nil until then.
	Projects *projects.Store
	// Needs is the needs-you row. [Frame.Attach] makes it and puts its view
	// in Slots.Needs.
	Needs *NeedsRow
	// Card is the answer card that the needs-you row opens. [Frame.Attach]
	// makes it and puts its view in Slots.Card.
	Card *FloatCard
	// Worker is the panel's worker view. [Frame.Attach] makes it. The panel
	// shows it while a worker is selected.
	Worker *WorkerPanel
	// AgentsList is the agents section. [Frame.Attach] makes it and puts
	// its view in Slots.Agents.
	AgentsList *AgentsList
	// StartAgents follows the agents. [Frame.Attach] calls it once, with a
	// notify function that is safe from any goroutine, and uses the source
	// and the stop function it returns. Nil means no agents.
	StartAgents func(notify func()) (src AgentSource, stop func())
	// PickFolder and SpawnOrchestrator serve `+ new` (D57): the first asks the
	// owner for a folder (ErrPickCancelled for none), the second starts the
	// orchestrator profile there. Both block, so the frame runs them off the
	// UI thread. Nil makes `+ new` do nothing.
	PickFolder func() (string, error)
	// LoadProjects reads the projects store (D109). It runs on a goroutine.
	// Nil reads the file in the user config folder.
	LoadProjects      func() (*projects.Store, error)
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
	// Slots.Panel.
	Panel *BoardPanel
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
	// Host and Usage feed the status bar. [Frame.Attach] creates them when they are nil (tests set
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

	status statusBar
	bar    titleBar
	// agentSnap gives the agents; nil before Attach or without agents. openProject is the
	// project the title bar shows (D50).
	agentSnap   func() agentstate.Snapshot
	openProject string
	sidebarOpen bool
	panelOpen   bool
	reviewOpen  bool
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
	// post and invalidate are the window's; nil before attachTerminal.
	post       func(fn func())
	invalidate func()
	winFocused bool
	// reviewDir is the repo that Review shows now. Empty: none.
	reviewDir string
	reviewSet bool
}

// NewFrame returns a frame with the sidebar and the panel open and
// placeholder slots.
func NewFrame() *Frame {
	return &Frame{Slots: PlaceholderSlots(), sidebarOpen: true, panelOpen: true}
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
func (f *Frame) OpenPR(dir string, n int) {
	if f.PR != nil {
		f.PR.Open(dir, n)
	}
}

// OpenIssue opens issue n of the repository in dir in the browser, off the UI thread (D74). A
// failure shows in the status message.
func (f *Frame) OpenIssue(dir string, n int) {
	open := f.OpenIssueWeb
	if open == nil {
		open = func(dir string, n int) error { return github.New(dir).OpenIssue(context.Background(), n) }
	}
	run := Runner{}
	if f.WorkerPRs != nil {
		run = f.WorkerPRs.Run
	}
	run.Do(func() func() {
		err := open(dir, n)
		return func() {
			if err != nil && f.Status != nil {
				f.Status.Show(err.Error())
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
	LabelSidebarHeader = "sidebar header"
	LabelSidebarToggle = "Toggle sidebar"
	LabelSidebarReopen = "Show sidebar"
	LabelTitleBar      = "title bar"
	LabelTerminal      = "terminal area"
	LabelTerminalGrid  = "terminal grid"
	LabelReview        = "review area"
	LabelPanel         = "board panel"
	LabelPanelToggle   = "Toggle panel"
	LabelStatusBar     = "status bar"
)

func color(rgb uint32) ui.Color { return theme.Color(rgb) }

// View builds the frame: the sidebar over the full height, the title bar,
// the terminal and the panel to its right, and the status bar below them.
func (f *Frame) View(c *ui.Context) {
	ui.Column(c).Fill().Background(color(theme.BgWindow)).TextColor(color(theme.Text)).Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if f.sidebarOpen {
				f.sidebar(c)
			}
			f.main(c)
		})
		ui.Box(c).Label(LabelStatusBar).FillWidth().Height(theme.StatusBarHeight).Shrink(0).
			Background(color(theme.BgBars)).BorderWidth(1, 0, 0, 0).BorderColor(color(theme.Line)).
			Children(func() { f.statusBar(c) })
	})
}

// statusBar draws the slot when one is set, and the status bar otherwise.
func (f *Frame) statusBar(c *ui.Context) {
	if f.Slots.StatusBar != nil {
		f.Slots.StatusBar(c)
		return
	}
	f.status.core = f.coreVersion
	if f.Host != nil {
		f.status.host = f.Host
	}
	if f.Usage != nil {
		f.status.usage = f.Usage
	}
	if f.Status != nil {
		f.status.message = f.Status
	}
	f.status.View(c)
}

func (f *Frame) run(c *ui.Context, s Slot) {
	if s != nil {
		s(c)
	}
}

func (f *Frame) sidebar(c *ui.Context) {
	ui.Column(c).Label(LabelSidebar).Width(theme.SidebarWidth).Shrink(0).
		Background(color(theme.BgBars)).BorderEndWidth(1).
		BorderColor(color(theme.Line)).Children(func() {
		ui.Row(c).Label(LabelSidebarHeader).FillWidth().Height(theme.TitleBarHeight).Shrink(0).
			PaddingStart(theme.TrafficLightInset).Gap(theme.SpacesGap).DragWindow().Children(func() {
			toggleButton(c, LabelSidebarToggle, true, f.ToggleSidebar)
			ui.Text(c, "contr").TextColor(color(theme.Text2))
		})
		ui.Box(c).Grow(1).MinHeight(0).Children(func() { f.run(c, f.Slots.Spaces) })
		// The agents section takes at most its share of the sidebar.
		ui.Column(c).Shrink(0).MaxHeightPercent(theme.AgentsMaxShare * 100).Children(func() { f.run(c, f.Slots.Agents) })
	})
}

func (f *Frame) main(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		f.titleBar(c)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				f.terminal(c)
				f.run(c, f.Slots.Needs)
			})
			if f.PR != nil {
				f.PR.View(c)
			}
			if f.panelOpen {
				ui.Box(c).Label(LabelPanel).Width(theme.PanelWidth).Shrink(0).
					Background(color(theme.BgWindow)).BorderStartWidth(1).BorderColor(color(theme.Line)).
					Children(func() { f.panel(c) })
			}
		})
	})
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

func (f *Frame) titleBar(c *ui.Context) {
	ui.Row(c).Label(LabelTitleBar).FillWidth().Height(theme.TitleBarHeight).Shrink(0).
		Background(color(theme.BgBars)).BorderWidth(0, 0, 1, 0).BorderColor(color(theme.Line)).
		PaddingEnd(theme.TitleBarPadEnd).DragWindow().Children(func() {
		if !f.sidebarOpen {
			// The window buttons sit over this corner: keep the room, then
			// show the toggle that opens the sidebar again.
			ui.Box(c).Width(theme.TrafficLightInset).Shrink(0)
			toggleButton(c, LabelSidebarReopen, false, f.ToggleSidebar)
		}
		f.bar.setTabs(f.agentTabs())
		f.bar.selectTab = f.selectTab
		f.bar.toggleReview, f.bar.reviewOpen = f.ToggleReview, f.reviewOpen
		f.bar.collapsed = f.PROpen()
		f.bar.newTab = nil
		if f.NewTerm != nil {
			f.bar.newTab = f.newTerminal
		}
		f.bar.build(c, f.panelOpen, f.TogglePanel)
	})
}

// terminal shows the terminal slot and the answer card, or the Review view.
func (f *Frame) terminal(c *ui.Context) {
	if f.reviewOpen && f.Review != nil {
		ui.Box(c).Label(LabelReview).Grow(1).MinWidth(0).FillHeight().
			Background(color(theme.BgWindow)).Children(func() { f.Review.View(c) })
		return
	}
	col := ui.Column(c).Label(LabelTerminal).Grow(1).MinWidth(0).Center().
		Background(color(theme.BgTerminal))
	col.Children(func() {
		f.run(c, f.Slots.Terminal)
		// The card floats over the bottom edge of this strip. The strip has
		// no height, so a press on the terminal is outside the card.
		if f.Card != nil {
			f.Card.Anchor = ui.Box(c).FillWidth().Height(0).Shrink(0)
		}
		f.run(c, f.Slots.Card)
	})
}

// agentTabs returns the tabs of the open project. The open project follows the selected
// orchestrator, worker or plain terminal, and keeps its last value for any other selection.
func (f *Frame) agentTabs() []AgentTab {
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
	selected := ""
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		selected = sel.ID
	}
	return AgentTabs(snap, f.openProject, selected)
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
	for _, t := range f.agentTabs() {
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

// selectTab selects the terminal of a title bar tab.
func (f *Frame) selectTab(t AgentTab) {
	if f.Selection == nil {
		return
	}
	kind := state.KindWorker
	switch {
	case t.Orchestrator:
		kind = state.KindOrchestrator
	case t.Plain:
		kind = state.KindTerminal
	}
	f.Selection.Select(state.Selected{Kind: kind, ID: t.Address})
}

func panelToggle(c *ui.Context, open bool, onClick func()) {
	toggleButton(c, LabelPanelToggle, open, onClick)
}

// toggleTip is the tip of a sidebar or panel toggle.
func toggleTip(label string) string {
	if label == LabelPanelToggle {
		return "Show or hide the board panel"
	}
	return "Show or hide the sidebar"
}

// toggleButton draws the sidebar or panel toggle: a small outline with one
// filled part. The sidebar's fill is on the left, the panel's on the right,
// and the fill shows while the part is open.
func toggleButton(c *ui.Context, label string, open bool, onClick func()) {
	left := label != LabelPanelToggle
	ui.ButtonBase(c).Label(label).Tooltip(toggleTip(label)).Size(theme.TitleButtonSize, theme.TitleButtonSize).Shrink(0).
		Cursor(ui.CursorPointer).OnClick(onClick).Children(func() {
		glyph := ui.Row(c).Size(theme.TitleGlyphWidth, theme.TitleGlyphHeight).
			Border(1, color(theme.TextDim)).Radius(theme.SidebarToggleRadius)
		if !left {
			glyph = glyph.Reverse()
		}
		glyph.Children(func() {
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

// scope is the project the needs-you row shows: the selected orchestrator's,
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
	if f.AgentsList != nil {
		f.AgentsList.Sync()
	}
	if f.SpacesList != nil {
		f.SpacesList.Sync()
	}
	if f.PR != nil {
		f.PR.OnAgentsSync()
	}
	f.syncWorkerPRs()
}

// loadProjects reads the projects store off the UI thread and gives it to the
// sidebar (D109). A read error shows in the status message; the store is then
// empty, so the sidebar keeps the tree order, and the file stays as it is (D63).
func (f *Frame) loadProjects(w Window) {
	load := f.LoadProjects
	if load == nil {
		load = func() (*projects.Store, error) {
			path, err := projects.Path()
			if err != nil {
				return nil, err
			}
			return projects.Load(path)
		}
	}
	go func() {
		store, err := load()
		w.Update(func() {
			if err != nil {
				f.Status.Show("projects: " + err.Error())
			}
			if store != nil {
				f.Projects = store
				f.SpacesList.SetProjects(store)
			}
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
	f.Status = state.NewStatusMessage(w.Invalidate)
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
		f.Card = &FloatCard{Source: src, Run: run, Invalidate: w.Invalidate,
			Select: func(address string, orchestrator bool) {
				kind := state.KindWorker
				if orchestrator {
					kind = state.KindOrchestrator
				}
				f.Selection.Select(state.Selected{Kind: kind, ID: address})
			}}
		f.CloseTerm = &CloseTerminal{Stop: src.Close, Run: run, Status: f.Status.Show, Closed: f.terminalClosed}
		setCloseTerminalHandler(func() { w.Update(f.closeSelectedTerminal) })
		f.Worker = &WorkerPanel{Source: src, Selection: f.Selection, Run: run, Status: f.Status.Show, Invalidate: w.Invalidate}
		f.Slots.Card = f.Card.View
	}
	f.AgentsList = NewAgentsList(agentSnap, f.Selection)
	f.Slots.Agents = f.AgentsList.View
	if f.Host == nil {
		f.Host = host.NewSystem(w.Invalidate)
	}
	// The git scan runs on a goroutine and lands through Window.Update.
	if f.SpacesList == nil {
		f.SpacesList = NewSpacesList(agentSnap, f.Host, f.Selection,
			func() bool { return f.Visibility == nil || f.Visibility.Visible() }, w.Update)
	}
	f.SpacesList.OpenPR = f.OpenPR
	if f.PickFolder != nil && f.SpawnOrchestrator != nil {
		f.NewOrch = &NewOrchestrator{Pick: f.PickFolder, Spawn: f.SpawnOrchestrator,
			Run: Runner{Post: w.Update}, Status: f.Status.Show}
		f.SpacesList.OnNew = f.NewOrch.Start
	}
	if f.OpenTerminal != nil {
		f.NewTerm = &NewTerminal{Open: f.OpenTerminal, Run: Runner{Post: w.Update}, Status: f.Status.Show,
			Select: func(addr string) { f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: addr}) }}
		setNewTerminalHandler(func() { w.Update(f.newTerminal) })
	}
	f.Slots.Spaces = f.SpacesList.View
	f.SpacesList.Sync()
	f.loadProjects(w)
	f.Needs = &NeedsRow{Snapshot: agentSnap}
	if src != nil {
		f.Needs.Scope = func() string { return f.scope(src) }
		f.Needs.Open = f.Card.Open
	}
	f.Needs.Collapsed = f.PROpen
	f.Slots.Needs = f.Needs.View
	f.Panel = &BoardPanel{Snapshot: agentSnap, Needs: f.Needs.Tabs}
	f.Panel.OpenPR = f.OpenPR
	f.Panel.OpenIssue = f.OpenIssue
	f.Panel.PRs = f.WorkerPRs
	f.syncWorkerPRs()
	if src != nil {
		f.Panel.Scope = f.Needs.Scope
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
		f.Host.SetVisible(visible)
		f.Usage.SetVisible(visible)
		f.SpacesList.SetVisible(visible)
		w.Update(func() {
			if f.conn != nil {
				f.conn.SetVisible(visible)
			}
		})
		if visible {
			w.Update(f.syncReview)
		}
	})
	// Visibility starts as visible, so a window that starts hidden is the only change that Watch
	// reports. The samplers start hidden: tell them the window state now.
	f.Host.SetVisible(w.IsVisible())
	f.Usage.SetVisible(w.IsVisible())
	f.SpacesList.SetVisible(w.IsVisible())
	unwatch := f.Visibility.Watch(w)
	term := f.attachTerminal(w)
	return func() {
		setNewTerminalHandler(nil)
		setCloseTerminalHandler(nil)
		unwatch()
		term()
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
	offFocus := w.OnFocus(func() { f.setTerminalFocus(true) })
	offBlur := w.OnBlur(func() { f.setTerminalFocus(false) })
	return func() {
		offFocus()
		offBlur()
		f.closeTerminal()
	}
}

func (f *Frame) setTerminalFocus(focused bool) {
	f.winFocused = focused
	if f.conn != nil {
		if err := f.conn.SetFocus(focused); err != nil {
			slog.Debug("terminal focus not sent", "err", err)
		}
	}
}

// terminalOptions are the plugin's options: contr's colors, JetBrains Mono at
// the design's size and row height, and the design's padding.
func terminalOptions(conn *ghostty.Conn, open func(string)) gterm.Options {
	var pal [16]ui.Color
	for i, rgb := range theme.TerminalPalette {
		pal[i] = color(rgb)
	}
	return gterm.Options{
		Conn: conn,
		// The screen takes the core's size, which the Conn reads from the
		// stream, so a second viewer keeps its size while contr watches (D91).
		ResizeByConn: true,
		// Cmd-click opens http, https and mailto links only (D94).
		OpenLink: terminalOpenLink(open),
		Theme: &gterm.Theme{
			Foreground: color(theme.Text), Background: color(theme.BgTerminal), Palette: pal,
		},
		Font: gterm.Font{
			Family: theme.FontFamily, Size: theme.TerminalFontSize,
			LineHeight: theme.TerminalLineHeightScale,
			Features:   []string{"-calt", "-clig", "-liga"},
		},
		Padding: gterm.Padding{X: theme.TermPadding, Y: theme.TermPadding},
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
	f.termAddr, f.termSet = addr, true
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
	t, err := gterm.New(terminalOptions(conn, openURL))
	if err != nil {
		_ = conn.Close()
		slog.Error("terminal not started", "err", err)
		f.termErr = fmt.Sprintf("terminal unavailable: %v", err)
		return
	}
	conn.SetScreen(t)
	f.term, f.conn = t, conn
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
// colour. ok is false when the grid speaks for itself.
func (f *Frame) terminalLine() (line string, colour uint32, ok bool) {
	if f.term == nil {
		if f.termErr != "" {
			return f.termErr, theme.Text2, true
		}
		return TerminalOffLine, theme.TextDim, true
	}
	switch f.termState.Kind {
	case ghostty.NotRunning:
		return TerminalNotRunningLine, theme.Text2, true
	case ghostty.Incompatible:
		return f.termState.Message, theme.Text2, true
	}
	return "", 0, false
}

// buildTerminal is the frame's terminal slot: the plugin's view of the
// selected agent's terminal, dimmed and with a line while the connection is
// down.
func (f *Frame) buildTerminal(c *ui.Context) {
	line, colour, showLine := f.terminalLine()
	ui.Box(c).Fill().Center().Children(func() {
		if f.term != nil {
			grid := gterm.View(c, f.term).Label(LabelTerminalGrid).Absolute().Fill().AutoFocus()
			if f.termState.Kind != ghostty.Live {
				grid.Opacity(staleOpacity)
			}
		}
		if showLine {
			ui.Text(c, line).TextColor(theme.Color(colour))
		}
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
