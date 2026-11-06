package theme

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func channel(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func luminance(c ui.Color) float64 {
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// contrast is the WCAG contrast ratio of two opaque colors.
func contrast(a, b ui.Color) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

func TestANSIMatchesTokensMd(t *testing.T) {
	rows := specRows(t, "ANSI")
	if len(rows) != 16 {
		t.Fatalf("tokens.md has %d ANSI rows, want 16", len(rows))
	}
	for i, r := range rows {
		if r[0] != ANSIName[i] {
			t.Errorf("row %d is %q, want %q", i, r[0], ANSIName[i])
		}
		if got := Dark.ANSI[i]; got != specColor(t, r[1]) {
			t.Errorf("%s dark = %v, want %s", r[0], got, r[1])
		}
		if got := Light.ANSI[i]; got != specColor(t, r[2]) {
			t.Errorf("%s light = %v, want %s", r[0], got, r[2])
		}
	}
}

// The Light palette is readable on the Light panel: 4.5:1 for each color (D129).
func TestLightANSIIsReadableOnThePanel(t *testing.T) {
	bg := Light.PanelBase()
	for i, c := range Light.ANSI {
		if got := contrast(c, bg); got < 4.5 {
			t.Errorf("%s %v has %.2f:1 on the Light panel, want 4.5:1", ANSIName[i], c, got)
		}
	}
}

// The numbers in tokens.md match the colors.
func TestANSIContrastNumbersInTokensMd(t *testing.T) {
	for i, r := range specRows(t, "ANSI") {
		want := "Dark " + ratio(contrast(Dark.ANSI[i], Dark.PanelBase())) +
			", Light " + ratio(contrast(Light.ANSI[i], Light.PanelBase())) + " on panel."
		if !strings.Contains(r[3], want) {
			t.Errorf("%s: %q, want %q", r[0], r[3], want)
		}
	}
}

func ratio(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) + ":1" }

func TestPanelBaseIsTheFlatPanel(t *testing.T) {
	if got, want := Light.PanelBase(), ui.RGB(250, 251, 251); got != want {
		t.Errorf("Light panel = %v, want %v", got, want)
	}
	if got, want := Dark.PanelBase(), ui.RGB(20, 21, 24); got != want {
		t.Errorf("Dark panel = %v, want %v", got, want)
	}
}

func TestOverBlends(t *testing.T) {
	if got := Over(ui.RGBA(255, 255, 255, 0.5), ui.RGB(0, 0, 0)); got.R < 126 || got.R > 129 || got.A != 255 {
		t.Errorf("Over = %v, want a mid grey and opaque", got)
	}
	if got := Over(ui.RGB(10, 20, 30), ui.RGB(200, 200, 200)); got != ui.RGB(10, 20, 30) {
		t.Errorf("an opaque color changed: %v", got)
	}
}
