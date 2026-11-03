package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoHerdrBoardImport keeps the code that read the herdr-agents mod's
// board out of the app (D47).
func TestNoHerdrBoardImport(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/internal/board") {
			t.Errorf("internal/app depends on %s", dep)
		}
	}
}
