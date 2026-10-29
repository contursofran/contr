package icons

import (
	"io/fs"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
)

func one(s Status, size float32) func(*ui.Context) {
	return func(c *ui.Context) {
		ui.Column(c).Fill().Background(ui.RGB(0, 0, 0)).Children(func() {
			ui.Row(c).Children(func() {
				Sized(c, s, size)
			})
		})
	}
}

// Port of the Rust test labels_are_unique_and_match_design.
func TestLabelsAreUniqueAndMatchDesign(t *testing.T) {
	want := []string{"needs you", "decide", "do", "blocked", "working", "waiting", "idle", "done", "fyi", "not started"}
	labels, slugs := map[string]bool{}, map[string]bool{}
	for i, s := range All {
		if s.Label() != want[i] {
			t.Errorf("label %d is %q, want %q", i, s.Label(), want[i])
		}
		labels[s.Label()], slugs[s.Slug()] = true, true
	}
	if len(labels) != 10 || len(slugs) != 10 {
		t.Errorf("%d labels and %d slugs, want 10 of each", len(labels), len(slugs))
	}
}

// Port of the Rust test svgs_parse. All embedded files parse, and the
// parser rejects bad input, so the check means something.
func TestSVGsParse(t *testing.T) {
	files, err := fs.Glob(assets.Icons, "icons/*.svg")
	if err != nil || len(files) != 11 {
		t.Fatalf("icon files %v, err %v, want 11 files", files, err)
	}
	for _, f := range files {
		data, err := assets.Icons.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ui.ParseSVG(data); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := ui.ParseSVG([]byte("<svg")); err == nil {
		t.Error("ParseSVG accepted broken input")
	}
	for _, s := range All {
		file, _ := s.glyph()
		if svg(file) == nil {
			t.Errorf("%s has no glyph %q", s.Label(), file)
		}
	}
	if svg("working_arc") == nil {
		t.Error("no working arc")
	}
}

// Port of the Rust test needs_you_has_a_cutout: the "!" is a hole in the
// circle, so the background shows through it.
func TestNeedsYouHasACutout(t *testing.T) {
	tt := ui.NewTester(one(NeedsYou, 32), 64, 64)
	r, ok := tt.Find("needs you")
	if !ok {
		t.Fatal("icon not found")
	}
	img := tt.Image()
	at := func(x, y int) uint8 {
		return img.RGBAAt(int(r.X)+x, int(r.Y)+y).R
	}
	if got := at(15, 12); got > 40 {
		t.Errorf("inside the bar the red channel is %d, want the dark background", got)
	}
	if got := at(5, 16); got < 200 {
		t.Errorf("circle body red channel is %d, want white", got)
	}
}

// Port of the Rust test icon_is_14px.
func TestIconIs14px(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) { Icon(c, Idle) }, 100, 100)
	r, ok := tt.Find("idle")
	if !ok {
		t.Fatal("icon not found")
	}
	if r.W != theme.IconSize || r.H != theme.IconSize {
		t.Errorf("icon is %vx%v, want %vx%v", r.W, r.H, float32(theme.IconSize), float32(theme.IconSize))
	}
}

// Port of the Rust test idle_icon_requests_no_frames.
func TestIdleIconRequestsNoFrames(t *testing.T) {
	tt := ui.NewTester(one(Idle, 14), 100, 100)
	if tt.FramePending() {
		t.Error("an idle icon asks for frames")
	}
}

// Port of the Rust test working_icon_requests_a_frame.
func TestWorkingIconRequestsAFrame(t *testing.T) {
	tt := ui.NewTester(one(Working, 14), 100, 100)
	if !tt.FramePending() {
		t.Error("a working icon asks for no frames")
	}
}

// The working arc shows at most theme.SpinFPS positions a second, not one
// at each refresh of the display: at the display's rate the arc was the
// whole idle CPU of the app (M13.4). The Tester has no refresh rate, so it
// paces no frames; the image shows the steps, and the bench's frame log
// shows the frames.
func TestWorkingIconPaintsAtSpinFPS(t *testing.T) {
	tt := ui.NewTester(one(Working, 14), 20, 20)
	refresh := time.Second / 60 // the Tester's display
	last := tt.Image().Pix
	paints := 0
	for i := 0; i < 60; i++ {
		tt.Advance(refresh)
		img := tt.Image().Pix
		if string(img) != string(last) {
			paints++
		}
		last = img
	}
	if paints < theme.SpinFPS-1 || paints > theme.SpinFPS+1 {
		t.Errorf("the arc painted %d times in a second, want %d", paints, theme.SpinFPS)
	}
	if !tt.FramePending() {
		t.Error("the arc stopped asking for frames")
	}
}

func TestSpinAngleTurnsOncePerPeriod(t *testing.T) {
	base := time.UnixMilli(1_000_000 * theme.SpinPeriod.Milliseconds())
	step := time.Second / theme.SpinFPS
	for _, tc := range []struct {
		after time.Duration
		want  float32
	}{
		{0, 0},
		{step - time.Millisecond, 0},
		{step + time.Millisecond, 360 / float32(spinSteps)},
		{theme.SpinPeriod / 2, 180},
		{theme.SpinPeriod, 0},
	} {
		if got := SpinAngle(base.Add(tc.after)); got != tc.want {
			t.Errorf("after %v angle %v, want %v", tc.after, got, tc.want)
		}
	}
	if spinSteps != 42 {
		t.Errorf("%d steps a turn, want 42 (1.4 s at 30 a second)", spinSteps)
	}
}

func TestAgentStatesMapToTheirIcons(t *testing.T) {
	cases := []struct {
		in   herdr.AgentState
		want Status
	}{
		{herdr.StateIdle, Idle},
		{herdr.StateWorking, Working},
		{herdr.StateBlocked, Blocked},
		{herdr.StateDone, Done},
		{herdr.StateUnknown, Idle},
	}
	for _, c := range cases {
		if got := FromAgentState(c.in); got != c.want {
			t.Errorf("FromAgentState(%s) = %s, want %s", c.in.Label(), got.Label(), c.want.Label())
		}
	}
}
