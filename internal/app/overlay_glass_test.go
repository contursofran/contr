package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A floating surface with the glass-float fill is 72-80% opaque, so the
// terminal text under it shows through unless the surface blurs what is
// behind it (#536). kit.GlassFloat sets the fill and the blur together. The
// view tester draws no materials, so the test reads the source: no file
// except the helper may fill a surface with Background(p.GlassFloat).
func TestGlassFloatFillOnlyThroughTheHelper(t *testing.T) {
	const helper = "kit/glass.go"
	used := false
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		if filepath.ToSlash(path) == helper {
			used = strings.Contains(src, "p.GlassFloat") && strings.Contains(src, "glass.Blur{")
			return nil
		}
		if strings.Contains(src, "Background(p.GlassFloat)") {
			t.Errorf("%s: Background(p.GlassFloat) outside kit.GlassFloat: the terminal shows through", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Errorf("%s does not set the glass-float fill and glass.Blur together", helper)
	}
}
