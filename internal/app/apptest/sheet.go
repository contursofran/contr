package apptest

// The spec sheet (#458, D179): each package under internal/app that draws a spec'd component has a
// TestSpecSheet in its sheet_test.go that gives its parts to [SpecSheet]. scripts/sheet.sh runs
// them all.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

// SheetPart is one spec'd component that the spec sheet draws: the name of its spec and its image
// in docs/spec, the size of the box it gets, in points, and the view. The data copies the spec
// image, so the two can be compared.
type SheetPart struct {
	Name string
	W, H int
	View func(c *ui.Context)
}

// SheetPad is the space around a part, as in the spec images.
const SheetPad = 24

var sheetFonts sync.Once

var sheetThemes = []struct {
	name string
	src  mygo.ThemeSource
	pal  *theme.Palette
}{
	{"dark", mygo.ThemeDark, theme.Dark},
	{"light", mygo.ThemeLight, theme.Light},
}

// sheetView is a part as the app shows it: on the ground, in the app's fonts and MyGo theme.
func sheetView(p SheetPart) func(c *ui.Context) {
	return func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Width(float32(p.W + 2*SheetPad)).Height(float32(p.H + 2*SheetPad)).Padding(SheetPad).
			Background(theme.Current().Ground).Font(theme.FontFamilySans).
			FontSize(theme.FontSize).Children(func() { p.View(c) })
	}
}

// SpecSheet renders each part in Dark and Light and fails when one draws nothing. With
// CONTR_SHEET set (scripts/sheet.sh sets it) it also writes tmp/sheet/<part>-<theme>.png: the
// spec image on top, contr's render below. CONTR_SHEET is "all" or a comma list of part names; a
// name of a part of another package is skipped here (scripts/sheet.sh reports a name that no
// package has). noImage lists the parts that may have no spec image yet; a missing image of any
// other part fails the test.
func SpecSheet(t *testing.T, parts []SheetPart, noImage []string) {
	t.Helper()
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
	root := repoRoot(t)
	want := os.Getenv("CONTR_SHEET")
	names := strings.Split(want, ",")
	out := filepath.Join(root, "tmp", "sheet")
	for _, tc := range sheetThemes {
		themetest.Use(t, tc.src)
		for _, p := range parts {
			img := ui.Render(sheetView(p), p.W+2*SheetPad, p.H+2*SheetPad, 1)
			if inked(img, tc.pal.Ground) < 50 {
				t.Errorf("%s %s: the render is empty", p.Name, tc.name)
			}
			if want == "" || (want != "all" && !slices.Contains(names, p.Name)) {
				continue
			}
			sheet := sheetImage(t, root, p, tc.name, noImage)
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(out, p.Name+"-"+tc.name+".png")
			writePNG(t, path, sheet)
			t.Logf("sheet: %s", path)
		}
	}
}

// repoRoot is the folder of go.mod above the test's folder.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test folder")
		}
		dir = up
	}
}

// sheetImage draws the spec image of the part over contr's render, each under a caption.
func sheetImage(t *testing.T, root string, p SheetPart, themeName string, noImage []string) *image.RGBA {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "spec", "images", p.Name+"-"+themeName+".png"))
	var spec *ui.Bitmap
	var sw, sh float32
	switch {
	case err == nil:
		if spec, err = ui.DecodeBitmap(data); err != nil {
			t.Fatal(err)
		}
		sw, sh = ui.ImageSize(spec)
	case os.IsNotExist(err) && slices.Contains(noImage, p.Name):
	default:
		t.Fatalf("%s: spec image: %v", p.Name, err)
	}
	w := max(int(sw), p.W+2*SheetPad)
	const caption = 28
	h := int(sh) + p.H + 2*SheetPad + 2*caption
	specNote := "spec: docs/spec/images/" + p.Name + "-" + themeName + ".png"
	if spec == nil {
		specNote = "spec: no image yet; the spec is docs/spec/" + p.Name + ".md"
	}
	return ui.Render(func(c *ui.Context) {
		ui.Column(c).Width(float32(w)).Height(float32(h)).Background(theme.Current().Ground).
			Font(theme.FontFamilySans).FontSize(theme.FontSize).Children(func() {
			label := func(s string) {
				ui.Row(c).Height(caption).Padding(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s).TextColor(theme.Current().Ink100)
				})
			}
			label(specNote)
			if spec != nil {
				ui.Image(c, spec)
			}
			label("contr: " + p.Name + ", " + themeName)
			sheetView(p)(c)
		})
	}, w, h, 1)
}

// inked counts the pixels that differ from the ground by more than 16 in a channel.
func inked(img *image.RGBA, ground ui.Color) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if d(p.R, ground.R) > 16 || d(p.G, ground.G) > 16 || d(p.B, ground.B) > 16 {
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
