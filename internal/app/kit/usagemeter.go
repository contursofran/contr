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
	// Value is the text at the right of the label, such as "62%". Empty
	// shows [NoValue].
	Value string
	// Percent is the filled part of the bar, 0 to 100. It counts only when
	// Has is true.
	Percent float64
	// Has tells that there is data. Without it the value is [NoValue] and
	// the bar is empty.
	Has bool
}

// UsageMeter builds the info block of the files panel (docs/spec/UsageMeter.md):
// for each line, its label and value, then its bar. Put it in the footer of
// the Panel, which draws the hairline above it.
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
	value, ink := m.Value, p.Ink62
	if !m.Has || value == "" {
		value, ink = NoValue, p.Ink42
	}
	ui.Column(c).Label(m.Label+" line").Gap(theme.Space4).Margin(top, 0, 0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
			ui.Text(c, m.Label).SingleLine().Grow(1).MinWidth(0).
				Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).FixedLineHeight(theme.StyleSmall.Line).TextColor(p.Ink42)
			ui.Text(c, value).SingleLine().Shrink(0).
				Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).FixedLineHeight(theme.StyleValue.Line).TextColor(ink)
		})
		meterBar(c, m)
	})
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
	return ui.Meter(c, pct, 0, 100, nil).Label(m.Label + " meter").
		Height(theme.SizeMeterHeight).MinWidth(0).Radius(theme.Radius2).Background(p.MeterTrack).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			if pct > 0 {
				pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(pct/100), H: r.H}, fill, theme.Radius2)
			}
		})
}

// PlanUsageWidth is the width of a bar of [PlanUsage].
const PlanUsageWidth = 44

// PlanUsage builds the plan meters of the sidebar foot (D196): for each plan,
// inline, its label, a 44px bar and its value ("Claude ▬ 38%  Codex ▬ 12%").
// A plan with no data has no bar: its label and [NoValue] in ink-42 (D255).
// A narrow foot hides the plans that do not fit whole.
func PlanUsage(c *ui.Context, meters []Meter) ui.Element {
	p := theme.Current()
	// A plan that does not fit goes to a second line, which the clip hides: a narrow foot
	// shows whole plans only.
	return ui.Row(c).Label("plan usage").AlignItems(ui.Center).Gap(theme.Space7 + theme.Space6).
		Wrap().Height(theme.StyleValue.Line).MinWidth(0).Shrink(1).Clip().Children(func() {
		for _, m := range meters {
			value, ink := m.Value, p.Ink62
			has := m.Has && value != ""
			if !has {
				value, ink = NoValue, p.Ink42
			}
			ui.Row(c).Label(m.Label + " plan").AlignItems(ui.Center).Gap(theme.Space7).Shrink(0).Height(theme.StyleValue.Line).Children(func() {
				ui.Text(c, m.Label).SingleLine().Shrink(0).
					Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).TextColor(p.Ink42)
				if has {
					meterBar(c, m).Width(PlanUsageWidth).Shrink(0)
				}
				ui.Text(c, value).SingleLine().Shrink(0).
					Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).TextColor(ink)
			})
		}
	})
}
