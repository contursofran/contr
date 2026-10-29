// Package icons draws the ten status icons of docs/DESIGN.md: one set for
// the sidebar, tabs, board stages and needs-you rows.
//
// Ported from the Rust contr status_icon module.
package icons

import (
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
)

// Status is one of the ten icons.
type Status int

// The statuses, in legend order (the table order of docs/DESIGN.md).
const (
	NeedsYou Status = iota
	Decide
	Do
	Blocked
	Working
	Waiting
	Idle
	Done
	Fyi
	NotStarted
)

// All lists the statuses in legend order.
var All = [...]Status{
	NeedsYou, Decide, Do, Blocked, Working, Waiting, Idle, Done, Fyi, NotStarted,
}

var labels = [...]string{
	"needs you", "decide", "do", "blocked", "working",
	"waiting", "idle", "done", "fyi", "not started",
}

var slugs = [...]string{
	"needs-you", "decide", "do", "blocked", "working",
	"waiting", "idle", "done", "fyi", "not-started",
}

// Label is the accessible label, and the legend text.
func (s Status) Label() string { return labels[s] }

// Slug is the kebab-case id for keys and test selectors.
func (s Status) Slug() string { return slugs[s] }

// glyph returns the file name and color of the static glyph. Working returns
// its ring: the arc turns on top of it.
func (s Status) glyph() (file string, color uint32) {
	switch s {
	case NeedsYou:
		return "needs_you", theme.IconYou
	case Decide:
		return "decide", theme.IconYou
	case Do:
		return "do", theme.IconYou
	case Blocked:
		return "blocked", theme.IconYou
	case Working:
		return "working_ring", theme.IconFaint
	case Waiting:
		return "waiting", theme.IconGrey
	case Idle:
		return "idle", theme.IconGrey
	case Done:
		return "done", theme.IconGrey
	case Fyi:
		return "fyi", theme.IconGrey
	default:
		return "not_started", theme.IconFaint
	}
}

var (
	loadOnce sync.Once
	parsed   map[string]*ui.SVG
)

// svg returns an embedded icon. A file that does not parse is logged and
// draws nothing; the tests make sure that all of them parse.
func svg(name string) *ui.SVG {
	loadOnce.Do(func() {
		parsed = make(map[string]*ui.SVG)
		files, err := assets.Icons.ReadDir("icons")
		if err != nil {
			log.Printf("icons: %v", err)
			return
		}
		for _, f := range files {
			base := f.Name()[:len(f.Name())-len(".svg")]
			data, err := assets.Icons.ReadFile("icons/" + f.Name())
			if err != nil {
				log.Printf("icons: %v", err)
				continue
			}
			s, err := ui.ParseSVG(data)
			if err != nil {
				log.Printf("icons: %s: %v", f.Name(), err)
				continue
			}
			parsed[base] = s
		}
	})
	return parsed[name]
}

// spinSteps is how many positions the working arc has in one turn.
const spinSteps = int64(theme.SpinPeriod) * theme.SpinFPS / int64(time.Second)

// SpinAngle is the turn of the working arc, in degrees, at time t. One turn
// takes theme.SpinPeriod, in steps of 1/theme.SpinFPS seconds: the arc
// paints at that rate, so each paint shows a new step.
func SpinAngle(t time.Time) float32 {
	phase := t.UnixNano() % int64(theme.SpinPeriod)
	step := phase * theme.SpinFPS / int64(time.Second)
	return 360 * float32(step) / float32(spinSteps)
}

// Icon shows a status icon at theme.IconSize.
func Icon(c *ui.Context, s Status) ui.Element { return Sized(c, s, theme.IconSize) }

// Sized shows a status icon at size DIPs (12 to 14).
//
// A working icon draws itself: it turns from the frame time and asks for
// frames only while it is visible, at theme.SpinFPS. MyGo paints those
// frames from the last build without running any view, so the turn costs
// painting only, and no other row repaints (see ui/drawing.md,
// "Animation"). Each frame still costs the process CPU, so the arc does not
// ask for the display's rate (AnimationFrame).
func Sized(c *ui.Context, s Status, size float32) ui.Element {
	file, color := s.glyph()
	if svg(file) == nil || (s == Working && svg("working_arc") == nil) {
		return ui.Box(c).Size(size, size).Label(s.Label()) // file missing, logged
	}
	if s != Working {
		return ui.Icon(c, svg(file)).Size(size, size).TextColor(theme.Color(color)).Label(s.Label())
	}
	ring, arc := svg(file), svg("working_arc")
	return ui.Box(c).Size(size, size).Label(s.Label()).Draw(func(p *ui.Painter, r ui.Rect) {
		p.AnimationFrameRate(theme.SpinFPS)
		p.Icon(ring, r, theme.Color(color))
		p.Transform(ui.Rotation(SpinAngle(p.Now())), r.X+r.W/2, r.Y+r.H/2, func() {
			p.Icon(arc, r, theme.Color(theme.IconYou))
		})
	})
}

// FromAgentState returns the icon for an agent's state. Unknown shows as
// idle.
func FromAgentState(s herdr.AgentState) Status {
	switch s {
	case herdr.StateWorking:
		return Working
	case herdr.StateBlocked:
		return Blocked
	case herdr.StateDone:
		return Done
	}
	return Idle
}
