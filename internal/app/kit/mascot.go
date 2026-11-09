package kit

import (
	"log"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
)

// The mascot is 16×10 cells of 1.5 SVG units: its viewBox is 24×15
// (docs/spec/StatusGlyph.md, D218).
const (
	mascotCols = 16
	mascotRows = 10
)

var (
	mascotOnce sync.Once
	mascotSVG  *ui.SVG
)

// ClaudeMascot returns the Claude Code mascot, assets/icons/claude-mascot.svg.
// A file that does not parse is logged and gives nil; the tests make sure it
// parses.
func ClaudeMascot() *ui.SVG {
	mascotOnce.Do(func() {
		data, err := assets.Icons.ReadFile("icons/claude-mascot.svg")
		if err != nil {
			log.Printf("kit: claude mascot: %v", err)
			return
		}
		mascotSVG, err = ui.ParseSVG(data)
		if err != nil {
			log.Printf("kit: claude mascot: %v", err)
		}
	})
	return mascotSVG
}

// mascotRect is the rect of the mascot in a glyph box r, lift DIPs higher: as
// wide as the box, 10/16 as high, centred. A cell is r.W/16: one DIP in the
// 16px box, a whole number of device pixels, so the edges are sharp. Each edge
// is on a device pixel at scale, so the mask keeps its size and its shape
// while the mascot hops.
func mascotRect(r ui.Rect, lift, scale float32) ui.Rect {
	if scale <= 0 {
		scale = 1
	}
	px := func(v float32) float32 { return float32(math.Round(float64(v*scale))) / scale }
	h := px(r.W * mascotRows / mascotCols)
	return ui.Rect{X: px(r.X), Y: px(r.Y + (r.H-h)/2 - lift), W: px(r.W), H: h}
}
