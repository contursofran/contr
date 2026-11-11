package diagramspike

import "sort"

// The layout rule of the spike: layers left to right from a longest-path order, the nodes of a
// layer in the order that one barycenter sweep gives, fixed box heights, box widths from the text.
// Back edges of a cycle are drawn as arcs under the boxes.

// Limits past which a diagram shows as its code block.
const (
	MaxNodes  = 12
	MaxLayers = 6
	MaxRows   = 4 // nodes in one layer
	MaxLabel  = 28
)

// Layered is a flow cut into layers: Layer[i] is the layer of node i, Row[i] its place in it.
// Ids from len(f.Nodes) on are virtual: the points where an edge that skips layers crosses one.
type Layered struct {
	Layer, Row []int
	Layers     [][]int
	Back       []bool  // per edge: an edge that goes back (a cycle), drawn as an arc
	Via        [][]int // per edge: its virtual points, in order
}

// Fits reports whether the flow is small enough to draw, and the reason when it is not.
func (f Flow) Fits() (bool, string) {
	switch {
	case len(f.Nodes) > MaxNodes:
		return false, "more than 12 nodes"
	}
	for _, n := range f.Nodes {
		if len([]rune(n.Label)) > MaxLabel {
			return false, "a label longer than 28 characters"
		}
	}
	return true, ""
}

// Layout cuts f into layers.
func Layout(f Flow) Layered {
	n := len(f.Nodes)
	out := make([]([]int), n)
	for i, e := range f.Edges {
		out[e.From] = append(out[e.From], i)
	}
	// Back edges: an edge to a node on the DFS stack, from the nodes in the order written.
	back := make([]bool, len(f.Edges))
	mark := make([]int, n) // 0 new, 1 on the stack, 2 done
	var dfs func(v int)
	dfs = func(v int) {
		mark[v] = 1
		for _, ei := range out[v] {
			w := f.Edges[ei].To
			switch mark[w] {
			case 0:
				dfs(w)
			case 1:
				back[ei] = true
			}
		}
		mark[v] = 2
	}
	for v := range n {
		if mark[v] == 0 {
			dfs(v)
		}
	}
	// Longest path from the sources, without back edges; nodes are in DFS post order reversed.
	layer := make([]int, n)
	for changed := true; changed; {
		changed = false
		for i, e := range f.Edges {
			if !back[i] && e.From != e.To && layer[e.To] < layer[e.From]+1 {
				layer[e.To], changed = layer[e.From]+1, true
			}
		}
	}
	nl := 0
	for _, l := range layer {
		nl = max(nl, l+1)
	}
	// Virtual points on edges that skip layers, and the segments of all forward edges.
	via := make([][]int, len(f.Edges))
	type seg struct{ from, to int }
	var segs []seg
	for i, e := range f.Edges {
		if back[i] || e.From == e.To {
			continue
		}
		prev := e.From
		for l := layer[e.From] + 1; l < layer[e.To]; l++ {
			v := len(layer)
			layer = append(layer, l)
			via[i] = append(via[i], v)
			segs = append(segs, seg{prev, v})
			prev = v
		}
		segs = append(segs, seg{prev, e.To})
	}
	layers := make([][]int, nl)
	for v, l := range layer {
		layers[l] = append(layers[l], v)
	}
	row := make([]int, len(layer))
	place := func() {
		for _, ls := range layers {
			for r, v := range ls {
				row[v] = r
			}
		}
	}
	place()
	// One sweep: order each layer by the mean row of the nodes that point to it.
	for l := 1; l < nl; l++ {
		bary := map[int]float64{}
		for _, v := range layers[l] {
			sum, k := 0.0, 0
			for _, sg := range segs {
				if sg.to == v {
					sum += float64(row[sg.from])
					k++
				}
			}
			if k > 0 {
				bary[v] = sum / float64(k)
			} else {
				bary[v] = float64(row[v])
			}
		}
		sort.SliceStable(layers[l], func(a, b int) bool { return bary[layers[l][a]] < bary[layers[l][b]] })
		place()
	}
	return Layered{Layer: layer, Row: row, Layers: layers, Back: back, Via: via}
}

// Fits reports whether the layers fit the limits.
func (l Layered) Fits() (bool, string) {
	if len(l.Layers) > MaxLayers {
		return false, "more than 6 layers"
	}
	for _, ls := range l.Layers {
		if len(ls) > MaxRows {
			return false, "more than 4 nodes in a layer"
		}
	}
	return true, ""
}
