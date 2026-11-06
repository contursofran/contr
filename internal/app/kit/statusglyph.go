package kit

import (
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/theme"
)

// GlyphState is the state a StatusGlyph shows (docs/spec/StatusGlyph.md).
// It comes from the agent state (D35). NeedsYou wins over every other state:
// the caller picks it first.
type GlyphState int

// The states of a StatusGlyph.
const (
	GlyphIdle        GlyphState = iota // nothing to do
	GlyphWorking                       // the agent runs
	GlyphWorkingSlow                   // no output for a while
	GlyphWaiting                       // waits for CI or another agent
	GlyphNeedsYou                      // asks the owner, or is stuck at a prompt
)

// Label is the screen-reader label of the state.
func (s GlyphState) Label() string {
	switch s {
	case GlyphWorking, GlyphWorkingSlow:
		return "working"
	case GlyphWaiting:
		return "waiting"
	case GlyphNeedsYou:
		return "needs you"
	default:
		return "idle"
	}
}

// Moves reports whether a braille glyph of this state changes with time.
// Idle and needs you are still and use no timer.
func (s GlyphState) Moves() bool {
	return s == GlyphWorking || s == GlyphWorkingSlow || s == GlyphWaiting
}

// Times of the motion, from docs/spec/StatusGlyph.md.
const (
	// WorkingStep is the time of one braille frame while working.
	WorkingStep = 90 * time.Millisecond
	// WorkingSlowStep is the time of one frame while working slow: a cycle
	// of 1.4s over ten frames.
	WorkingSlowStep = 140 * time.Millisecond
	// WaitingCycle is one fade of the waiting glyph, from .25 to 1 and back.
	WaitingCycle = 1600 * time.Millisecond
	// ClaudeTurn is one turn of the Claude glyph while working.
	ClaudeTurn = 2400 * time.Millisecond
)

// Opacities from the spec.
const (
	WaitingMinOpacity = 0.25
	ClaudeIdleOpacity = 0.45
)

// BrailleFrames is the spinner, one frame every WorkingStep.
var BrailleFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// The still glyphs: idle and waiting. Needs you shows the first spinner frame.
const (
	BrailleIdle    = "⠶"
	BrailleWaiting = "⠿"
)

// The frame rate of a fade and of the turn of the Claude mark: smooth enough
// at 30 frames a second.
const smoothFPS = 30

// step is the time of one braille frame of a state; 0 for a state that does
// not step.
func step(s GlyphState) time.Duration {
	switch s {
	case GlyphWorking:
		return WorkingStep
	case GlyphWorkingSlow:
		return WorkingSlowStep
	}
	return 0
}

// Braille returns the glyph a state shows at time t. With still (Reduce
// Motion) every state is still: working shows ⠋ and waiting shows ⠿.
func Braille(s GlyphState, t time.Time, still bool) string {
	switch s {
	case GlyphWorking, GlyphWorkingSlow:
		if still {
			return BrailleFrames[0]
		}
		n := t.UnixNano() / int64(step(s))
		return BrailleFrames[n%int64(len(BrailleFrames))]
	case GlyphWaiting:
		return BrailleWaiting
	case GlyphNeedsYou:
		return BrailleFrames[0]
	default:
		return BrailleIdle
	}
}

// BrailleOpacity is the opacity of a braille glyph at time t: waiting fades
// between WaitingMinOpacity and 1 over WaitingCycle; the others are opaque.
func BrailleOpacity(s GlyphState, t time.Time, still bool) float32 {
	if s != GlyphWaiting || still {
		return 1
	}
	phase := float64(t.UnixNano()%int64(WaitingCycle)) / float64(WaitingCycle)
	wave := 0.5 - 0.5*math.Cos(2*math.Pi*phase) // 0 → 1 → 0
	return WaitingMinOpacity + (1-WaitingMinOpacity)*float32(wave)
}

// ClaudeAngle is the turn of the Claude glyph in degrees at time t: one turn
// in ClaudeTurn while working, and still otherwise (the spec has no motion
// for working slow and waiting). With still (Reduce Motion) it does not turn.
func ClaudeAngle(s GlyphState, t time.Time, still bool) float32 {
	if s != GlyphWorking || still {
		return 0
	}
	return 360 * float32(t.UnixNano()%int64(ClaudeTurn)) / float32(ClaudeTurn)
}

// ClaudeOpacity is the opacity of the Claude glyph: ClaudeIdleOpacity when
// idle, 1 otherwise.
func ClaudeOpacity(s GlyphState) float32 {
	if s == GlyphIdle {
		return ClaudeIdleOpacity
	}
	return 1
}

// Runs reports whether a glyph of this state asks for repaints: a braille
// glyph that moves, or the Claude mark while it works, and not under Reduce
// Motion.
func Runs(s GlyphState, claude, still bool) bool {
	if still {
		return false
	}
	if claude {
		return s == GlyphWorking
	}
	return s.Moves()
}

