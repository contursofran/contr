package contr

import (
	"os"
	"strings"
	"testing"
)

func TestVersionMatchesFile(t *testing.T) {
	b, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	if want := strings.TrimSpace(line); Version() != want {
		t.Errorf("Version() = %q, want %q", Version(), want)
	}
}

func TestVersionEmptyIsDev(t *testing.T) {
	old := versionFile
	defer func() { versionFile = old }()
	for _, s := range []string{"", "\n", "  \n0.2.0\n"} {
		versionFile = s
		if got := Version(); got != "dev" {
			t.Errorf("Version() with %q = %q, want dev", s, got)
		}
	}
	versionFile = "0.2.0\nextra\n"
	if got := Version(); got != "0.2.0" {
		t.Errorf("Version() = %q, want 0.2.0", got)
	}
}
