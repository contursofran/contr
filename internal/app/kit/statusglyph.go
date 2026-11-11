package kit

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

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

// Moves reports whether a dot glyph of this state changes with time.
// Idle and needs you are still and use no timer.
func (s GlyphState) Moves() bool {
	return s == GlyphWorking || s == GlyphWorkingSlow || s == GlyphWaiting
}

// Times of the motion, from docs/spec/StatusGlyph.md.
const (
	// WorkingStep is the time of one dot frame while working.
	WorkingStep = 90 * time.Millisecond
	// WorkingSlowStep is the time of one frame while working slow: a cycle
	// of 1.4s over ten frames.
	WorkingSlowStep = 140 * time.Millisecond
	// WaitingCycle is one fade of the waiting glyph, from .25 to 1 and back.
	WaitingCycle = 1600 * time.Millisecond
	// ClaudeHop is one hop of the Claude mascot while working.
	ClaudeHop = 800 * time.Millisecond
	// ClaudeHopHeight is how high the Claude mascot hops, in DIPs.
	ClaudeHopHeight = 1.5
)

// Opacities from the spec.
const (
	WaitingMinOpacity = 0.25
	ClaudeIdleOpacity = 0.45
)

// BrailleFrames is the spinner, one frame every WorkingStep. The braille
// characters name the dot patterns that the glyph draws (Dots).
var BrailleFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// The still glyphs: idle and waiting. Needs you shows the first spinner frame.
const (
	BrailleIdle    = "⠶"
	BrailleWaiting = "⠿"
)

// The frame rate of a fade and of the hop of the Claude mascot: smooth enough
// at 30 frames a second.
const smoothFPS = 30

// step is the time of one dot frame of a state; 0 for a state that does
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

// Braille returns the pattern a state shows at time t, as a braille
// character. With still (Reduce
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

// BrailleOpacity is the opacity of a dot glyph at time t: waiting fades
// between WaitingMinOpacity and 1 over WaitingCycle; the others are opaque.
func BrailleOpacity(s GlyphState, t time.Time, still bool) float32 {
	if s != GlyphWaiting || still {
		return 1
	}
	phase := float64(t.UnixNano()%int64(WaitingCycle)) / float64(WaitingCycle)
	wave := 0.5 - 0.5*math.Cos(2*math.Pi*phase) // 0 → 1 → 0
	return WaitingMinOpacity + (1-WaitingMinOpacity)*float32(wave)
}

// ClaudeHopOffset is how far the Claude mascot is above its place at time t,
// in DIPs: one hop up to ClaudeHopHeight and back in ClaudeHop while
// working, and 0 otherwise (the spec has no motion for working slow and
// waiting). With still (Reduce Motion) it does not hop.
func ClaudeHopOffset(s GlyphState, t time.Time, still bool) float32 {
	if s != GlyphWorking || still {
		return 0
	}
	phase := float64(t.UnixNano()%int64(ClaudeHop)) / float64(ClaudeHop)
	return ClaudeHopHeight * float32(math.Sin(math.Pi*phase))
}

// ClaudeOpacity is the opacity of the Claude mascot: ClaudeIdleOpacity when
// idle, 1 otherwise.
func ClaudeOpacity(s GlyphState) float32 {
	if s == GlyphIdle {
		return ClaudeIdleOpacity
	}
	return 1
}

// Runs reports whether a glyph of this state asks for repaints: a dot
// glyph that moves, or the Claude mascot while it works, and not under Reduce
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
	// Claude draws the Claude Code mascot, for a Claude session, in place of
	// the dot spinner.
	Claude bool
	// Grey draws the Claude mascot in ink-48: a Claude session that does not
	// orchestrate (D191). The claude hue stays for orchestrators.
	Grey bool
	// Small draws the 14px glyph of tabs and the notch; else 16px (rows).
	Small bool
	// Color draws the lit dots in this ink in place of the state's ink.
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
// the lit dots of an idle glyph, claude for the Claude mascot, ink-100 otherwise. The
// fade and the idle opacity of the Claude mascot are not in it.
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

