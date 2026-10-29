// Ported from the Rust contr module status_bar.

package app

// The status bar along the bottom of the frame. Its left side shows each plan's usage as the
// percent left and a bar. Its right side shows the message slot, host CPU and memory, the contr
// version, and the herdr version while herdr is connected.
//
// The bar is cached: [statusBar.refresh] reads the sources and tells whether what the bar shows
// changed. It changes only when the plan usage, the host reading, the message or the herdr version
// changes. A herdr change that keeps the version changes nothing. Its methods run on the UI thread.

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// Version is contr's version, shown in the status bar.
const Version = "0.1.0"

// noReading is shown in place of a reading that has not arrived yet.
const noReading = "–"

// Labels of the status bar parts. A plan's labels are "usage-<id>", "usage-<id>-bar",
// "usage-<id>-fill" or "usage-<id>-low", and "usage-<id>-pct".
const (
	LabelUsage        = "usage"
	LabelStatusRight  = "status-right"
	LabelStatusMsg    = "status-message"
	LabelHostCPU      = "host-cpu-status"
	LabelHostMem      = "host-mem-status"
	LabelContrVersion = "contr-version"
	LabelHerdrVersion = "herdr-version"
)

// The sources that the bar reads. [host.Host], [usage.Sampler] and [state.StatusMessage] fit them.
type (
	hostSource interface {
		Stats() (host.Stats, bool)
	}
	usageSource interface {
		Left(plan usage.PlanID, now uint64) (float64, bool)
	}
	messageSource interface {
		Text() (string, bool)
	}
)

// tone is how a segment is coloured (DESIGN.md: labels dim, numbers secondary, versions dim-2).
type tone uint8

const (
	// toneMessage is the message slot. It truncates before anything else when the window is narrow.
	toneMessage tone = iota
	// toneReading is a live reading: dim label, secondary value.
	toneReading
	// toneMeta is a version: dim-2 label and value.
	toneMeta
)

// segment is one item on the right side of the bar.
type segment struct {
	label string // element label
	tone  tone
	// name is "CPU", "MEM", "contr", "herdr", or the whole message text.
	name string
	// value is "38%", "–", "0.1.0" or "0.9.3". It is empty for the message.
	value string
}

// text is the item as plain text, for example "CPU 38%".
func (s segment) text() string {
	if s.value == "" {
		return s.name
	}
	return s.name + " " + s.value
}

// planSegment is one plan on the left side, for example "claude ▬ 62%". left is the percent left
// and has is false without data.
type planSegment struct {
	plan usage.PlanID
	left float64
	has  bool
}

// text is the item as plain text, for example "claude 62%". The bar has no text.
func (p planSegment) text() string { return p.plan.Label() + " " + usage.LeftLabel(p.left, p.has) }

// isLow is true when the owner must act on a plan: its percent left is under
// [theme.UsageLowLeft].
func isLow(left float64) bool { return left < theme.UsageLowLeft }

// pctColour is the colour of a plan's percent text. Under [theme.UsageLowLeft] it is white, like
// the fill, so the low state shows at 0 to 2 % fill too (D74). Otherwise it is secondary text.
func pctColour(left float64, has bool) uint32 {
	if has && isLow(left) {
		return theme.YouBg
	}
	return theme.Text2
}

// fillWidth is the filled part of a track: [theme.UsageBarWidth] × left / 100.
func fillWidth(left float64) float32 {
	return float32(theme.UsageBarWidth * min(max(left, 0), 100) / 100)
}

// barContent is everything that the bar shows. It can be compared with [slices.Equal] on its parts.
type barContent struct {
	plans []planSegment
	right []segment
}

func (a barContent) equal(b barContent) bool {
	return slices.Equal(a.plans, b.plans) && slices.Equal(a.right, b.right)
}

// statusBar reads the sources and draws the bar.
type statusBar struct {
	host    hostSource
	usage   usageSource
	message messageSource
	herdr   liveSource
	// now is the Unix time that the plan windows are read at.
	now func() uint64

	shown barContent
	read  bool
}

// herdrVersion is the version to show: the server's, while herdr is connected.
func herdrVersion(h liveSource) (string, bool) {
	if h == nil {
		return "", false
	}
	if s := h.Live().State; s.Kind == herdr.Connected {
		return s.ServerVersion, true
	}
	return "", false
}

