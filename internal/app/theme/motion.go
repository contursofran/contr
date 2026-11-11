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

// Motion tokens of the second tier (D263): what moves and changes place.
// All ease out (MotionEase), except a panel (MotionPanelEase) and the
// questions card that closes into the notch (MotionNotchCloseEase).
const (
	// MotionTab: the pill of the selected tab slides to the tab selected now.
	MotionTab = 180 * time.Millisecond
	// MotionPanel: the sidebar or the files panel comes in as it shows.
	MotionPanel = 220 * time.Millisecond
	// MotionNotchOpen: the questions card grows from the notch.
	MotionNotchOpen = 260 * time.Millisecond
	// MotionNotchClose: the notch shrinks back from the card.
	MotionNotchClose = 160 * time.Millisecond
	// MotionRowIn: an agent row that arrives fades and moves in.
	MotionRowIn = 200 * time.Millisecond
)

// Distances of the second tier (D263), in DIPs: a panel that shows comes in
// from MotionPanelShift toward its outer edge; an agent row that arrives
// comes down from MotionRowShift above its place.
const (
	MotionPanelShift = 24
	MotionRowShift   = 4
)

// MotionPopScale is the scale a popover or modal comes in from and goes to.
const MotionPopScale = 0.96

// MotionEase is the easing of every motion token but the two below.
var MotionEase ui.Easing = ui.EaseOut

// MotionPanelEase is the easing of MotionPanel: cubic-bezier(.2, 0, 0, 1).
var MotionPanelEase = ui.CubicBezier(0.2, 0, 0, 1)

// MotionNotchCloseEase is the easing of MotionNotchClose.
var MotionNotchCloseEase ui.Easing = ui.EaseIn
