package chatview

// Wide blocks (docs/spec/ChatView.md, D252): prose stays in the column, at most 920px wide, while
// a table, a code block or an open work group may take the panel's whole content width when its
// content needs it.

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"github.com/contursofran/contr/internal/app/theme"
)

// Part is a piece of one text of Claude: prose, or a block that may be wide.
type Part struct {
	Text string
	Wide bool
}

// Parts cuts a Markdown text into runs of prose and the blocks that may be wide (tables and code
// blocks), in order. Each cut is at the start of a line, so a part is Markdown of its own.
func Parts(src string) []Part {
	d := parser.Parse(src)
	var out []Part
	from := 0
	add := func(to int, wide bool) {
		if s := src[from:to]; strings.TrimSpace(s) != "" {
			out = append(out, Part{Text: s, Wide: wide})
		}
		from = to
	}
	for i := range d.Children(0) {
		b := d.Block(i)
		if b.Kind != parser.Table && b.Kind != parser.CodeBlock {
			continue
		}
		add(lineStart(src, b.Start), false)
		add(lineEnd(src, b.End), true)
	}
	add(len(src), false)
	return out
}

// lineStart is the offset of the start of the line that holds offset i.
func lineStart(s string, i int) int {
	i = min(max(i, 0), len(s))
	return strings.LastIndexByte(s[:i], '\n') + 1
}

// lineEnd is the offset after the end of the line that holds offset i, its newline included.
func lineEnd(s string, i int) int {
	i = min(max(i, 0), len(s))
	if i > 0 && s[i-1] == '\n' {
		return i
	}
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(s)
}

// said is a text of Claude: its prose in the column and its wide blocks in a wide box.
func (ch *Chat) said(c *ui.Context, id, src string) {
	parts := ch.parts[id]
	if parts == nil || ch.partsOf[id] != src {
		parts = Parts(src)
		ch.parts[id], ch.partsOf[id] = parts, src
	}
	ui.Column(c).FillWidth().Gap(theme.Space12).Children(func() {
		for i, p := range parts {
			c := c.Key(i)
			text := ch.chipText(id, i, p.Text)
			if p.Wide {
				ch.wide(c, func() { markdown.View(c, text, &ch.md) })
			} else {
				column(c, func() { markdown.View(c, text, &ch.md) })
			}
		}
	})
}

// wide holds a block that may be wider than the column: as wide as its content, at least the
// column and at most the content width of the panel, centered. A block that fits in the column
// stays in it, its left edge on the column's left edge.
func (ch *Chat) wide(c *ui.Context, fn func()) {
	w := ch.contentWidth(c)
	ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
		box := ui.Column(c).MinWidth(0)
		if w > 0 {
			box.MinWidth(min(columnMax, w)).MaxWidth(w)
		} else {
			box.Grow(1).MaxWidth(columnMax)
		}
		box.Children(fn)
	})
}

// contentWidth is the width of the panel less the side insets, from the last frame; 0 before the
// first one.
func (ch *Chat) contentWidth(c *ui.Context) float32 {
	r := ch.panel.Bounds(c)
	return max(r.W-2*theme.Space20, 0)
}

// columnLeft is how far the column's left edge is from the left of the content width.
func (ch *Chat) columnLeft(c *ui.Context) float32 {
	return max((ch.contentWidth(c)-columnMax)/2, 0)
}
