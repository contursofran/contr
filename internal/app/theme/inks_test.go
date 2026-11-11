package theme

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// textSurfaces are the surfaces where text sits: the window and its wings (the
// window glass over the ground, D250), the stage, the fills of a selected row
// or tab, and the floating panels.
func textSurfaces(p *Palette) map[string]ui.Color {
	w := wing(p)
	return map[string]ui.Color{
		"window and wings": w,
		"stage":            p.StageBase(),
		"selected-quiet":   Over(p.SelectedQuiet, w),
		"selected":         Over(p.Selected, w),
		"float-solid":      p.FloatSolid,
		"glass-float":      Over(p.GlassFloat, w),
	}
}

// Each text ink has 4.5:1 on each surface where text sits, in both themes
// (D268). ink-30 is for marks only, so it has no such floor.
func TestTextInksAreReadable(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		inks := map[string]ui.Color{"ink-100": p.Ink100, "ink-88": p.Ink88, "ink-64": p.Ink64, "ink-48": p.Ink48}
		for name, ink := range inks {
			for sname, bg := range textSurfaces(p) {
				if got := contrast(Over(ink, bg), bg); got < 4.5 {
					t.Errorf("dark=%v: %s has %.2f:1 on %s, want 4.5:1", p.Dark, name, got, sname)
				}
			}
		}
	}
}

// The five inks step down in contrast: each one reads weaker than the one
// above it, on the window, in both themes (D268).
func TestInksStepDown(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		w := wing(p)
		order := []ui.Color{p.Ink100, p.Ink88, p.Ink64, p.Ink48, p.Ink30}
		for i := 1; i < len(order); i++ {
			hi, lo := contrast(Over(order[i-1], w), w), contrast(Over(order[i], w), w)
			if lo >= hi {
				t.Errorf("dark=%v: ink %d has %.2f:1, not below ink %d with %.2f:1", p.Dark, i, lo, i-1, hi)
			}
		}
	}
}

// inkMark is the comment that lets a line give ink-30 to a glyph drawn as
// text that is a mark, not a word (a drag grip).
const inkMark = "// ink-30: a mark"

// ink30Text finds a line that gives ink-30 to text: a text or placeholder
// color, a text line, or a field whose name says text. An icon line
// (ui.Icon) also sets a text color, but an icon is a mark.
var ink30Text = regexp.MustCompile(`(TextColor|PlaceholderColor|TextLine)\(.*\bInk30\b|Text\w*:\s*[\w.]*\bInk30\b`)

func isInk30Text(line string) bool {
	return ink30Text.MatchString(line) && !strings.Contains(line, "ui.Icon(") && !strings.Contains(line, inkMark)
}

// No text uses ink-30, as far as the source shows (D268): the scan finds ink-30
// given to text in the views. A color passed through a variable is not seen.
func TestInk30IsNoText(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if isInk30Text(line) {
				t.Errorf("%s:%d gives ink-30 to text: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The scan finds the shapes of text it looks for, and lets marks pass.
func TestInk30ScanFindsText(t *testing.T) {
	for _, line := range []string{
		`ui.Text(c, s).TextColor(p.Ink30)`,
		`ui.TextInput(c, &v).PlaceholderColor(pal.Ink30)`,
		`TextLine(c, a, b, theme.Current().Ink30)`,
		`QuoteText: pal.Ink30,`,
	} {
		if !isInk30Text(line) {
			t.Errorf("scan misses %q", line)
		}
	}
	for _, line := range []string{
		`QuoteBar: pal.Ink30, QuoteText: pal.Ink88,`,
		`ui.Icon(c, s).Size(16, 16).TextColor(p.Ink30)`,
		`ui.Text(c, grip).TextColor(p.Ink30) ` + inkMark,
		`Scrollbar: p.Ink30,`,
		`ui.Text(c, s).TextColor(p.Ink48)`,
	} {
		if isInk30Text(line) {
			t.Errorf("scan flags the mark %q", line)
		}
	}
}
