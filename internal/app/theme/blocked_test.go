package theme

import "testing"

// The reason of a needs-you row is `blocked` text on `blocked-fill` over the
// panel (D249). It keeps 4.5:1 in both themes.
func TestBlockedReadsOnBlockedFill(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		bg := Over(p.BlockedFill, p.PanelBase())
		if got := contrast(p.Blocked, bg); got < 4.5 {
			t.Errorf("dark=%v: blocked has %.2f:1 on blocked-fill, want 4.5:1", p.Dark, got)
		}
		t.Logf("dark=%v: blocked %.2f:1 on blocked-fill", p.Dark, contrast(p.Blocked, bg))
	}
}
