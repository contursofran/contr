package kit

// The overlay layer above the terminal panel (docs/spec/Terminal.md).

import "github.com/egoist/mygo/ui"

// LabelTerminalOverlay names the layer that holds the overlays of the terminal.
const LabelTerminalOverlay = "terminal overlay"

// TerminalOverlay is the layer above the terminal for the questions notch and
// the outline (#346, #348). It builds nothing while it has no layer, so it
// takes no pointer or key from the terminal. A layer draws only small floating
// elements, placed with Absolute; it must not cover the grid. A key goes to a
// layer only while an element of the layer has the focus. UI thread only.
type TerminalOverlay struct {
	layers []*overlayLayer
}

type overlayLayer struct {
	name  string
	build func(c *ui.Context)
}

// Add puts a layer above the terminal and returns the function that removes
// it. Layers build in the order they were added, the last on top.
func (o *TerminalOverlay) Add(name string, build func(c *ui.Context)) (remove func()) {
	l := &overlayLayer{name: name, build: build}
	o.layers = append(o.layers, l)
	return func() {
		for i, x := range o.layers {
			if x == l {
				o.layers = append(o.layers[:i], o.layers[i+1:]...)
				return
			}
		}
	}
}

// Empty tells that no layer is set.
func (o *TerminalOverlay) Empty() bool { return len(o.layers) == 0 }

// View builds the layers inside the terminal area.
func (o *TerminalOverlay) View(c *ui.Context) {
	for _, l := range o.layers {
		l.build(c)
	}
}

// Names lists the names of the layers, in the order they build.
func (o *TerminalOverlay) Names() []string {
	names := make([]string, len(o.layers))
	for i, l := range o.layers {
		names[i] = l.name
	}
	return names
}
