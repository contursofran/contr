package icons

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"io/fs"
	"testing"

	"github.com/contursofran/contr/assets"
)

// docs/spec/README.md (Icons) names the line icons.
func TestEveryLineIconLoads(t *testing.T) {
	want := []string{"project", "computer", "server", "folder", "folder-open", "file-go",
		"file-md", "file", "close", "plus", "split", "search", "sidebar-right", "filter", "terminal", "settings", "download", "chevron-down",
		"todo", "todo-done", "artifact"}
	if len(LineNames) != len(want) {
		t.Fatalf("%d line names, want %d", len(LineNames), len(want))
	}
	for i, name := range want {
		if LineNames[i] != name {
			t.Errorf("LineNames[%d] = %q, want %q", i, LineNames[i], name)
		}
		if Line(name) == nil {
			t.Errorf("line icon %q does not load", name)
		}
		if _, err := fs.Stat(assets.Icons, "icons/"+name+".svg"); err != nil {
			t.Error(err)
		}
	}
	if Line("no-such-icon") != nil {
		t.Error("Line returned an icon that does not exist")
	}
}

// Each file is a 16px line drawing with a stroke of 1.2 to 1.4, in no color
// of its own (docs/spec/README.md, Icons), and it paints something.
func TestLineIconsAreLineDrawings(t *testing.T) {
	stroke := regexp.MustCompile(`stroke-width="([0-9.]+)"`)
	for _, name := range LineNames {
		data, err := assets.Icons.ReadFile("icons/" + name + ".svg")
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, `viewBox="0 0 16 16"`) {
			t.Errorf("%s: not a 16px drawing", name)
		}
		m := stroke.FindStringSubmatch(text)
		if m == nil {
			t.Errorf("%s: no stroke width", name)
			continue
		}
		if w, _ := strconv.ParseFloat(m[1], 64); w < 1.2 || w > 1.4 {
			t.Errorf("%s: stroke %v, want 1.2 to 1.4", name, w)
		}
		if strings.Contains(text, `fill="#`) || strings.Contains(text, "currentColor") {
			t.Errorf("%s: has a fill color", name)
		}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Row(c).Fill().Background(ui.RGB(0, 0, 0)).Children(func() {
				ui.Icon(c, Line(name)).Size(16, 16).TextColor(ui.RGB(255, 255, 255))
			})
		}, 16, 16)
		img, drawn := tt.Image(), false
		for y := 0; y < 16 && !drawn; y++ {
			for x := 0; x < 16; x++ {
				if img.RGBAAt(x, y).R > 0 {
					drawn = true
					break
				}
			}
		}
		if !drawn {
			t.Errorf("%s: paints nothing", name)
		}
	}
}

// The filter icon is the mock's three lines, not a funnel: it has no closed
// outline.
func TestFilterIconIsLines(t *testing.T) {
	b, err := fs.ReadFile(assets.Icons, "icons/filter.svg")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(b), "Zz") {
		t.Error("the filter icon has a closed outline (a funnel)")
	}
	if strings.Count(string(b), "M") != 3 {
		t.Errorf("the filter icon is not three lines: %s", b)
	}
}
