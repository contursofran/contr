package diagram

import (
	"math"
	"sort"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Sizes of the drawing, in points.
const (
	boxMinH   = 36
	boxPadX   = 14
	boxMinW   = 88
	boxRadius = theme.Radius8
	rowGap    = 26
	colGapMin = 64
	headLen   = 7
	backDrop  = 26 // how far under the boxes a back edge goes
)

// Style is the text of a diagram: the box text and the edge labels. The chat gives them at its
// text size (D272).
type Style struct {
	Node, Label theme.TextStyle
}

// DefaultStyle is the text at the base size: body text in the boxes, meta text on the edges.
var DefaultStyle = Style{Node: theme.StyleBody, Label: theme.StyleMeta}

func (st Style) node(s string) ui.Span {
	return ui.Span{Text: s, Font: theme.FontFamilySans, Size: st.Node.Size, Weight: st.Node.Weight}
}

func (st Style) label(s string) ui.Span {
	return ui.Span{Text: s, Font: theme.FontFamily, Size: st.Label.Size}
}

func stateColor(pal *theme.Palette, s State) (ui.Color, bool) {
	switch s {
	case "done":
		return pal.ContentDone, true
	case "running":
		return pal.ContentRunning, true
	case "waiting":
		return pal.ContentWaiting, true
	case "failed":
		return pal.ContentFailed, true
	case "info":
		return pal.ContentInfo, true
	}
	return ui.Color{}, false
}

// head is an open arrow head at (x, y), pointing along (dx, dy).
func head(x, y, dx, dy float32) *ui.Path {
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return &ui.Path{}
	}
	dx, dy = dx/l, dy/l
	nx, ny := -dy, dx
	const w = 3.5
	return (&ui.Path{}).MoveTo(x-dx*headLen+nx*w, y-dy*headLen+ny*w).LineTo(x, y).
		LineTo(x-dx*headLen-nx*w, y-dy*headLen-ny*w)
}

// LabelDiagram is the label of a drawn flow, for the screen reader and the tests.
const LabelDiagram = "Flow diagram"

