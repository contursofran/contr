package kit

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// NoValue is the value of a meter that has no data (D255; a dash before it).
const NoValue = "no data yet"

// Meter is one line of a [UsageMeter]: a label, a value, and a bar.
type Meter struct {
	// Label names the line and the meter ("Context", "Claude").
	Label string
	// Value is the text at the right of the line, such as "62%". Empty
	// shows [NoValue].
	Value string
	// Detail is the text after the label, such as the sizes of the context
	// ("90k of 200k"). It clips with an ellipsis; the label and the value
	// do not. It shows only with data.
	Detail string
	// Percent is the filled part of the bar, 0 to 100. It counts only when
	// Has is true.
	Percent float64
	// Has tells that there is data. Without it the value is [NoValue] and
	// the bar is empty.
	Has bool
	// Soon draws the value and the fill in ink-100: the context compacts
	// soon (D264). No hue.
	Soon bool
}

// UsageMeter builds the info block of the files panel (docs/spec/UsageMeter.md):
// for each line, its label and value, then its bar. Put it in the footer of
// the Panel. No hairline sits above it (D258): the 12px above the text keep
// it apart from the body.
func UsageMeter(c *ui.Context, meters []Meter) ui.Element {
	return ui.Column(c).Label("usage").Grow(1).MinWidth(0).Gap(theme.Space7).
		// The Panel footer already has 6px above its content.
		// The sides are 10px, so the text sits 20px from the panel side (D228).
		Padding(theme.Space12-theme.Space6, theme.Space10, theme.Space2).Children(func() {
		for i, m := range meters {
			// Each meter after the first has space-6 more above it.
			var top float32
			if i > 0 {
				top = theme.Space6
			}
			meterLine(c, m, top)
		}
	})
}

// meterLine is the row of label and value, with the bar under it, and top
// more space above it.
func meterLine(c *ui.Context, m Meter, top float32) {
	p := theme.Current()
	value, ink := meterValueText(p, m)
	ui.Column(c).Label(m.Label+" line").Gap(theme.Space4).Margin(top, 0, 0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
			label := ui.Text(c, m.Label).SingleLine().
				Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).FixedLineHeight(theme.StyleSmall.Line).TextColor(p.Ink48)
			if m.Detail == "" || !m.Has {
				label.Grow(1).MinWidth(0)
			} else {
				label.Shrink(0)
				ui.Text(c, m.Detail).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).MarginStart(theme.Space4).
					Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size).FixedLineHeight(theme.StyleSmall.Line).TextColor(p.Ink48)
			}
			meterValue(ui.Text(c, value).SingleLine().Shrink(0).
				Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).FixedLineHeight(theme.StyleValue.Line).TextColor(ink), m)
		})
		meterBar(c, m)
	})
}

// meterValueText is the value of a meter line and its ink: [NoValue] in
// ink-48 without data, ink-100 when the context compacts soon (D264), else
// ink-64.
func meterValueText(p *theme.Palette, m Meter) (string, ui.Color) {
	switch {
	case !m.Has || m.Value == "":
		return NoValue, p.Ink48
	case m.Soon:
		return m.Value, p.Ink100
	}
	return m.Value, p.Ink64
}

// meterBar is the bar of a meter. ui.Meter gives the role, the range and the
// name for assistive technology. Its colors are the theme's, so the track and
// the fill are set again from the tokens.
func meterBar(c *ui.Context, m Meter) ui.Element {
	p := theme.Current()
	pct := 0.0
	if m.Has {
		pct = math.Min(math.Max(m.Percent, 0), 100)
	}
	fill := p.MeterFill
	if m.Soon && m.Has {
		fill = p.Ink100
	}
	bar := ui.Meter(c, pct, 0, 100, nil).Label(m.Label + " meter").
		Height(theme.SizeMeterHeight).MinWidth(0).Radius(theme.Radius2).Background(p.MeterTrack)
	// The fill moves to its width in theme.MotionMeter (D256); the value
	// for assistive technology is the new one at once.
	shown := MeterFill(bar, pct)
	return bar.Draw(func(pt *ui.Painter, r ui.Rect) {
		if shown > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(shown/100), H: r.H}, fill, theme.Radius2)
		}
	})
}

// meterValue rolls the digits of the value text e of m as they change
// (D256). A meter without data shows NoValue (D255): it changes at once.
func meterValue(e ui.Element, m Meter) ui.Element {
	if !m.Has || m.Value == "" {
		return e
	}
	return RollNumber(e, m.Percent)
}

// PlanUsageWidth is the width of a bar of [PlanUsage].
const PlanUsageWidth = 44

// PlanUsage builds the plan meters of the sidebar foot (D196): for each plan,
// inline, its label, a 44px bar and its value ("Claude ▬ 38%  Codex ▬ 12%").
// A plan with no data has no bar: its label and [NoValue] in ink-48 (D255).
// A narrow foot hides the plans that do not fit whole.
func PlanUsage(c *ui.Context, meters []Meter) ui.Element {
	p := theme.Current()
	// A plan that does not fit goes to a second line, which the clip hides: a narrow foot
	// shows whole plans only.
	return ui.Row(c).Label("plan usage").AlignItems(ui.Center).Gap(theme.Space7 + theme.Space6).
		Wrap().Height(theme.StyleValue.Line).MinWidth(0).Shrink(1).Clip().Children(func() {
		for _, m := range meters {
			value, ink := m.Value, p.Ink64
			has := m.Has && value != ""
			if !has {
				value, ink = NoValue, p.Ink48
			}
			ui.Row(c).Label(m.Label + " plan").AlignItems(ui.Center).Gap(theme.Space7).Shrink(0).Height(theme.StyleValue.Line).Children(func() {
				ui.Text(c, m.Label).SingleLine().Shrink(0).
					Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).TextColor(p.Ink48)
				if has {
					meterBar(c, m).Width(PlanUsageWidth).Shrink(0)
				}
				meterValue(ui.Text(c, value).SingleLine().Shrink(0).
					Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).TextColor(ink), m)
			})
		}
	})
}
