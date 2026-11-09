package app

// The worker view of the right panel. While a worker is selected in the
// sidebar, the panel shows that worker: its name, role, issue, worktree and
// status, the last lines of its terminal in a code view, and Interrupt and
// Stop. Stop asks for a second click. The terminal is read off the UI thread,
// when the worker is selected and when the agent state changes (#162, S5).

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the worker view.
const (
	LabelWorkerPanel  = "worker panel"
	LabelWorkerOutput = "worker output"
	LabelWorkerGone   = "worker gone"
	WorkerInterrupt   = "Interrupt"
	WorkerStop        = "Stop"
	WorkerConfirmStop = "Stop? Click again"
	workerLines       = 40
)

// WorkerPanel is the worker view. Its methods run on the UI thread.
type WorkerPanel struct {
	// Entry opens the worker's pull request or issue (D106).
	Entry     WorkerEntry
	Source    AgentSource
	Selection *state.Selection
	// Env runs the hc calls off the UI thread, redraws and shows messages.
	Env Env

	addr  string // the worker that out belongs to
	out   string
	err   string
	code  *editor.DiffState
	busy  bool // a read is running
	again bool // the state changed during a read
	armed string
}

// Selected returns the selected worker, or false when none is (an
// orchestrator or nothing is selected, or the worker is gone from the state).
func (p *WorkerPanel) Selected() (agents.Worker, bool) {
	if p.Selection == nil || p.Source == nil {
		return agents.Worker{}, false
	}
	sel, ok := p.Selection.Current()
	if !ok || sel.Kind != state.KindWorker {
		return agents.Worker{}, false
	}
	for _, w := range p.Source.Snapshot().Workers {
		if w.Address == sel.ID {
			return w, true
		}
	}
	return agents.Worker{}, false
}

// Following tells whether the panel shows a worker: one is selected.
func (p *WorkerPanel) Following() bool {
	if p.Selection == nil {
		return false
	}
	sel, ok := p.Selection.Current()
	return ok && sel.Kind == state.KindWorker
}

// Sync reads the selected worker's terminal again. The frame calls it when the
// agent state changes.
func (p *WorkerPanel) Sync() {
	if w, ok := p.Selected(); ok {
		p.read(w.Address)
	}
}

func (p *WorkerPanel) read(addr string) {
	if p.busy {
		p.again = true
		return
	}
	p.busy = true
	p.Env.Do(func() func() {
		text, err := p.Source.Read(addr, workerLines)
		return func() {
			p.busy = false
			if p.addr == addr {
				p.out, p.err = text, ""
				if err != nil {
					p.err = err.Error()
				}
				p.code = kit.NewCode("", p.out, p.out)
			}
			if p.again {
				p.again = false
				p.Sync()
			}
			p.Env.Redraw()
		}
	})
}

// follow switches to the worker at addr: it forgets the old output and reads.
func (p *WorkerPanel) follow(addr string) {
	if p.addr == addr {
		return
	}
	p.addr, p.out, p.err, p.code, p.armed = addr, "", "", nil, ""
	p.read(addr)
}

// interrupt sends the interrupt key to the worker.
func (p *WorkerPanel) interrupt(w agents.Worker) {
	p.armed = ""
	p.act(w, "interrupted", p.Source.Interrupt)
}

// stop closes the worker's terminal. The first click arms the button; the
// second one stops.
func (p *WorkerPanel) stop(w agents.Worker) {
	if p.armed != w.Address {
		p.armed = w.Address
		p.Env.Redraw()
		return
	}
	p.armed = ""
	p.act(w, "stopped", p.Source.Stop)
}

func (p *WorkerPanel) act(w agents.Worker, done string, do func(string) error) {
	p.Env.Do(func() func() {
		err := do(w.Address)
		return func() {
			switch {
			case err != nil:
				p.err = err.Error()
			default:
				p.Env.Show(w.Name + " " + done)
			}
			p.Env.Redraw()
		}
	})
}

// View builds the worker view.
func (p *WorkerPanel) View(c *ui.Context) {
	w, ok := p.Selected()
	ui.Column(c).Label(LabelWorkerPanel).Fill().Children(func() {
		if !ok {
			TextLine(c, LabelWorkerGone, "worker gone", theme.Current().Ink50)
			return
		}
		p.follow(w.Address)
		ui.Row(c).FillWidth().Height(theme.PanelHeaderHeight).Shrink(0).AlignItems(ui.Center).
			Gap(theme.NeedGap).PaddingX(theme.PanelPadX).Children(func() {
			icons.Sized(c, workerIcon(w), theme.IconSizeSmall).Shrink(0)
			ui.Text(c, w.Name).Bold().SingleLine().Grow(1).MinWidth(0)
			p.button(c, WorkerInterrupt, ButtonOutlined, func() { p.interrupt(w) })
			label, style := WorkerStop, ButtonOutlined
			if p.armed == w.Address {
				label, style = WorkerConfirmStop, ButtonFilled
			}
			p.button(c, label, style, func() { p.stop(w) })
		})
		for _, line := range workerFacts(w) {
			TextLine(c, "worker fact", line, theme.Current().Ink62)
		}
		if label, open := p.Entry.For(w); open != nil {
			ui.Row(c).FillWidth().Shrink(0).PaddingX(theme.PanelPadX).Children(func() {
				p.button(c, label, ButtonOutlined, open)
			})
		}
		if p.err != "" {
			TextLine(c, "worker error", p.err, theme.Current().Ink50)
		}
		ui.Column(c.Key("worker code")).Label(LabelWorkerOutput).FillWidth().Grow(1).MinHeight(0).Children(func() {
			if p.code != nil {
				kit.CodeView(c, p.code).Fill()
			}
		})
	})
}

func (p *WorkerPanel) button(c *ui.Context, label string, style ButtonStyle, click func()) {
	cardButton(c, label, style, theme.Current().Ink95, click)
}

// workerFacts are the lines under the name: role and issue, worktree, status.
func workerFacts(w agents.Worker) []string {
	line := w.Role
	if w.Issue != "" {
		line += " #" + w.Issue
	}
	status := w.Status
	if !w.Live {
		status = "ended"
		if w.ExitCode != nil {
			status += " " + strconv.Itoa(*w.ExitCode)
		}
	}
	if status == "" {
		status = "no status"
	}
	if w.Msg != "" {
		status += ": " + w.Msg
	}
	facts := []string{line}
	if w.Worktree != "" {
		facts = append(facts, w.Worktree)
	}
	return append(facts, status)
}
