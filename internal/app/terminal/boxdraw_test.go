package terminal

import (
	"fmt"
	"slices"
	"testing"
)

// lineGlyphs is every character the table draws with lines.
const lineGlyphs = "─│┌┐└┘├┤┬┴┼╴╵╶╷━┃┏┓┗┛┣┫┳┻╋═║╔╗╚╝╠╣╦╩╬"

// boxCell is an 8 x 20 logical pixel cell at the origin.
var boxCell = Edges{X0: 0, Y0: 0, X1: 8, Y1: 20}

// covered is true when the point (x, y), in logical pixels, is inside a quad.
func covered(quads []Rect, x, y float32) bool {
	for _, q := range quads {
		if x >= q.X && x < q.X+q.W && y >= q.Y && y < q.Y+q.H {
			return true
		}
	}
	return false
}

func armsOf(t *testing.T, c rune) Arms {
	t.Helper()
	g, ok := Segments(c)
	if !ok || g.IsBlock {
		t.Fatalf("%c should be a line glyph", c)
	}
	return g.Arms
}

func linesQuads(t *testing.T, c rune, cell Edges, scale float32) []Rect {
	t.Helper()
	g, ok := Segments(c)
	if !ok {
		t.Fatalf("%c should have a glyph", c)
	}
	var out []Rect
	Quads(g, cell, scale, func(q Rect) { out = append(out, q) })
	return out
}

func wantCovered(t *testing.T, quads []Rect, want bool, points ...[2]float32) {
	t.Helper()
	for _, p := range points {
		if covered(quads, p[0], p[1]) != want {
			t.Errorf("covered(%v, %v) = %v, want %v", p[0], p[1], !want, want)
		}
	}
}

func pt(x, y float32) [2]float32 { return [2]float32{x, y} }

func TestTheTableGivesTheArmsOfEachKindOfLine(t *testing.T) {
	cases := []struct {
		c    rune
		want Arms
	}{
		{'┼', Arms{Light, Light, Light, Light}},
		{'╋', Arms{Heavy, Heavy, Heavy, Heavy}},
		{'═', Arms{None, None, Double, Double}},
		{'│', Arms{Light, Light, None, None}},
		{'╴', Arms{None, None, Light, None}},
	}
	for _, c := range cases {
		if got := armsOf(t, c.c); got != c.want {
			t.Errorf("%c: %v, want %v", c.c, got, c.want)
		}
	}
}

func TestEveryListedGlyphIsDrawnAndEachAxisHasOneWeight(t *testing.T) {
	for _, c := range lineGlyphs {
		a := armsOf(t, c)
		if a.Left != None && a.Right != None && a.Left != a.Right {
			t.Errorf("%c: both horizontal arms share a weight", c)
		}
		if a.Up != None && a.Down != None && a.Up != a.Down {
			t.Errorf("%c: both vertical arms share a weight", c)
		}
	}
}

func TestRoundedCornersAndOtherTextAreNotDrawnAsQuads(t *testing.T) {
	for _, c := range []rune{'╭', '╮', '╯', '╰', '+', 'a', ' ', 'é', '界'} {
		if _, ok := Segments(c); ok {
			t.Errorf("%c stays text", c)
		}
	}
}

func TestTheBlockGlyphsFillTheirPartOfTheCell(t *testing.T) {
	full, ok := Segments('█')
	if !ok || !full.IsBlock {
		t.Fatal("█ is a block")
	}
	if full.Block != (Block{0, 0, 1, 1, 1}) {
		t.Errorf("full = %v", full.Block)
	}
	upper := linesQuads(t, '▀', boxCell, 1)
	if want := []Rect{{0, 0, 8, 10}}; !slices.Equal(upper, want) {
		t.Errorf("upper = %v, want %v", upper, want)
	}
	if g, _ := Segments('░'); g.Alpha() != 0.25 {
		t.Errorf("░ alpha = %v", g.Alpha())
	}
	if g, _ := Segments('▓'); g.Alpha() != 0.75 {
		t.Errorf("▓ alpha = %v", g.Alpha())
	}
	if g, _ := Segments('│'); g.Alpha() != 1 {
		t.Errorf("│ alpha = %v", g.Alpha())
	}
}

func TestABlockMeetsTheCellEdgesAtScale1And2(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		if got, want := linesQuads(t, '█', boxCell, scale), []Rect{{0, 0, 8, 20}}; !slices.Equal(got, want) {
			t.Errorf("scale %v: full = %v", scale, got)
		}
		if got, want := linesQuads(t, '▄', boxCell, scale), []Rect{{0, 10, 8, 10}}; !slices.Equal(got, want) {
			t.Errorf("scale %v: lower half = %v", scale, got)
		}
	}
}

