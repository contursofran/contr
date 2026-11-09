package kit

import (
	"fmt"
	"log"
	"math"
	"sync"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
)

// MascotBanner is the Claude Code mascot as the block characters of Claude
// Code's start banner (#549). assets/icons/claude-mascot.svg draws it cell
// for cell; a test keeps the two the same.
var MascotBanner = [...]string{
	" ▐▛███▜▌",
	"▝▜█████▛▘",
	"  ▘▘ ▝▝",
}

// The size of the mascot grid in pixels: two pixels per character each way.
const (
	MascotCols = 18
	MascotRows = 6
)

// quadrants gives the four pixels of a block character, in the order top
// left, top right, bottom left, bottom right.
var quadrants = map[rune][4]bool{
	' ': {},
	'▘': {true, false, false, false},
	'▝': {false, true, false, false},
	'▖': {false, false, true, false},
	'▗': {false, false, false, true},
	'▀': {true, true, false, false},
	'▄': {false, false, true, true},
	'▌': {true, false, true, false},
	'▐': {false, true, false, true},
	'▚': {true, false, false, true},
	'▞': {false, true, true, false},
	'▛': {true, true, true, false},
	'▜': {true, true, false, true},
	'▙': {true, false, true, true},
	'▟': {false, true, true, true},
	'█': {true, true, true, true},
}

// MascotGrid converts banner lines of block characters to a pixel grid of
// 2·len(lines) rows and 2·(longest line) columns. Each character is 2×2
// pixels; a short line ends in empty pixels. A character that is not a
// block character is an error.
func MascotGrid(lines []string) ([][]bool, error) {
	cols := 0
	for _, l := range lines {
		cols = max(cols, utf8.RuneCountInString(l))
	}
	grid := make([][]bool, 2*len(lines))
	for i := range grid {
		grid[i] = make([]bool, 2*cols)
	}
	for row, l := range lines {
		col := 0
		for _, ch := range l {
			q, ok := quadrants[ch]
			if !ok {
				return nil, fmt.Errorf("mascot: line %d: %q is not a block character", row+1, ch)
			}
			grid[2*row][2*col], grid[2*row][2*col+1] = q[0], q[1]
			grid[2*row+1][2*col], grid[2*row+1][2*col+1] = q[2], q[3]
			col++
		}
	}
	return grid, nil
}

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

// The ink of the mascot grid: columns 1 to 16 and rows 0 to 4. Column 0,
// column 17 and row 5 are empty. A test checks them against MascotGrid.
const (
	mascotInkLeft = 1
	mascotInkCols = 16
	mascotInkRows = 5
)

// mascotRect is the rect of the whole mascot grid in a glyph box r, lift DIPs
// higher. The ink is as wide as the box and centred in it, so a cell is
// r.W/16: one DIP in the 16 px box, a whole number of device pixels and
// sharp. The empty columns go past the box. Each edge is on a device pixel at
// scale, so the mask keeps its size and its shape while the mascot hops.
func mascotRect(r ui.Rect, lift, scale float32) ui.Rect {
	if scale <= 0 {
		scale = 1
	}
	px := func(v float32) float32 { return float32(math.Round(float64(v*scale))) / scale }
	cell := r.W / mascotInkCols
	x := px(r.X - mascotInkLeft*cell)
	y := px(r.Y + r.H/2 - mascotInkRows*cell/2 - lift)
	return ui.Rect{X: x, Y: y, W: px(MascotCols * cell), H: px(MascotRows * cell)}
}
