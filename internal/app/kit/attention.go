package kit

// Attention on arrival (D240): when the number of open items goes up, the
// questions notch springs, glows in `blocked` and pulses its dot for 10 s; the
// item badge of an orchestrator row springs and glows. Then both are still, so
// no frame is asked while items only wait (D55). With Reduce Motion nothing
// moves: the still end state shows at once.
//
// The motion is MyGo's Keyframes on an element that stays while the effect
// plays (the notch button, the badge). The view asks for it only while the
// effect plays: after the end no Keyframes call is made, so nothing asks for a
// frame.

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Arrival is the last arrival of a key: Run counts the arrivals, At is when
// the last one came. The zero Arrival is none.
type Arrival struct {
	Run uint64
	At  time.Time
}

// Arrivals remembers the count of open items of each key (a project, an
// orchestrator) and tells when it goes up. Its methods run on the UI thread.
type Arrivals struct {
	m map[string]*arrivalState
}

type arrivalState struct {
	count int
	Arrival
}

// Observe records that key has n open items at now and returns its arrival. A
// count higher than the last one is a new arrival, unless start is false: then
// only the count is kept (the owner already looks at the items). A count of 0
// forgets the key, so the next item is an arrival again.
func (a *Arrivals) Observe(key string, n int, now time.Time, start bool) Arrival {
	if n <= 0 {
		delete(a.m, key)
		return Arrival{}
	}
	if a.m == nil {
		a.m = map[string]*arrivalState{}
	}
	s := a.m[key]
	if s == nil {
		s = &arrivalState{}
		a.m[key] = s
	}
	if n > s.count && start {
		s.Run++
		s.At = now
	}
	s.count = n
	return s.Arrival
}

// AttentionLook is the effect at one frame. The still end state is Scale 1,
// Glow 0 and Pulse 0.
type AttentionLook struct {
	// Scale is the scale of the notch or the badge.
	Scale float32
	// Glow is the strength of the `blocked` glow, 0 to 1.
	Glow float32
	// Pulse dims and shrinks the notch dot: 0 is solid, 1 is the dim end of a
	// pulse (opacity 0.3, scale 0.8).
	Pulse float32
}

// Still is the end state of the effect.
var Still = AttentionLook{Scale: 1}

// Timings of the effect, from the owner's mockups (#631).
const (
	// AttentionBump is how long the spring of the notch and the badge takes.
	AttentionBump = 400 * time.Millisecond
	// AttentionGlow is how long the glow takes: up in 80 ms, out in 2 s.
	AttentionGlow = 2080 * time.Millisecond
	// PulseCycle is one pulse of the notch dot; PulseCount pulses follow the
	// spring.
	PulseCycle = 2 * time.Second
	PulseCount = 5
	// NotchAttentionLength and BadgeAttentionLength are the whole effects.
	NotchAttentionLength = AttentionBump + PulseCount*PulseCycle
	BadgeAttentionLength = AttentionGlow
)

// Peaks of the spring: the notch grows to 1.06, the badge to 1.25, and each
// goes a little under 1 before it settles.
const (
	notchPeak, notchDip = 1.06, 0.985
	badgePeak, badgeDip = 1.25, 0.94
)

// NotchAttention returns the effect of the notch e for arrival a: spring,
// glow and pulse. Keep e the same element while the effect plays.
func NotchAttention(c *ui.Context, e ui.Element, a Arrival) AttentionLook {
	if !playing(c, a, NotchAttentionLength) {
		return Still
	}
	o := ui.KeyframeOptions{Run: a.Run}
	look := AttentionLook{
		Scale: e.Keyframes("attention-bump", bumpFrames(notchPeak, notchDip), o),
		Glow:  e.Keyframes("attention-glow", glowFrames, o),
	}
	pulse := o
	pulse.Delay, pulse.Repeat = AttentionBump, PulseCount-1
	look.Pulse = e.Keyframes("attention-pulse", pulseFrames, pulse)
	return look
}

// BadgeAttention returns the effect of the item badge e for arrival a: spring
// and glow. Keep e the same element while the effect plays.
func BadgeAttention(c *ui.Context, e ui.Element, a Arrival) AttentionLook {
	if !playing(c, a, BadgeAttentionLength) {
		return Still
	}
	o := ui.KeyframeOptions{Run: a.Run}
	return AttentionLook{
		Scale: e.Keyframes("attention-bump", bumpFrames(badgePeak, badgeDip), o),
		Glow:  e.Keyframes("attention-glow", glowFrames, o),
	}
}

// playing tells whether the effect of a plays at this frame. Under Reduce
// Motion MyGo's Keyframes are at their last value at once (the still state)
// and ask for no frame, so the effect needs no check of its own.
func playing(c *ui.Context, a Arrival, length time.Duration) bool {
	return a.Run != 0 && c.Now().Sub(a.At) < length
}

// bumpFrames is the spring: up to peak in 120 ms, a little under 1, and back
// to 1 at AttentionBump.
func bumpFrames(peak, dip float32) []ui.Keyframe {
	return []ui.Keyframe{
		{Value: 1},
		{At: 120 * time.Millisecond, Value: peak, Ease: ui.EaseOut},
		{At: 260 * time.Millisecond, Value: dip, Ease: ui.EaseInOut},
		{At: AttentionBump, Value: 1, Ease: ui.EaseInOut},
	}
}

// glowFrames: the glow comes in 80 ms and fades out over 2 s.
var glowFrames = []ui.Keyframe{
	{Value: 0},
	{At: 80 * time.Millisecond, Value: 1, Ease: ui.EaseOut},
	{At: AttentionGlow, Value: 0, Ease: ui.EaseOut},
}

// pulseFrames is one pulse: to the dim end and back.
var pulseFrames = []ui.Keyframe{
	{Value: 0},
	{At: PulseCycle / 2, Value: 1, Ease: ui.EaseInOut},
	{At: PulseCycle, Value: 0, Ease: ui.EaseInOut},
}
