package kit

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/contursofran/contr/assets"
)

// rows prints a grid as lines of # and .
func rows(g [][]bool) []string {
	out := make([]string, len(g))
	for i, r := range g {
		var b strings.Builder
		for _, on := range r {
			if on {
				b.WriteByte('#')
			} else {
				b.WriteByte('.')
			}
		}
		out[i] = b.String()
	}
	return out
}

func TestMascotGridOfTheBanner(t *testing.T) {
	g, err := MascotGrid(MascotBanner[:])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"...############...",
		"...##.######.##...",
		".################.",
		"...############...",
		"....#.#....#.#....",
		"..................",
	}
	got := rows(g)
	if len(got) != MascotRows || len(got[0]) != MascotCols {
		t.Fatalf("grid is %dx%d, want %dx%d", len(got[0]), len(got), MascotCols, MascotRows)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d is %s, want %s", i, got[i], want[i])
		}
	}
	// The ink bounds that mascotRect uses.
	left, right, bottom := MascotCols, -1, -1
	for y, r := range g {
		for x, on := range r {
			if on {
				left, right, bottom = min(left, x), max(right, x), max(bottom, y)
			}
		}
	}
	if left != mascotInkLeft || right-left+1 != mascotInkCols || bottom+1 != mascotInkRows || !strings.Contains(got[0], "#") {
		t.Errorf("ink is columns %d to %d, rows 0 to %d; mascotRect uses %d, %d columns, %d rows",
			left, right, bottom, mascotInkLeft, mascotInkCols, mascotInkRows)
	}
}

func TestMascotGridOfEachBlockCharacter(t *testing.T) {
	cases := []struct {
		ch   string
		want [2]string
	}{
		{" ", [2]string{"..", ".."}},
		{"▘", [2]string{"#.", ".."}},
		{"▝", [2]string{".#", ".."}},
		{"▖", [2]string{"..", "#."}},
		{"▗", [2]string{"..", ".#"}},
		{"▀", [2]string{"##", ".."}},
		{"▄", [2]string{"..", "##"}},
		{"▌", [2]string{"#.", "#."}},
		{"▐", [2]string{".#", ".#"}},
		{"▚", [2]string{"#.", ".#"}},
		{"▞", [2]string{".#", "#."}},
		{"▛", [2]string{"##", "#."}},
		{"▜", [2]string{"##", ".#"}},
		{"▙", [2]string{"#.", "##"}},
		{"▟", [2]string{".#", "##"}},
		{"█", [2]string{"##", "##"}},
	}
	for _, c := range cases {
		g, err := MascotGrid([]string{c.ch})
		if err != nil {
			t.Errorf("%q: %v", c.ch, err)
			continue
		}
		if got := rows(g); got[0] != c.want[0] || got[1] != c.want[1] {
			t.Errorf("%q is %v, want %v", c.ch, got, c.want)
		}
	}
}

func TestMascotGridPadsShortLines(t *testing.T) {
	g, err := MascotGrid([]string{"█", "▘ ▝"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"##....", "##....", "#....#", "......"}
	for i, r := range rows(g) {
		if r != want[i] {
			t.Errorf("row %d is %s, want %s", i, r, want[i])
		}
	}
}

func TestMascotGridRejectsOtherCharacters(t *testing.T) {
	if _, err := MascotGrid([]string{"█x"}); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("err %v, want an error for line 1", err)
	}
}

// The SVG draws the banner, cell for cell: one 1×1 rect per pixel, in a
// viewBox of the grid, with sharp edges and one fill.
func TestMascotSVGIsTheBanner(t *testing.T) {
	data, err := assets.Icons.ReadFile("icons/claude-mascot.svg")
	if err != nil {
		t.Fatal(err)
	}
	type rect struct {
		X    string `xml:"x,attr"`
		Y    string `xml:"y,attr"`
		W    string `xml:"width,attr"`
		H    string `xml:"height,attr"`
		Fill string `xml:"fill,attr"`
	}
	var raw struct {
		ViewBox string `xml:"viewBox,attr"`
		Shape   string `xml:"shape-rendering,attr"`
		Fill    string `xml:"fill,attr"`
		Rects   []rect `xml:"rect"`
	}
	if err := xml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.ViewBox != "0 0 18 6" || raw.Shape != "crispEdges" || raw.Fill == "" {
		t.Errorf("svg has viewBox %q, shape-rendering %q, fill %q", raw.ViewBox, raw.Shape, raw.Fill)
	}
	g, err := MascotGrid(MascotBanner[:])
	if err != nil {
		t.Fatal(err)
	}
	drawn := make([][]bool, MascotRows)
	for i := range drawn {
		drawn[i] = make([]bool, MascotCols)
	}
	for _, r := range raw.Rects {
		if r.W != "1" || r.H != "1" || r.Fill != "" {
			t.Errorf("rect %+v, want a 1×1 cell in the one fill", r)
			continue
		}
		var x, y int
		if _, err := fmt.Sscan(r.X+" "+r.Y, &x, &y); err != nil || x < 0 || x >= MascotCols || y < 0 || y >= MascotRows {
			t.Errorf("rect at %s,%s is not a cell of the grid", r.X, r.Y)
			continue
		}
		if drawn[y][x] {
			t.Errorf("cell %d,%d is drawn twice", x, y)
		}
		drawn[y][x] = true
	}
	want, got := rows(g), rows(drawn)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("svg row %d is %s, want %s", i, got[i], want[i])
		}
	}
}
