package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

// sheetPart is one spec'd component that the spec sheet draws (#458, D179):
// the name of its spec and its image in docs/spec, the size of the box it
// gets, in points, and the view. The data copies the spec image, so the two
// can be compared.
type sheetPart struct {
	name string
	w, h int
	view func(c *ui.Context)
}

// sheetPad is the space around a part, as in the spec images.
const sheetPad = 24

var sheetParts = []sheetPart{
	{"AgentRow", 320, 230, func(c *ui.Context) {
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Agent board polish", Model: "opus", Age: "14m", State: kit.GlyphWorking, Orchestrator: true, Selected: true})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Agent rows on ui.List", Name: "b-241", Model: "sonnet", Age: "6m", State: kit.GlyphWorking})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Checks on hc event", Name: "b-250", Model: "sonnet", Age: "2m", State: kit.GlyphNeedsYou})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Sprite atlas rebuild", Name: "b-88", Model: "deepseek", Age: "25m", CI: true, State: kit.GlyphIdle}) // waiting on CI: the idle glyph
	}},
	{"StatusGlyph", 700, 60, func(c *ui.Context) {
		glyphs := []struct {
			o    kit.GlyphOpts
			text string
		}{
			{kit.GlyphOpts{State: kit.GlyphWorking}, "working"},
			{kit.GlyphOpts{State: kit.GlyphWorkingSlow}, "working, slow"},
			{kit.GlyphOpts{State: kit.GlyphWaiting}, "waiting"},
			{kit.GlyphOpts{State: kit.GlyphIdle}, "idle"},
			{kit.GlyphOpts{State: kit.GlyphNeedsYou}, "needs you"},
			{kit.GlyphOpts{State: kit.GlyphWorking, Claude: true}, "orchestrating"},
			{kit.GlyphOpts{State: kit.GlyphIdle, Claude: true}, "orchestrator idle"},
			{kit.GlyphOpts{State: kit.GlyphNeedsYou, Claude: true}, "orchestrator asks"},
		}
		for line := range 2 {
			ui.Row(c).Height(32).Children(func() {
				for _, g := range glyphs[line*4 : line*4+4] {
					ui.Row(c).Width(160).Gap(8).AlignItems(ui.Center).Children(func() {
						kit.StatusGlyph(c, g.o)
						ui.Text(c, g.text).TextColor(theme.Current().Ink62)
					})
				}
			})
		}
	}},
	{"IconButton", 520, 40, func(c *ui.Context) {
		on := true
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			kit.IconButton(c, kit.IconButtonOpts{Icon: "sidebar-right", Label: "Hide files"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "search", Label: "Search"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: "New tab"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "sidebar-right", Label: "Files", On: &on})
			ui.Text(c, "pressed").TextColor(theme.Current().Ink50)
			kit.IconButton(c, kit.IconButtonOpts{Icon: "settings", Label: "Settings", Small: true})
			ui.Text(c, "24px footer").TextColor(theme.Current().Ink50)
			kit.IconButton(c, kit.IconButtonOpts{Icon: "close", Label: "Close", Small: true})
			ui.Text(c, "close, 16px").TextColor(theme.Current().Ink50)
		})
	}},
	{"DiffSummary", 500, 30, func(c *ui.Context) {
		ui.Row(c).Gap(32).AlignItems(ui.Center).Children(func() {
			kit.DiffSummary(c, kit.DiffSummaryOpts{Added: 120, Removed: 126, Files: 9})
			kit.DiffSummary(c, kit.DiffSummaryOpts{Added: 31, Files: 1})
			kit.DiffSummary(c, kit.DiffSummaryOpts{})
		})
	}},
	{"UsageMeter", 258, 130, func(c *ui.Context) {
		kit.UsageMeter(c, []kit.Meter{
			{Label: "Context", Value: "90k of 200k 45%", Percent: 45, Has: true},
			{Label: "Claude", Value: "38%", Percent: 38, Has: true},
			{Label: "Codex", Value: "12%", Percent: 12, Has: true},
		})
	}},
}

