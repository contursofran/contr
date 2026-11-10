package kit

// Short transitions (D256, docs/spec/Motion.md): a selected row or tab fades
// its fill and inks, a popover or a modal scales in from 0.96 with a fade, a
// worker row that turns to needs you fades its inks, and numbers and meters
// roll and move. All are MyGo's own motion (ElementTransition, AnimateColor,
// AnimatePose, ContentTransition, AnimateWith). Each one ends, and after its
// end no frame is asked (D55). With Reduce Motion MyGo moves and scales
// nothing: positions, sizes and scales change at once, a fade stays, and a
// rolled number cross-fades.

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// FadeTransition fades the fill, the border and the rings of an element over
// d: theme.MotionSelect for a selection, theme.MotionState for a row that
// turns to needs you. Positions and sizes are not animated.
func FadeTransition(d time.Duration) ui.ElementTransition {
	return ui.ElementTransition{Colors: true, Shadows: true, Duration: d, Ease: theme.MotionEase}
}

// FadeInk returns the ink of e at this frame, on its way to ink over d. A
// color is not motion, so it fades with Reduce Motion too.
func FadeInk(e ui.Element, key string, ink ui.Color, d time.Duration) ui.Color {
	return e.AnimateColor(key, ink, ui.Animation{Duration: d, Ease: theme.MotionEase})
}

// RowMotion is the duration of a change of row e at this frame:
// theme.MotionState when needs (the row needs the owner) changed since the
// last frame, else theme.MotionSelect. Call it once per build of e. The
// duration counts only in the frame that changes a color: a fade goes on
// with the duration it started with.
func RowMotion(e ui.Element, needs bool) time.Duration {
	last := ui.Local(e, "motion-needs", func() bool { return needs })
	d := theme.MotionSelect
	if *last != needs {
		d = theme.MotionState
	}
	*last = needs
	return d
}

// popTransition is the way in and out of a popover panel: from 0.96 with a
// fade in theme.MotionPopIn, back in theme.MotionPopOut.
var popTransition = ui.ElementTransition{
	Duration:     theme.MotionPopIn,
	Ease:         theme.MotionEase,
	ExitDuration: theme.MotionPopOut,
	ExitEase:     theme.MotionEase,
	Enter:        &ui.Motion{Scale: theme.MotionPopScale},
	Exit:         &ui.Motion{Scale: theme.MotionPopScale},
}

// PopIn gives the panel of a PopoverBase its way in and out. The panel scales
// from the side of its anchor (AttachedOrigin).
func PopIn(panel ui.Element) ui.Element {
	return panel.AttachedOrigin().Transition(popTransition)
}

// PopDialog gives a DialogBase its way in and out. MyGo builds the panel in a
// backdrop that is new when the dialog opens, so the panel has no Enter of
// its own: the backdrop fades in and out (with the panel in it), and the
// panel scales from 0.96 with a pose in its first frames. It goes with the
// fade alone.
func PopDialog(c *ui.Context, backdrop, panel ui.Element) {
	backdrop.Transition(ui.ElementTransition{
		Duration: theme.MotionPopIn, Ease: theme.MotionEase,
		ExitDuration: theme.MotionPopOut, ExitEase: theme.MotionEase,
		Enter: &ui.Motion{}, Exit: &ui.Motion{},
	})
	entered := ui.Local(panel, "pop-entered", func() bool { return false })
	pose := ui.Pose{}
	if !*entered {
		pose, *entered = ui.Pose{Scale: theme.MotionPopScale}, true
		c.Invalidate()
	}
	panel.AnimatePose(pose, ui.Animation{Duration: theme.MotionPopIn, Ease: theme.MotionEase})
}

// RollNumber rolls the characters of text e that change, up when value goes
// up and down when it goes down, in theme.MotionNumber. With Reduce Motion
// the text cross-fades.
func RollNumber(e ui.Element, value float64) ui.Element {
	r := ui.Local(e, "roll", func() roll { return roll{value: value} })
	if value != r.value {
		r.down, r.value = value < r.value, value
	}
	return e.ContentTransition(ui.ContentTransition{Numeric: true, Down: r.down,
		Animation: &ui.Animation{Duration: theme.MotionNumber, Ease: theme.MotionEase}})
}

// roll is the last value of a rolled number and the way it last rolled.
type roll struct {
	value float64
	down  bool
}

// MeterFill returns the filled part of meter e at this frame, on its way to
// value in theme.MotionMeter. A width is motion: with Reduce Motion it is
// value at once.
func MeterFill(e ui.Element, value float64) float64 {
	return float64(e.AnimateWith("meter-fill", float32(value), theme.MotionMeter, theme.MotionEase))
}
