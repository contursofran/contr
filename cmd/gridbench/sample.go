//go:build bench

package main

import (
	"github.com/contursofran/contr/internal/app/terminal"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// sample fills the surface with one of each look the painter has.
func (s *state) sample() {
	row := func(text string, fg, bg uint32, attrs uint16) endpoint.Row {
		var r endpoint.Row
		for _, ch := range text {
			r = append(r, endpoint.Cell{Text: string(ch), Fg: fg, Bg: bg, Attrs: attrs})
		}
		return r
	}
	join := func(parts ...endpoint.Row) endpoint.Row {
		var r endpoint.Row
		for _, p := range parts {
			r = append(r, p...)
		}
		return r
	}
	named := func(i uint32) uint32 { return i } // named colors 1..16
	rgb := func(v uint32) uint32 { return 0x02000000 | v }
	styles := func(label string, attrs uint16) endpoint.Row {
		return join(row(label+" ", 0, 0, 0), row("underline", rgb(0xe0c070), 0, attrs))
	}
	const ul = terminal.Underline
	st := func(n uint16) uint16 { return ul | n<<terminal.UnderlineStyleShift }
	s.surface.Rows = []endpoint.Row{
		join(row("red ", named(2), 0, 0), row("green ", named(3), 0, 0), row("blue ", named(5), 0, 0),
			row("rgb ", rgb(0xff8800), 0, 0), row(" on bg ", rgb(0x101010), rgb(0x6ca8e8), 0)),
		join(row("bold ", 0, 0, terminal.Bold), row("italic ", 0, 0, terminal.Italic), row("dim ", 0, 0, terminal.Dim),
			row("strike ", 0, 0, terminal.Strikethrough), row("reverse", 0, 0, terminal.Reversed)),
		styles("single ", st(1)),
		styles("double ", st(2)),
		styles("curly  ", st(3)),
		styles("dotted ", st(4)),
		styles("dashed ", st(5)),
		row("┌──────┬──────┐ ╭──╮ ░▒▓█ ▀▄▌▐", 0, 0, 0),
		row("│ box  │ draw │ ╰──╯ ▁▂▃▄▅▆▇", 0, 0, 0),
		row("╞══════╪══════╡ -> != <= ", 0, 0, 0),
		row("selected text here", 0, 0, 0),
		row("└──────┴──────┘", 0, 0, 0),
	}
	for i, r := range s.surface.Rows {
		for len(r) < 40 {
			r = append(r, endpoint.Cell{Text: " "})
		}
		s.surface.Rows[i] = r
	}
	s.surface.Size = endpoint.GridSize{Cols: 40, Rows: uint16(len(s.surface.Rows))}
	s.surface.Cursor = &endpoint.Cursor{X: 13, Y: 10, Visible: true, Shape: 0}
	s.highlights = []terminal.Highlight{{Row: 10, Start: 0, End: 8}}
}
