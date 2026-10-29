// Package app holds the content of contr's window: the three-column frame
// and the views in its slots. docs/PORT-PLAN.md lists the views that come
// next.
package app

import (
	"context"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// Slot is a view that fills one place of the frame. Later milestones replace
// the placeholders of [PlaceholderSlots] with the real views.
type Slot func(c *ui.Context)

// Slots are the views of the frame. A nil slot is left empty.
type Slots struct {
	Spaces    Slot // sidebar, under its header
	Agents    Slot // sidebar, at the bottom
	Terminal  Slot // shown only while herdr is connected
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

// liveSource is the part of [Herdr] that the frame reads.
type liveSource interface {
	Live() herdr.LiveHerdr
}

// Frame is the window's view. Build it with [NewFrame] and give [Frame.View]
// to the window. Its methods run on the UI thread.
type Frame struct {
	Slots Slots
	// Herdr tells the connection state. The terminal area shows the state's
	// line while herdr is not connected.
	Herdr liveSource
	// Visibility, Selection and Status are the app state. The frame holds
	// them for the views in its slots.
	Visibility *state.Visibility
	Selection  *state.Selection
	Status     *state.StatusMessage
	// Send takes the title bar's herdr commands. [Frame.Attach] sets it. When it is nil, the
	// commands are dropped.
	Send func(TabCommand)
	// Board is the coordinator's board. [Frame.Attach] sets it. Views read
	// it with Board.View.
	Board *Board
	// Outbox queues the actions that contr sends to the coordinator mod.
	// [Frame.Attach] sets it.
	Outbox *BoardOutbox
	// AgentsList is the agents section. [Frame.Attach] makes it and puts
	// its view in Slots.Agents.
	AgentsList *AgentsList
	// Host and Usage feed the status bar. [Frame.Attach] creates them when they are nil (tests set
	// fakes first) and ends them with its stop function.
	Host  *host.Host
	Usage *usage.Sampler

	status      statusBar
	bar         titleBar
	sidebarOpen bool
	panelOpen   bool
}

// NewFrame returns a frame with the sidebar and the panel open and
// placeholder slots. A nil herdr counts as connected.
func NewFrame(h liveSource) *Frame {
	return &Frame{Slots: PlaceholderSlots(), Herdr: h, sidebarOpen: true, panelOpen: true}
}

// SidebarOpen tells whether the sidebar shows.
func (f *Frame) SidebarOpen() bool { return f.sidebarOpen }

// PanelOpen tells whether the board panel shows.
func (f *Frame) PanelOpen() bool { return f.panelOpen }

// ToggleSidebar shows or hides the sidebar. It sends no herdr command.
func (f *Frame) ToggleSidebar() { f.sidebarOpen = !f.sidebarOpen }

// TogglePanel shows or hides the board panel. It sends no herdr command.
func (f *Frame) TogglePanel() { f.panelOpen = !f.panelOpen }

func (f *Frame) connectionLine() (string, bool) {
	if f.Herdr == nil {
		return "", false
	}
	return f.Herdr.Live().State.Line()
}

// Element labels. Tests and the e2e driver find the frame's parts by them.
const (
	LabelSidebar       = "sidebar"
	LabelSidebarHeader = "sidebar header"
	LabelSidebarToggle = "Toggle sidebar"
	LabelSidebarReopen = "Show sidebar"
	LabelTitleBar      = "title bar"
	LabelTerminal      = "terminal area"
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
	f.status.herdr = f.Herdr
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
			f.terminal(c)
			if f.panelOpen {
				ui.Box(c).Label(LabelPanel).Width(theme.PanelWidth).Shrink(0).
					Background(color(theme.BgWindow)).BorderStartWidth(1).BorderColor(color(theme.Line)).
					Children(func() { f.run(c, f.Slots.Panel) })
			}
		})
	})
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
		var strip herdr.TabStrip
		if f.Herdr != nil {
			strip = herdr.TabStripFromTree(f.Herdr.Live().Tree)
		}
		f.bar.setStrip(strip)
		f.bar.send = f.send
		f.bar.build(c, f.panelOpen, f.TogglePanel)
	})
}

