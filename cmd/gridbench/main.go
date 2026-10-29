//go:build bench

// Command gridbench draws a terminal-sized grid with the terminal painter. It
// is the workload of the grid test in docs/BENCHMARKS.md, so the numbers can
// be compared with the MyGo column there. Modes: idle (the grid does not
// change), scroll (one new line per frame), churn (every cell changes) and
// sample (a fixed surface with colors, box glyphs, underlines and a cursor,
// for a screenshot).
//
//	go run -tags bench ./cmd/gridbench -mode scroll
package main

import (
	"flag"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/terminal"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

const cols, rows = 200, 60

// The cell size of the earlier MyGo test, so the grid fits the same window.
var metrics = terminal.Metrics{CellW: 7.2, CellH: 15.8}

var palette = []uint32{
	0xd0d0d0, 0xe06c75, 0x98c379, 0xe5c07b, 0x61afef, 0xc678dd, 0x56b6c2, 0xabb2bf,
}

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789 -_=+()[]{}<>/\\:;.,"

type state struct {
	surface    endpoint.Surface
	r          *rand.Rand
	mode       string
	cache      terminal.Cache
	highlights []terminal.Highlight
	frames     int
	start      time.Time
}

// randomRow makes a row of runs of 4-25 characters, one color each, and a
// background on one run in eight.
func (s *state) randomRow() endpoint.Row {
	row := make(endpoint.Row, 0, cols)
	for len(row) < cols {
		n := min(4+s.r.Intn(22), cols-len(row))
		fg := 0x02000000 | palette[s.r.Intn(len(palette))]
		var bg uint32
		if s.r.Intn(8) == 7 {
			bg = 0x022c313a
		}
		for i := 0; i < n; i++ {
			row = append(row, endpoint.Cell{Text: string(alphabet[s.r.Intn(len(alphabet))]), Fg: fg, Bg: bg})
		}
	}
	return row
}

func (s *state) view(c *ui.Context) {
	rs := s.surface.Rows
	switch s.mode {
	case "scroll":
		// The rows that stay are the same slices, as herdr sends them.
		copy(rs, rs[1:])
		rs[rows-1] = s.randomRow()
	case "churn":
		for i := range rs {
			rs[i] = s.randomRow()
		}
	}
	s.frames++
	if d := time.Since(s.start); d > 4*time.Second {
		log.Printf("fps=%.1f", float64(s.frames)/d.Seconds())
		s.frames, s.start = 0, time.Now()
	}
	ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, theme.Color(theme.BgTerminal), 0)
		terminal.Paint(p, ui.Point{X: r.X, Y: r.Y}, metrics, &s.surface, s.highlights, terminal.Typing{Focused: true}, &s.cache)
	})
}

func main() {
	mode := flag.String("mode", "idle", "idle | scroll | churn | sample")
	flag.Parse()
	s := &state{r: rand.New(rand.NewSource(1)), mode: *mode, start: time.Now()}
	s.surface = endpoint.Surface{Size: endpoint.GridSize{Cols: cols, Rows: rows}}
	width, height := 1500, 950
	if *mode == "sample" {
		s.sample()
		width, height = 620, 360
		metrics = terminal.Metrics{CellW: 7.8, CellH: 20}
	} else {
		for i := 0; i < rows; i++ {
			s.surface.Rows = append(s.surface.Rows, s.randomRow())
		}
	}
	// The app will embed its fonts in a later slice; the benchmark reads them
	// from the checkout, so it paints with the font the app will use.
	for _, face := range []string{"Regular", "Bold", "Italic", "BoldItalic"} {
		data, err := os.ReadFile(filepath.Join("assets", "fonts", "JetBrainsMono-"+face+".ttf"))
		if err != nil {
			log.Printf("font %s: %v (run from the repository root)", face, err)
			continue
		}
		if err := ui.RegisterFont(data, theme.FontFamily); err != nil {
			log.Printf("font %s: %v", face, err)
		}
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "contr grid " + *mode, Width: width, Height: height, Content: ui.View(s.view)})
		if *mode == "scroll" || *mode == "churn" {
			// 60 updates per second, like the earlier tests.
			go func() {
				for range time.Tick(time.Second / 60) {
					win.Invalidate()
				}
			}()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
