package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion tokens of docs/spec/tokens.md and docs/spec/Motion.md (D256): how
// long a short transition takes. All ease out (MotionEase). Views use these
// names, never a number.
const (
	// MotionSelect: the fill and inks of a row or tab that is selected.
	MotionSelect = 120 * time.Millisecond
	// MotionPopIn and MotionPopOut: a popover or a modal comes in and goes.
	MotionPopIn  = 140 * time.Millisecond
	MotionPopOut = 100 * time.Millisecond
	// MotionState: the inks of a worker row that turns to needs you.
	MotionState = 300 * time.Millisecond
	// MotionNumber: the digits of a value roll.
	MotionNumber = 200 * time.Millisecond
	// MotionMeter: the fill of a meter moves to its new width.
	MotionMeter = 300 * time.Millisecond
)

// MotionPopScale is the scale a popover or modal comes in from and goes to.
const MotionPopScale = 0.96

// MotionEase is the easing of every motion token.
var MotionEase ui.Easing = ui.EaseOut
