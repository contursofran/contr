package kit

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTheKeyChipSitsOnTheButtonCornerAndMovesNothing(t *testing.T) {
	o := IconButtonOpts{Icon: "search", Label: "Search", Key: "⌘F", OnClick: func() {}}
	plain := ui.NewTester(buttonView(o), 120, 80)
	before, _ := plain.Find("Search")
	o.Hint = "F"
	tt := ui.NewTester(buttonView(o), 120, 80)
	r, ok := tt.Find("Search")
	if !ok || r != before {
		t.Fatalf("button %v with the chip, %v without", r, before)
	}
	chip, ok := tt.Find(LabelKeyHint)
	if !ok || !slices.Contains(tt.Texts(), "F") {
		t.Fatalf("no chip: %q", tt.Texts())
	}
	// Centered on the bottom right corner.
	if cx, cy := chip.X+chip.W/2, chip.Y+chip.H/2; cx != r.X+r.W || cy != r.Y+r.H {
		t.Errorf("chip center %v,%v, corner %v,%v", cx, cy, r.X+r.W, r.Y+r.H)
	}
	if chip.H != SizeKeyHint || chip.W < SizeKeyHint {
		t.Errorf("chip %vx%v", chip.W, chip.H)
	}
	// The chip takes no click: the button under it gets it.
	clicks := 0
	o.OnClick = func() { clicks++ }
	tt = ui.NewTester(buttonView(o), 120, 80)
	chip, _ = tt.Find(LabelKeyHint)
	tt.ClickAt(chip.X+chip.W/4, chip.Y+chip.H/4)
	if clicks != 1 {
		t.Errorf("a click on the chip over the button: %d clicks", clicks)
	}
}

func TestNoChipWithoutAHintOrOnADisabledButton(t *testing.T) {
	for _, o := range []IconButtonOpts{
		{Icon: "search", Label: "Search"},
		{Icon: "search", Label: "Search", Hint: "F", Disabled: true},
	} {
		tt := ui.NewTester(buttonView(o), 120, 80)
		if _, ok := tt.Find(LabelKeyHint); ok {
			t.Errorf("%+v: a chip shows", o)
		}
	}
}
