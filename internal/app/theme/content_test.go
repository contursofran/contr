package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The content colors are words, so they have 4.5:1 on the stage, and on a code block where diffs
// and code use them (D261).
func TestContentColorsAreReadable(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		words := map[string]ui.Color{"content-done": p.ContentDone, "content-running": p.ContentRunning,
			"content-waiting": p.ContentWaiting, "content-failed": p.ContentFailed, "content-info": p.ContentInfo}
		for name, c := range words {
			if got := contrast(Over(c, p.StageBase()), p.StageBase()); got < 4.5 {
				t.Errorf("dark=%v: %s has %.2f:1 on the stage, want 4.5:1", p.Dark, name, got)
			}
		}
		code := map[string]ui.Color{"code-keyword": p.CodeKeyword, "code-function": p.CodeFunction,
			"code-string": p.CodeString, "code-number": p.CodeNumber, "code-comment": p.CodeComment}
		for name, c := range code {
			if got := contrast(Over(c, p.CodeBase()), p.CodeBase()); got < 4.5 {
				t.Errorf("dark=%v: %s has %.2f:1 on a code block, want 4.5:1", p.Dark, name, got)
			}
		}
		// A diff line: its text in the color, on the color's tint over the code block.
		for name, c := range map[string]ui.Color{"content-done": p.ContentDone, "content-failed": p.ContentFailed} {
			bg := Over(DiffTint(c), p.CodeBase())
			if got := contrast(c, bg); got < 4.5 {
				t.Errorf("dark=%v: %s has %.2f:1 on its diff tint, want 4.5:1", p.Dark, name, got)
			}
		}
	}
}

// content-failed is the red of the chrome: blocked in Dark, blocked-text in Light, where blocked
// has less than 4.5:1 on a diff line's tint (D261).
func TestContentFailedIsTheChromeRed(t *testing.T) {
	if Dark.ContentFailed != Dark.Blocked || Light.ContentFailed != Light.BlockedText {
		t.Errorf("content-failed: Dark %v, Light %v", Dark.ContentFailed, Light.ContentFailed)
	}
}