func TestACellWithNoWidthDrawsNothing(t *testing.T) {
	flat := boxCell
	flat.X1 = 0
	if got := linesQuads(t, '─', flat, 1); len(got) != 0 {
		t.Errorf("─ = %v", got)
	}
	if got := linesQuads(t, '█', flat, 1); len(got) != 0 {
		t.Errorf("█ = %v", got)
	}
}

func TestEachArmReachesItsCellEdgeAndNoOtherEdge(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		devCols := int(boxCell.X1 * scale)
		devRows := int(boxCell.Y1 * scale)
		// The middle of the first and last device pixel on each side.
		inset := 0.5 / scale
		for _, c := range lineGlyphs {
			quads := linesQuads(t, c, boxCell, scale)
			arms := armsOf(t, c)
			// Sample points: the centre of each device pixel along the x and y axes.
			hitAtY := func(y float32) bool {
				for i := 0; i < devCols; i++ {
					if covered(quads, (float32(i)+0.5)/scale, y) {
						return true
					}
				}
				return false
			}
			hitAtX := func(x float32) bool {
				for j := 0; j < devRows; j++ {
					if covered(quads, x, (float32(j)+0.5)/scale) {
						return true
					}
				}
				return false
			}
			got := fmt.Sprint(hitAtY(inset), hitAtX(boxCell.X1-inset), hitAtY(boxCell.Y1-inset), hitAtX(inset))
			want := fmt.Sprint(arms.Up != None, arms.Right != None, arms.Down != None, arms.Left != None)
			if got != want {
				t.Errorf("%c at scale %v: (top, right, bottom, left) = %s, want %s", c, scale, got, want)
			}
		}
	}
}

func TestAVerticalLineSpansTheCellHeightWithNoGapAtScale1And2(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		cell := Edges{X0: 10, Y0: 20, X1: 18, Y1: 40}
		quads := linesQuads(t, '│', cell, scale)
		top, bottom := float32(1e30), float32(-1e30)
		for _, q := range quads {
			top = min(top, q.Y)
			bottom = max(bottom, q.Y+q.H)
		}
		if top != cell.Y0 || bottom != cell.Y1 {
			t.Errorf("scale %v: span (%v, %v), want (%v, %v)", scale, top, bottom, cell.Y0, cell.Y1)
		}
		// Two cells one under the other: every device row of both is inside a quad.
		below := cell
		below.Y0, below.Y1 = cell.Y1, cell.Y1+20
		both := append(slices.Clone(quads), linesQuads(t, '│', below, scale)...)
		x := (cell.X0 + cell.X1) / 2
		for row := 0; row < int(2*20*scale); row++ {
			y := cell.Y0 + (float32(row)+0.5)/scale
			if !covered(both, x, y) {
				t.Errorf("scale %v: device row %d has a gap", scale, row)
			}
		}
	}
}

func TestALightLineIsOneLogicalPixelAndAHeavyLineTwoAtAnyScale(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		for _, q := range linesQuads(t, '─', boxCell, scale) {
			if q.H != 1 {
				t.Errorf("scale %v: light h = %v", scale, q.H)
			}
		}
		for _, q := range linesQuads(t, '━', boxCell, scale) {
			if q.H != 2 {
				t.Errorf("scale %v: heavy h = %v", scale, q.H)
			}
		}
	}
}

func TestALightCrossHasNoChannelSoItsCentreIsCovered(t *testing.T) {
	q := linesQuads(t, '┼', boxCell, 1)
	wantCovered(t, q, true, pt(4.5, 10.5), pt(4.5, 0.5), pt(0.5, 10.5))
}

func TestADoubleLineIsTwoOnePixelLinesTwoPixelsApart(t *testing.T) {
	q := linesQuads(t, '═', boxCell, 1)
	// Rows 9 and 11 are the two lines; row 10 between them is open.
	wantCovered(t, q, true, pt(4.5, 9.5), pt(4.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 10.5))
}

func TestADoubleCornerClosesItsOuterCornerAndLeavesItsInnerChannelOpen(t *testing.T) {
	q := linesQuads(t, '╔', boxCell, 1)
	// The outer corner is closed, and so is the gap above the channel.
	wantCovered(t, q, true, pt(3.5, 9.5), pt(4.5, 9.5))
	// The inner corner is closed, and the channel under the outer top line is open.
	wantCovered(t, q, true, pt(5.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 11.5))
}

func TestAMirroredDoubleCornerClosesItsOuterCornerAndLeavesItsChannelOpen(t *testing.T) {
	q := linesQuads(t, '╗', boxCell, 1)
	// The outer top line runs left to the outer right stem, so the top corner is closed.
	wantCovered(t, q, true, pt(5.5, 9.5), pt(4.5, 9.5))
	// The inner stem joins the inner bar, and the gap between the two stems stays open below it.
	wantCovered(t, q, true, pt(3.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 11.5))
}