// terminal shows herdr's line while herdr is not connected, centred in the
// area, and the terminal slot otherwise.
func (f *Frame) terminal(c *ui.Context) {
	line, down := f.connectionLine()
	ui.Column(c).Label(LabelTerminal).Grow(1).MinWidth(0).Center().
		Background(color(theme.BgTerminal)).Children(func() {
		if down {
			ui.Text(c, line).TextColor(color(theme.Text2))
			return
		}
		f.run(c, f.Slots.Terminal)
	})
}

func (f *Frame) send(cmd TabCommand) {
	if f.Send != nil {
		f.Send(cmd)
	}
}

func panelToggle(c *ui.Context, open bool, onClick func()) {
	toggleButton(c, LabelPanelToggle, open, onClick)
}

// toggleButton draws the sidebar or panel toggle: a small outline with one
// filled part. The sidebar's fill is on the left, the panel's on the right,
// and the fill shows while the part is open.
func toggleButton(c *ui.Context, label string, open bool, onClick func()) {
	left := label != LabelPanelToggle
	ui.ButtonBase(c).Label(label).Size(theme.TitleButtonSize, theme.TitleButtonSize).Shrink(0).
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
	Invalidate()
	Update(fn func())
}

// syncHerdr follows a change of the herdr tree: the board looks at the bridge
// again, and the agents list takes the new rows. UI thread.
func (f *Frame) syncHerdr() {
	if f.Herdr != nil && f.Board != nil {
		f.Board.Sync(f.Herdr.Live().Tree)
	}
	f.syncAgents()
}

// syncBoard follows a change of the board: the outbox reads its report, and
// the agents list takes the new rows. UI thread.
func (f *Frame) syncBoard() {
	if f.Outbox != nil && f.Board != nil {
		f.Outbox.Sync(f.Board.View().Snapshot)
	}
	f.syncAgents()
}

func (f *Frame) syncAgents() {
	if f.AgentsList != nil {
		f.AgentsList.Sync()
	}
}

// Attach connects the frame's state and the herdr connection to the window.
// A change of any of them redraws the window; Visibility follows the window
// and tells herdr. Call it once, on the UI thread, after the window exists.
// The returned function ends the connection and the watching.
func (f *Frame) Attach(ctx context.Context, w Window, target herdr.Target) (stop func()) {
	f.Selection = state.NewSelection(w.Invalidate)
	f.Status = state.NewStatusMessage(w.Invalidate)
	f.Send = newCommander(f.Status.Show).Send
	// An onChange func can run on any goroutine or on the UI thread. It only
	// queues the sync with Window.Update, which is safe from both.
	dir, hasDir := board.Dir()
	f.Board = NewBoard(dir, hasDir, func() { w.Update(f.syncBoard) })
	f.Outbox = NewBoardOutbox(dir, hasDir, w.Invalidate, f.Status.Show)
	h := NewHerdr(ctx, target, w.IsVisible(), func() { w.Update(f.syncHerdr) })
	f.Herdr = h
	f.AgentsList = NewAgentsList(h, f.Board, f.Selection, f.Status)
	f.Slots.Agents = f.AgentsList.View
	f.Board.Sync(h.Live().Tree)
	if f.Host == nil {
		f.Host = host.NewSystem(w.Invalidate)
	}
	if f.Usage == nil {
		f.Usage = usage.NewFileSampler(w.Invalidate)
	}
	f.Visibility = state.NewVisibility(func(visible bool) {
		h.SetVisible(visible)
		f.Host.SetVisible(visible)
		f.Usage.SetVisible(visible)
	})
	// Visibility starts as visible, so a window that starts hidden is the only change that Watch
	// reports. The samplers start hidden: tell them the window state now.
	f.Host.SetVisible(w.IsVisible())
	f.Usage.SetVisible(w.IsVisible())
	unwatch := f.Visibility.Watch(w)
	return func() {
		unwatch()
		// Held actions are written now, so quit loses none.
		f.Outbox.ReleaseForQuit()
		f.Outbox.Wait()
		h.Close()
		f.Host.Close()
		f.Usage.Close()
	}
}