// content reads the sources. A nil source shows no reading.
func (b *statusBar) content() barContent {
	now := usage.UnixNow
	if b.now != nil {
		now = b.now
	}
	var c barContent
	for _, plan := range usage.Plans {
		p := planSegment{plan: plan}
		if b.usage != nil {
			p.left, p.has = b.usage.Left(plan, now())
		}
		c.plans = append(c.plans, p)
	}
	cpu, mem := noReading, noReading
	if b.host != nil {
		if stats, ok := b.host.Stats(); ok {
			cpu, mem = stats.CPULabel(), stats.MemLabel()
		}
	}
	if b.message != nil {
		if text, ok := b.message.Text(); ok {
			c.right = append(c.right, segment{LabelStatusMsg, toneMessage, text, ""})
		}
	}
	c.right = append(c.right,
		segment{LabelHostCPU, toneReading, "CPU", cpu},
		segment{LabelHostMem, toneReading, "MEM", mem},
		segment{LabelContrVersion, toneMeta, "contr", Version})
	if v, ok := herdrVersion(b.herdr); ok {
		c.right = append(c.right, segment{LabelHerdrVersion, toneMeta, "herdr", v})
	}
	return c
}

// refresh reads the sources and tells whether the bar's content changed since the last refresh.
// The first refresh always counts as a change.
func (b *statusBar) refresh() bool {
	c := b.content()
	if b.read && c.equal(b.shown) {
		return false
	}
	b.shown, b.read = c, true
	return true
}

// leftTexts are the texts of the plans, in [usage.Plans] order, for example "claude 62%".
func (b *statusBar) leftTexts() []string {
	var out []string
	for _, p := range b.content().plans {
		out = append(out, p.text())
	}
	return out
}

// rightTexts are the texts of the right side's items in display order, without separators.
func (b *statusBar) rightTexts() []string {
	var out []string
	for _, s := range b.content().right {
		out = append(out, s.text())
	}
	return out
}

// View draws the bar into the frame's status bar box.
func (b *statusBar) View(c *ui.Context) {
	b.refresh()
	ui.Row(c).Fill().AlignItems(ui.Center).PaddingX(theme.StatusPadX).Children(func() {
		ui.Row(c).Label(LabelUsage).Shrink(0).AlignItems(ui.Center).Gap(theme.StatusGap).Children(func() {
			for i, p := range b.shown.plans {
				if i > 0 {
					separator(c)
				}
				planView(c, p)
			}
		})
		ui.Row(c).Label(LabelStatusRight).Grow(1).MinWidth(0).AlignItems(ui.Center).Justify(ui.End).
			Gap(theme.StatusGap).Children(func() {
			for i, s := range b.shown.right {
				if i > 0 {
					separator(c)
				}
				segmentView(c, s)
			}
		})
	})
}

func separator(c *ui.Context) {
	ui.Text(c, "·").Shrink(0).TextColor(color(theme.TextDim2))
}

func segmentView(c *ui.Context, s segment) {
	switch s.tone {
	case toneMessage:
		ui.Row(c).Label(s.label).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, s.name).SingleLine().MinWidth(0).TextColor(color(theme.Text2))
		})
	case toneReading:
		ui.Row(c).Label(s.label).Shrink(0).AlignItems(ui.Center).Gap(theme.StatusGap).Children(func() {
			ui.Text(c, s.name).TextColor(color(theme.TextDim))
			ui.Text(c, s.value).TextColor(color(theme.Text2))
		})
	default:
		ui.Row(c).Label(s.label).Shrink(0).Children(func() {
			ui.Text(c, s.text()).TextColor(color(theme.TextDim2))
		})
	}
}

// planView draws the label, the track and the percent. The track is empty without data.
func planView(c *ui.Context, p planSegment) {
	id := p.plan.ID()
	ui.Row(c).Label("usage-" + id).Shrink(0).AlignItems(ui.Center).Gap(theme.StatusGap).Children(func() {
		ui.Text(c, p.plan.Label()).TextColor(color(theme.TextDim))
		ui.Row(c).Label("usage-" + id + "-bar").Shrink(0).Width(theme.UsageBarWidth).Height(theme.UsageBarHeight).
			Background(color(theme.Line)).Radius(theme.UsageBarRadius).Clip().Children(func() {
			if !p.has {
				return
			}
			label, fill := "usage-"+id+"-fill", uint32(theme.TextDim)
			if isLow(p.left) {
				label, fill = "usage-"+id+"-low", theme.YouBg
			}
			ui.Box(c).Label(label).Width(fillWidth(p.left)).FillHeight().Shrink(0).Background(color(fill))
		})
		ui.Text(c, usage.LeftLabel(p.left, p.has)).Label("usage-" + id + "-pct").
			TextColor(color(pctColour(p.left, p.has)))
	})
}
