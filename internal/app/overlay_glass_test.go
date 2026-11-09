package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A floating surface with the glass-float fill is 72-80% opaque, so the
// terminal text under it shows through unless the surface blurs what is
// behind it (#536). The test tests the source because the view tester draws
// no materials. Each file that fills a surface with glass-float must also ask
// for glass.Blur (the notch puts the blur on the parent of its inner fill).
func TestGlassFloatSurfacesBlurWhatIsBehind(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if !strings.Contains(src, "Background(p.GlassFloat)") {
			continue
		}
		found++
		if !strings.Contains(src, "glass.Blur{") {
			t.Errorf("%s: glass-float fill without Material(glass.Blur{...}): the terminal shows through", f)
		}
	}
	if found == 0 {
		t.Error("no file uses glass-float: the guard checks nothing")
	}
}