func TestADoubleTKeepsTheChannelOfItsStemOpenWhereItsBarsJoin(t *testing.T) {
	// ╠: the outer stem line is full height. The inner stem line breaks where the bars run
	// off it, so the channel between the two bars stays open to the right edge.
	q := linesQuads(t, '╠', boxCell, 1)
	wantCovered(t, q, true, pt(3.5, 0.5), pt(3.5, 10.5), pt(3.5, 19.5), pt(5.5, 9.5), pt(5.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 9.5), pt(5.5, 10.5), pt(7.5, 10.5))
}

func TestADoubleTWithBarsToTheLeftBreaksItsInnerStemLine(t *testing.T) {
	// ╣ is ╠ mirrored: the inner stem line is the left one.
	q := linesQuads(t, '╣', boxCell, 1)
	wantCovered(t, q, true, pt(5.5, 0.5), pt(5.5, 10.5), pt(5.5, 19.5), pt(3.5, 9.5), pt(3.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 9.5), pt(3.5, 10.5), pt(2.5, 10.5))
}

func TestADoubleBarWithAStemBelowHangsTheStemFromItsInnerLine(t *testing.T) {
	// ╦: the bar runs the full width, and the stem hangs from the inner (lower) line.
	q := linesQuads(t, '╦', boxCell, 1)
	wantCovered(t, q, true, pt(0.5, 9.5), pt(3.5, 19.5))
	wantCovered(t, q, false, pt(0.5, 10.5), pt(3.5, 10.5))
	// The outer bar line is whole, and the inner one breaks where the stem channel starts.
	wantCovered(t, q, true, pt(4.5, 9.5), pt(3.5, 11.5), pt(5.5, 11.5))
	wantCovered(t, q, false, pt(4.5, 11.5), pt(4.5, 19.5))
}

func TestADoubleBarWithAStemAboveBreaksItsInnerLineAboveTheChannel(t *testing.T) {
	// ╩ is ╦ mirrored: the inner bar line is the upper one.
	q := linesQuads(t, '╩', boxCell, 1)
	wantCovered(t, q, true, pt(0.5, 9.5), pt(3.5, 0.5), pt(4.5, 11.5))
	wantCovered(t, q, false, pt(0.5, 10.5), pt(3.5, 10.5), pt(4.5, 9.5), pt(4.5, 0.5))
}

func TestADoubleCrossKeepsAllFourChannelPointsOpen(t *testing.T) {
	q := linesQuads(t, '╬', boxCell, 1)
	// The four pixels next to the centre, where each double line crosses the other channel,
	// and the centre itself.
	wantCovered(t, q, false, pt(4.5, 9.5), pt(3.5, 10.5), pt(5.5, 10.5), pt(4.5, 11.5), pt(4.5, 10.5))
	// The channel of each arm stays open up to the centre.
	wantCovered(t, q, false, pt(4.5, 2.5), pt(4.5, 17.5), pt(2.5, 10.5), pt(6.5, 10.5))
	// The four corners around the centre are closed.
	wantCovered(t, q, true, pt(3.5, 9.5), pt(5.5, 9.5), pt(3.5, 11.5), pt(5.5, 11.5))
}

func TestADoubleCrossKeepsItsChannelOpenAtScale2(t *testing.T) {
	q := linesQuads(t, '╬', boxCell, 2)
	// Device pixels around the centre channel: the channel is two device pixels wide.
	for _, d := range [][2]int{{7, 19}, {7, 18}, {6, 19}, {9, 19}, {7, 21}} {
		x, y := (float32(d[0])+0.5)/2, (float32(d[1])+0.5)/2
		if covered(q, x, y) {
			t.Errorf("device (%d, %d) is in the channel", d[0], d[1])
		}
	}
	// A device pixel on an outer line, away from the channel, is closed.
	wantCovered(t, q, true, pt(2.75, 8.75))
}

func TestATJunctionJoinsItsStemAndItsBarAtTheCentre(t *testing.T) {
	q := linesQuads(t, '├', boxCell, 1)
	// The stem is the device column 4, the bar is row 10. The stem runs the full height, the
	// bar reaches the right edge, and the centre is covered.
	wantCovered(t, q, true, pt(4.5, 0.5), pt(4.5, 19.5), pt(7.5, 10.5), pt(4.5, 10.5))
	wantCovered(t, q, false, pt(0.5, 10.5))
}

func TestACornerHasNoNotchWhereItsTwoArmsMeet(t *testing.T) {
	// ┘: the vertical arm ends at the bottom of the bar, and the bar ends at the right
	// of the stem. The pixel where they meet is covered.
	q := linesQuads(t, '┘', boxCell, 1)
	wantCovered(t, q, true, pt(4.5, 10.5), pt(4.5, 0.5), pt(0.5, 10.5))
}