var sheetFonts sync.Once

// sheetView is a part as the app shows it: on the ground, in the app's fonts.
func sheetView(p sheetPart) func(c *ui.Context) {
	return func(c *ui.Context) {
		ui.Column(c).Width(float32(p.w + 2*sheetPad)).Height(float32(p.h + 2*sheetPad)).Padding(sheetPad).
			Background(theme.Current().Ground).Font(theme.FontFamilySans).
			FontSize(theme.FontSize).Children(func() { p.view(c) })
	}
}

// TestSpecSheet renders each part of sheetParts in Dark and Light and fails
// when one draws nothing. With CONTR_SHEET set (scripts/sheet.sh sets it) it
// also writes tmp/sheet/<part>-<theme>.png: the spec image on top, contr's
// render below. CONTR_SHEET is "all" or a comma list of part names.
func TestSpecSheet(t *testing.T) {
	sheetFonts.Do(func() {
		for _, f := range []struct{ file, family string }{
			{"JetBrainsMono-Regular.ttf", theme.FontFamily}, {"JetBrainsMono-Bold.ttf", theme.FontFamily},
			{"Inter-Regular.ttf", theme.FontFamilySans}, {"Inter-Medium.ttf", theme.FontFamilySans},
		} {
			data, err := assets.Fonts.ReadFile("fonts/" + f.file)
			if err == nil {
				err = ui.RegisterFont(data, f.family)
			}
			if err != nil {
				t.Fatalf("font %s: %v", f.file, err)
			}
		}
	})
	want := os.Getenv("CONTR_SHEET")
	names := strings.Split(want, ",")
	out := filepath.Join("..", "..", "tmp", "sheet")
	for _, tc := range themeCases {
		themetest.Use(t, tc.src)
		for _, p := range sheetParts {
			img := ui.Render(sheetView(p), p.w+2*sheetPad, p.h+2*sheetPad, 1)
			if inked(img, tc.pal.Ground) < 50 {
				t.Errorf("%s %s: the render is empty", p.name, tc.name)
			}
			if want == "" || (want != "all" && !slices.Contains(names, p.name)) {
				continue
			}
			sheet := sheetImage(t, p, tc.name)
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			path, err := filepath.Abs(filepath.Join(out, p.name+"-"+tc.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			writePNG(t, path, sheet)
			t.Logf("sheet: %s", path)
		}
	}
	if want != "" && want != "all" {
		for _, n := range names {
			if !slices.ContainsFunc(sheetParts, func(p sheetPart) bool { return p.name == n }) {
				t.Errorf("sheet: no part %q", n)
			}
		}
	}
}

// sheetImage draws the spec image of the part over contr's render, each
// under a caption.
func sheetImage(t *testing.T, p sheetPart, themeName string) *image.RGBA {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", "images", p.name+"-"+themeName+".png"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ui.DecodeBitmap(data)
	if err != nil {
		t.Fatal(err)
	}
	sw, sh := ui.ImageSize(spec)
	w := max(int(sw), p.w+2*sheetPad)
	const caption = 28
	h := int(sh) + p.h + 2*sheetPad + 2*caption
	return ui.Render(func(c *ui.Context) {
		ui.Column(c).Width(float32(w)).Height(float32(h)).Background(theme.Current().Ground).
			Font(theme.FontFamilySans).FontSize(theme.FontSize).Children(func() {
			label := func(s string) {
				ui.Row(c).Height(caption).Padding(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s).TextColor(theme.Current().Ink100)
				})
			}
			label("spec: docs/spec/images/" + p.name + "-" + themeName + ".png")
			ui.Image(c, spec)
			label("contr: " + p.name + ", " + themeName)
			sheetView(p)(c)
		})
	}, w, h, 1)
}

// inked counts the pixels that differ from the ground.
func inked(img *image.RGBA, ground ui.Color) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !near(rgb(img, float32(x), float32(y)), ground, 16) {
				n++
			}
		}
	}
	return n
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