// GlyphOpts describes a StatusGlyph.
type GlyphOpts struct {
	State GlyphState
	// Claude draws the Claude mark, for an orchestrator, in place of the
	// braille spinner.
	Claude bool
	// Small draws the 14px glyph of tabs and the notch; else 16px (rows).
	Small bool
	// Color draws a braille glyph in this ink in place of the state's ink.
	// The zero value keeps GlyphColor.
	Color ui.Color
}

// GlyphSize is the width of a StatusGlyph in DIPs: 16, or 14 when small.
func GlyphSize(small bool) float32 {
	if small {
		return 14
	}
	return 16
}

// GlyphColor is the token color of a state: blocked for needs you, ink-30 for
// an idle braille glyph, claude for the Claude mark, ink-100 otherwise. The
// fade and the idle opacity of the Claude mark are not in it.
func GlyphColor(p *theme.Palette, s GlyphState, claude bool) ui.Color {
	switch {
	case s == GlyphNeedsYou:
		return p.Blocked
	case claude:
		return p.Claude
	case s == GlyphIdle:
		return p.Ink30
	default:
		return p.Ink100
	}
}

var (
	claudeOnce sync.Once
	claudeSVG  *ui.SVG
)

// ClaudeMark returns the Claude mark, assets/icons/claude.svg. A file that
// does not parse is logged and gives nil; the tests make sure it parses.
func ClaudeMark() *ui.SVG {
	claudeOnce.Do(func() {
		data, err := assets.Icons.ReadFile("icons/claude.svg")
		if err != nil {
			log.Printf("kit: claude mark: %v", err)
			return
		}
		claudeSVG, err = ui.ParseSVG(data)
		if err != nil {
			log.Printf("kit: claude mark: %v", err)
		}
	})
	return claudeSVG
}

// paints counts the paints of glyphs. The tests read it to see that a glyph
// that is still, off screen or under Reduce Motion is not painted again.
var paints atomic.Int64

type layoutKey struct {
	size  float32
	glyph string
}

var layouts sync.Map // layoutKey → *ui.TextLayout

// brailleLayout returns the shaped glyph, kept for the next paint.
func brailleLayout(size float32, glyph string) *ui.TextLayout {
	k := layoutKey{size, glyph}
	if l, ok := layouts.Load(k); ok {
		return l.(*ui.TextLayout)
	}
	l := ui.ShapeText(glyph, ui.Font{Family: theme.FontMono, Size: size, Weight: theme.StyleGlyph.Weight}, 0)
	layouts.Store(k, l)
	return l
}

// StatusGlyph shows the state of one agent as one mark: a braille spinner,
// or the Claude mark for an orchestrator (docs/spec/StatusGlyph.md).
//
// The glyph paints itself from the frame time in Draw. The timer is a repaint
// request (Painter.After or AnimationFrameRate) made only for a state that
// moves, only while the glyph paints, so it stops by itself off screen, in a
// hidden window, and for idle and needs you. Reduce Motion makes every glyph
// still. A frame that only paints does not build the view, so the glyph costs
// painting only.
//
// MyGo's Transition and AnimatePose go to a target and stop; the turn of the
// Claude mark repeats, so it draws from Draw as the working icon of
// internal/app/icons does.
func StatusGlyph(c *ui.Context, o GlyphOpts) ui.Element {
	pal := theme.Current()
	state := o.State
	still := c.Preferences().ReduceMotion
	runs := Runs(state, o.Claude, still)
	color := GlyphColor(pal, state, o.Claude)
	if o.Color != (ui.Color{}) && !o.Claude {
		color = o.Color
	}
	size := GlyphSize(o.Small)
	box := ui.Box(c).Size(size, size).Shrink(0).Label(state.Label())

	if o.Claude {
		mark := ClaudeMark()
		if mark == nil {
			return box // file missing, logged
		}
		return box.Draw(func(p *ui.Painter, r ui.Rect) {
			paints.Add(1)
			if runs {
				p.AnimationFrameRate(smoothFPS)
			}
			col := color
			if state != GlyphNeedsYou {
				col = col.Alpha(ClaudeOpacity(state))
			}
			angle := ClaudeAngle(state, p.Now(), still)
			p.Transform(ui.Rotation(angle), r.X+r.W/2, r.Y+r.H/2, func() {
				p.Icon(mark, r, col)
			})
		})
	}

	fontSize := theme.StyleGlyph.Size
	if o.Small {
		fontSize = 14
	}
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		paints.Add(1)
		now := p.Now()
		if runs {
			if d := step(state); d > 0 {
				p.After(d - time.Duration(now.UnixNano())%d) // the next frame
			} else {
				p.AnimationFrameRate(smoothFPS)
			}
		}
		l := brailleLayout(fontSize, Braille(state, now, still))
		w, h := l.Size()
		p.TextLayout(l, r.X+(r.W-w)/2, r.Y+(r.H-h)/2, color.Alpha(BrailleOpacity(state, now, still)))
	})
}
