package kit

import "testing"

func TestDiffSummaryParts(t *testing.T) {
	for _, c := range []struct {
		o                     DiffSummaryOpts
		added, removed, files string
	}{
		{DiffSummaryOpts{Added: 120, Removed: 126, Files: 9}, "+120", "−126", "9 files"},
		{DiffSummaryOpts{Added: 4, Files: 1}, "+4", "", "1 file"},
		{DiffSummaryOpts{Removed: 2, Files: 1}, "+0", "−2", "1 file"},
	} {
		a, r, f := DiffSummaryParts(c.o)
		if a != c.added || r != c.removed || f != c.files {
			t.Errorf("%+v: got %q %q %q, want %q %q %q", c.o, a, r, f, c.added, c.removed, c.files)
		}
	}
	// The minus is U+2212, not a hyphen.
	if _, r, _ := DiffSummaryParts(DiffSummaryOpts{Removed: 1}); r[:3] != "−" {
		t.Errorf("removed count %q does not start with U+2212", r)
	}
}

func TestDiffSummaryLabel(t *testing.T) {
	for _, c := range []struct {
		o    DiffSummaryOpts
		want string
	}{
		{DiffSummaryOpts{Added: 120, Removed: 126, Files: 9}, "120 lines added, 126 removed, 9 files. Open review"},
		{DiffSummaryOpts{Added: 4, Files: 1}, "4 lines added, 1 file. Open review"},
		{DiffSummaryOpts{Added: 1, Removed: 1, Files: 1}, "1 line added, 1 removed, 1 file. Open review"},
	} {
		if got := DiffSummaryLabel(c.o); got != c.want {
			t.Errorf("label %q, want %q", got, c.want)
		}
	}
}

func TestDiffSummaryHidden(t *testing.T) {
	if !(DiffSummaryOpts{}).Hidden() {
		t.Error("no change must hide")
	}
	for _, o := range []DiffSummaryOpts{{Added: 1}, {Removed: 1}, {Files: 1}} {
		if o.Hidden() {
			t.Errorf("%+v must show", o)
		}
	}
}
