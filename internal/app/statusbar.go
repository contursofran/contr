// Ported from the Rust contr module status_bar.

package app

// The status bar along the bottom of the frame. It shows the message slot, host CPU and memory, the
// contr version, and the hc core's version while contr follows the core. The plan usage moved to the
// files panel (D126, UsageMeter); #352 removes the bar.
//
// The bar is cached: [statusBar.refresh] reads the sources and tells whether what the bar shows
// changed. It changes only when the host reading, the message or the core version changes. Its methods run on the UI thread.

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
)

// Version is contr's version, shown in the status bar. It comes from the VERSION file (D107).
var Version = contr.Version()

// TextUpdateReady is the status bar item for a newer version (D115). A click installs it.
const TextUpdateReady = "update ready · restart"

// noReading is shown in place of a reading that has not arrived yet.
const noReading = "–"

// Labels of the status bar parts.
const (
	LabelStatusRight  = "status-right"
	LabelStatusMsg    = "status-message"
	LabelHostCPU      = "host-cpu-status"
	LabelHostMem      = "host-mem-status"
	LabelContrVersion = "contr-version"
	LabelCoreVersion  = "hc-version"
	LabelUpdateReady  = "update-ready"
)

// The sources that the bar reads. [host.Host] and [state.StatusMessage] fit them.
type (
	hostSource interface {
		Stats() (host.Stats, bool)
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
	// toneAction is an item the owner clicks. It is white, because it waits for the owner.
	toneAction
)

// segment is one item on the right side of the bar.
type segment struct {
	label string // element label
	tone  tone
	// name is "CPU", "MEM", "contr", "hc", or the whole message text.
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

// barContent is everything that the bar shows. It can be compared with [slices.Equal] on its parts.
type barContent struct {
	right []segment
}

func (a barContent) equal(b barContent) bool { return slices.Equal(a.right, b.right) }

// statusBar reads the sources and draws the bar.
type statusBar struct {
	host    hostSource
	message messageSource
	// core gives the hc core's version, and whether contr follows the core.
	core func() (string, bool)
	// updateReady tells whether a newer version waits (D115); install is the click on its item.
	updateReady func() bool
	install     func()

	shown barContent
	read  bool
}

// content reads the sources. A nil source shows no reading.
func (b *statusBar) content() barContent {
	var c barContent
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
	if b.updateReady != nil && b.updateReady() {
		c.right = append(c.right, segment{LabelUpdateReady, toneAction, TextUpdateReady, ""})
	}
	c.right = append(c.right,
		segment{LabelHostCPU, toneReading, "CPU", cpu},
		segment{LabelHostMem, toneReading, "MEM", mem},
		segment{LabelContrVersion, toneMeta, "contr", Version})
	if b.core != nil {
		if v, ok := b.core(); ok {
			c.right = append(c.right, segment{LabelCoreVersion, toneMeta, "hc", v})
		}
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
		ui.Row(c).Label(LabelStatusRight).Grow(1).MinWidth(0).AlignItems(ui.Center).Justify(ui.End).
			Gap(theme.StatusGap).Children(func() {
			for i, s := range b.shown.right {
				if i > 0 {
					separator(c)
				}
				b.segmentView(c, s)
			}
		})
	})
}

func separator(c *ui.Context) {
	ui.Text(c, "·").Shrink(0).TextColor(color(theme.TextDim2))
}

func (b *statusBar) segmentView(c *ui.Context, s segment) {
	switch s.tone {
	case toneAction:
		ui.Row(c).Label(s.label).Shrink(0).Cursor(ui.CursorPointer).OnClick(func() {
			if b.install != nil {
				b.install()
			}
		}).Children(func() {
			ui.Text(c, s.text()).TextColor(color(theme.YouBg))
		})
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