// View draws f, at most maxW wide, and returns true. It builds nothing and returns false when f
// is too large: more than MaxNodes nodes, past the layer limits or wider than maxW.
func View(c *ui.Context, f Flow, maxW float32, st Style) bool {
	if ok, _ := f.Fits(); !ok {
		return false
	}
	lay := Layout(f)
	if ok, _ := lay.Fits(); !ok {
		return false
	}
	boxH := max(boxMinH, st.Node.Line+18)
	pal := theme.Current()
	// Box widths from the text; a layer is as wide as its widest box.
	nw := make([]float32, len(lay.Layer)) // a virtual point has no box
	for i, n := range f.Nodes {
		w, _ := c.MeasureText(0, st.node(n.Label))
		nw[i] = max(boxMinW, float32(math.Ceil(float64(w)))+2*boxPadX)
	}
	colW := make([]float32, len(lay.Layers))
	for l, ls := range lay.Layers {
		for _, v := range ls {
			colW[l] = max(colW[l], nw[v])
		}
	}
	// The gap after a layer holds the widest label of the edges that leave it.
	gap := make([]float32, len(lay.Layers))
	for i := range gap {
		gap[i] = colGapMin
	}
	for i, e := range f.Edges {
		if e.Label == "" {
			continue
		}
		w, _ := c.MeasureText(0, st.label(e.Label))
		if !lay.Back[i] {
			gap[lay.Layer[e.From]] = max(gap[lay.Layer[e.From]], w+24)
		}
	}
	colX := make([]float32, len(lay.Layers))
	x := float32(1)
	for l := range lay.Layers {
		colX[l] = x
		x += colW[l] + gap[l]
	}
	totalW := x - gap[len(gap)-1] + 1
	if totalW > maxW {
		return false
	}
	rows := 0
	for _, ls := range lay.Layers {
		rows = max(rows, len(ls))
	}
	pitch := float32(boxH + rowGap)
	top := float32(4)
	bodyH := float32(rows)*pitch - rowGap
	hasBack := false
	for _, b := range lay.Back {
		hasBack = hasBack || b
	}
	totalH := top + bodyH + 4
	if hasBack {
		totalH += backDrop + 8
	}
	box := func(v int) ui.Rect {
		l := lay.Layer[v]
		n := len(lay.Layers[l])
		y := top + (bodyH-(float32(n)*pitch-rowGap))/2 + float32(lay.Row[v])*pitch
		if v >= len(f.Nodes) {
			return ui.Rect{X: colX[l], Y: y, W: colW[l], H: boxH} // the line crosses the layer
		}
		return ui.Rect{X: colX[l] + (colW[l]-nw[v])/2, Y: y, W: nw[v], H: boxH}
	}
	// The first and the last hop of a forward edge: a virtual point or the other end.
	firstHop := func(i int) int {
		if len(lay.Via[i]) > 0 {
			return lay.Via[i][0]
		}
		return f.Edges[i].To
	}
	lastHop := func(i int) int {
		if v := lay.Via[i]; len(v) > 0 {
			return v[len(v)-1]
		}
		return f.Edges[i].From
	}
	// Ports: the edges that leave or enter one side of a box spread 8pt apart.
	port := func(v int, out bool, ei int) float32 {
		var mine []int
		for i, e := range f.Edges {
			if lay.Back[i] {
				continue
			}
			if out && e.From == v || !out && e.To == v {
				mine = append(mine, i)
			}
		}
		sort.SliceStable(mine, func(a, b int) bool {
			if out {
				return lay.Row[firstHop(mine[a])] < lay.Row[firstHop(mine[b])]
			}
			return lay.Row[lastHop(mine[a])] < lay.Row[lastHop(mine[b])]
		})
		k := 0
		for j, i := range mine {
			if i == ei {
				k = j
			}
		}
		return (float32(k) - float32(len(mine)-1)/2) * 8
	}
	ui.Box(c).Label(LabelDiagram).Width(totalW).Height(totalH).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		at := func(b ui.Rect) ui.Rect { b.X += r.X; b.Y += r.Y; return b }
		type lbl struct {
			x, y float32
			i    int
		}
		var labels []lbl
		for i, e := range f.Edges {
			a, b := at(box(e.From)), at(box(e.To))
			var path *ui.Path
			var hx, hy, hdx, hdy, mx, my float32
			if lay.Back[i] {
				// Under the boxes: from the bottom of the source to the bottom of the target.
				x0, x1 := a.X+a.W/2, b.X+b.W/2
				y0, y1 := a.Y+a.H, b.Y+b.H
				low := r.Y + top + bodyH + backDrop
				path = (&ui.Path{}).MoveTo(x0, y0).CubeTo(x0, low, x1, low, x1, y1+1)
				hx, hy, hdx, hdy = x1, y1+1, 0, -1
				mx, my = (x0+x1)/2, low-2
			} else {
				x0, y0 := a.X+a.W, a.Y+a.H/2+port(e.From, true, i)
				x1, y1 := b.X-1, b.Y+b.H/2+port(e.To, false, i)
				path = (&ui.Path{}).MoveTo(x0, y0)
				px, py := x0, y0
				to := func(x, y float32) {
					if y == py {
						path.LineTo(x, y)
					} else {
						cx := (x - px) / 2
						path.CubeTo(px+cx, py, x-cx, y, x, y)
					}
					px, py = x, y
				}
				for _, v := range lay.Via[i] {
					vb := at(box(v))
					to(vb.X, vb.Y+vb.H/2)
					path.LineTo(vb.X+vb.W, vb.Y+vb.H/2)
					px = vb.X + vb.W
				}
				to(x1, y1)
				hdx, hdy = 1, 0
				hx, hy = x1, y1
				// The label sits on the middle of the first gap.
				l := lay.Layer[e.From]
				mx = r.X + colX[l] + colW[l] + gap[l]/2
				ny := y1
				if len(lay.Via[i]) > 0 {
					vb := at(box(lay.Via[i][0]))
					ny = vb.Y + vb.H/2
				}
				my = (y0 + ny) / 2
			}
			p.StrokePath(path, 1.2, pal.Ink30)
			p.StrokePath(head(hx, hy, hdx, hdy), 1.2, pal.Ink48)
			if e.Label != "" {
				labels = append(labels, lbl{mx, my, i})
			}
		}
		for v, n := range f.Nodes {
			b := at(box(v))
			if col, ok := stateColor(pal, n.State); ok {
				p.Fill(b, theme.DiffTint(col), boxRadius)
				p.Stroke(b, col, boxRadius, 1)
			} else {
				p.Fill(b, pal.Hover, boxRadius)
				p.Stroke(b, pal.Ink30, boxRadius, 1)
			}
			s := st.node(n.Label)
			s.Color = pal.Ink88
			w, h := p.MeasureText(0, s)
			p.RichText(b.X+(b.W-w)/2, b.Y+(b.H-h)/2, 0, s)
		}
		// Labels last, on a patch of the surface, so a line under one does not cross its text.
		for _, l := range labels {
			s := st.label(f.Edges[l.i].Label)
			s.Color, s.Background, s.PaddingX = pal.Ink48, pal.StageBase(), 3
			w, h := p.MeasureText(0, s)
			p.RichText(l.x-w/2, l.y-h/2, 0, s)
		}
	})
	return true
}