// optsColor is GlyphColor, with a grey Claude mascot for a Claude session that
// does not orchestrate (Grey, D191). Needs you keeps its hue.
func optsColor(p *theme.Palette, o GlyphOpts) ui.Color {
	if o.Claude && o.Grey && o.State != GlyphNeedsYou {
		return p.Ink48
	}
	return GlyphColor(p, o.State, o.Claude)
}

// paints counts the paints of glyphs. The tests read it to see that a glyph
// that is still, off screen or under Reduce Motion is not painted again.
var paints atomic.Int64

// Dots returns the lit dots of a state at time t, one bit for each dot of the
// 2×3 grid: bits 0 to 2 are the left column from the top, bits 3 to 5 the
// right column, as dots 1 to 6 of a braille cell. The braille frames above
// name the patterns.
func Dots(s GlyphState, t time.Time, still bool) uint8 {
	for _, r := range Braille(s, t, still) {
		return uint8(r - 0x2800)
	}
	return 0
}

// The dot grid in the 16px box, from docs/spec/StatusGlyph.md: dots of 3px,
// centres 5px apart, the grid centred in the box. The 14px box scales it.
const (
	DotSize  = 3
	dotPitch = 5
)

// dotRect is the rect of dot i (0 to 5) in the glyph box r.
func dotRect(r ui.Rect, i int) ui.Rect {
	k := r.W / 16
	d := DotSize * k
	cx := r.X + r.W/2 + float32(i/3*2-1)*dotPitch/2*k
	cy := r.Y + r.H/2 + float32(i%3-1)*dotPitch*k
	return ui.Rect{X: cx - d/2, Y: cy - d/2, W: d, H: d}
}

// StatusGlyph shows the state of one agent as one mark: a grid of six dots,
// or the Claude Code mascot for a Claude session (docs/spec/StatusGlyph.md).
//
// The dots are drawn shapes, not a font glyph: a lit dot takes the state's
// ink and a dim dot ink-30, so every state shows all six dots.
//
// The glyph paints itself from the frame time in Draw. The timer is a repaint
// request (Painter.After or AnimationFrameRate) made only for a state that
// moves, only while the glyph paints, so it stops by itself off screen, in a
// hidden window, and for idle and needs you. Reduce Motion makes every glyph
// still. A frame that only paints does not build the view, so the glyph costs
// painting only.
//
// MyGo's Transition and AnimatePose go to a target and stop; the hop of the
// Claude mascot repeats, so it draws from Draw as the working icon of
// internal/app/icons does. The hop only moves the mascot: its mask is
// rasterized once per size, so a frame places it again and draws nothing new.
func StatusGlyph(c *ui.Context, o GlyphOpts) ui.Element {
	pal := theme.Current()
	state := o.State
	still := c.Preferences().ReduceMotion
	runs := Runs(state, o.Claude, still)
	color := optsColor(pal, o)
	if o.Color != (ui.Color{}) && !o.Claude {
		color = o.Color
	}
	size := GlyphSize(o.Small)
	box := ui.Box(c).Size(size, size).Shrink(0).Label(state.Label())

	if o.Claude {
		mark := ClaudeMascot()
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
			p.Icon(mark, mascotRect(r, ClaudeHopOffset(state, p.Now(), still), p.Scale()), col)
		})
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
		alpha := BrailleOpacity(state, now, still)
		lit, dim := color.Alpha(alpha), pal.Ink30.Alpha(alpha)
		dots := Dots(state, now, still)
		for i := 0; i < 6; i++ {
			col := dim
			if dots&(1<<i) != 0 {
				col = lit
			}
			d := dotRect(r, i)
			p.Fill(d, col, d.W/2)
		}
	})
}
