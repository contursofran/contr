package questions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// outlineClockStart is when the outline began in the tests. The pins are placed
// before and after it.
var outlineClockStart = time.Date(2026, 11, 5, 14, 0, 0, 0, time.Local)

type outlineFixture struct {
	o    *Outline
	sel  *state.Selection
	todo []owner.Todo
	now  time.Time
	ov   kit.TerminalOverlay
	tt   *ui.Tester
}

func pin(id string, at time.Time, text string) owner.Todo {
	return owner.Todo{ID: id, Project: "contr", Kind: "do", Text: text, Pinned: at}
}

// newOutline builds an outline of an orchestrator in the overlay layer of a
// terminal-sized area, with the orchestrator selected.
func newOutline(t *testing.T, todos ...owner.Todo) *outlineFixture {
	t.Helper()
	f := &outlineFixture{todo: todos, now: outlineClockStart, sel: state.NewSelection(nil)}
	snap := func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: apptest.OrchAddr, Project: "contr", Role: agents.RoleOrchestrator}},
			Workers:       []agents.Worker{{Address: apptest.WorkAddr, Project: "contr", Role: "builder"}},
			Todos:         f.todo,
		}
	}
	f.o = NewOutline(snap, f.sel, view.Env{})
	f.o.Now = func() time.Time { return f.now }
	f.o.start = outlineClockStart
	f.ov.Add(LabelOutline, f.o.View)
	f.sel.Select(state.Selected{Kind: state.KindOrchestrator, ID: apptest.OrchAddr})
	f.tt = ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Box(c).Fill().Children(func() { f.ov.View(c) })
	}, 900, 600)
	return f
}

// threePins is two old pins and one that came while the owner was away.
func threePins() []owner.Todo {
	return []owner.Todo{
		pin("T1", outlineClockStart.Add(-2*time.Hour), "Decide the release name\nThe tag needs a name before Friday."),
		pin("T2", outlineClockStart.Add(-time.Hour), "Review the sidebar PR"),
		pin("T3", outlineClockStart.Add(10*time.Minute), "CI is green on the notch branch\nMerge when you are ready."),
	}
}

func (f *outlineFixture) rect(t *testing.T, label string) ui.Rect {
	t.Helper()
	r, ok := f.tt.Find(label)
	if !ok {
		t.Fatalf("no %q; texts %q", label, f.tt.Texts())
	}
	return r
}

func (f *outlineFixture) hover(t *testing.T, label string) {
	t.Helper()
	r := f.rect(t, label)
	f.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	f.tt.Advance(700 * time.Millisecond)
	f.tt.Frame()
}

func TestOutlineTicksOnePerPinnedMessage(t *testing.T) {
	todos := append(threePins(),
		owner.Todo{ID: "T4", Project: "other", Text: "Another project", Pinned: outlineClockStart},
		owner.Todo{ID: "T5", Project: "contr", Text: "Done already", Pinned: outlineClockStart, Done: outlineClockStart},
	)
	got := outlineTicks(todos, "contr")
	if len(got) != 3 || got[0].ID != "T1" || got[2].ID != "T3" {
		t.Fatalf("ticks %+v, want T1 T2 T3 of the project, open", got)
	}
	if got[0].Headline != "Decide the release name" || got[0].Detail != "The tag needs a name before Friday." {
		t.Errorf("first tick: %q, %q", got[0].Headline, got[0].Detail)
	}
	if got[1].Detail != "to do" {
		t.Errorf("a message with one line says its kind: %q", got[1].Detail)
	}
	know := outlineTicks([]owner.Todo{{ID: "T6", Project: "contr", Kind: "know", From: "b-346", Text: "FYI", Pinned: outlineClockStart}}, "contr")
	if len(know) != 1 || know[0].Detail != "to know · from b-346" {
		t.Errorf("know tick: %+v", know)
	}
	for _, tk := range got {
		if tk.Width < outlineMinWidth || tk.Width > outlineMaxWidth {
			t.Errorf("%s width %v is out of %d..%d", tk.ID, tk.Width, outlineMinWidth, outlineMaxWidth)
		}
	}
	long := outlineTicks([]owner.Todo{pin("T7", outlineClockStart, strings.Repeat("long ", 80))}, "contr")
	if long[0].Width != outlineMaxWidth {
		t.Errorf("a long message gets the widest tick, got %v", long[0].Width)
	}
}

func TestOutlineShowsOneTickPerPinInTheOrchestratorTerminalOnly(t *testing.T) {
	f := newOutline(t, threePins()...)
	for _, l := range []string{"Decide the release name", "Review the sidebar PR", "CI is green on the notch branch, new"} {
		f.rect(t, l)
	}
	if _, ok := f.tt.Find(LabelOutline); !ok {
		t.Error("no column")
	}
	r := f.rect(t, "Review the sidebar PR")
	if r.W != outlineHitWidth || r.H != outlineHitHeight {
		t.Errorf("hit area %vx%v, want %dx%d", r.W, r.H, outlineHitWidth, outlineHitHeight)
	}
	col := f.rect(t, LabelOutline)
	if mid := col.Y + col.H/2; mid < 290 || mid > 310 {
		t.Errorf("the column is centered on the height 600: middle at %v", mid)
	}
	if right := col.X + col.W; right != 900-outlineInset {
		t.Errorf("the column ends %v from the right edge, want %d", 900-right, outlineInset)
	}

	for name, sel := range map[string]state.Selected{
		"a worker":   {Kind: state.KindWorker, ID: apptest.WorkAddr},
		"a terminal": {Kind: state.KindTerminal, ID: "w1:t9"},
		"a worktree": {Kind: state.KindWorktree, ID: "/x"},
	} {
		f.sel.Select(sel)
		f.tt.Frame()
		if _, ok := f.tt.Find(LabelOutline); ok || f.tt.HasText("Review the sidebar PR") {
			t.Errorf("the outline shows for %s", name)
		}
	}
	f.sel.Clear()
	f.tt.Frame()
	if _, ok := f.tt.Find(LabelOutline); ok {
		t.Error("the outline shows with no selection")
	}
}

func TestOutlineHasNoColumnWithoutPins(t *testing.T) {
	f := newOutline(t)
	if _, ok := f.tt.Find(LabelOutline); ok {
		t.Error("a column shows with no pin")
	}
	if f.ov.Empty() {
		t.Fatal("the layer is set")
	}
}

func TestOutlineNewMeansAfterTheTerminalLastHadTheFocus(t *testing.T) {
	f := newOutline(t, threePins()...)
	_, v, ok := f.o.view()
	if !ok || v.Count != 1 || v.New[0] || v.New[1] || !v.New[2] {
		t.Fatalf("view %+v: want only T3 new", v)
	}
	if !f.tt.HasText(LabelOutlineCount) {
		t.Errorf("the count shows: texts %q", f.tt.Texts())
	}

	// The owner comes back: the new tick stays until the owner leaves.
	f.now = outlineClockStart.Add(30 * time.Minute)
	f.o.Focus(apptest.OrchAddr)
	f.tt.Frame()
	if _, v, _ := f.o.view(); v.Count != 1 {
		t.Errorf("a message that came while away is still new while the terminal has the focus: %+v", v)
	}
	// A message that comes while the owner looks is not new.
	f.todo = append(f.todo, pin("T4", f.now.Add(time.Minute), "Seen live"))
	f.now = f.now.Add(2 * time.Minute)
	if _, v, _ := f.o.view(); v.Count != 1 || v.New[3] {
		t.Errorf("a pin that came while the owner looks is old: %+v", v)
	}
	// The owner leaves: all become old.
	f.o.Focus("")
	f.tt.Frame()
	if _, v, _ := f.o.view(); v.Count != 0 {
		t.Errorf("after the focus left, nothing is new: %+v", v)
	}
	if f.tt.HasText(LabelOutlineCount) {
		t.Errorf("no count with nothing new: texts %q", f.tt.Texts())
	}
	// A message while away is new again.
	f.todo = append(f.todo, pin("T5", f.now.Add(time.Minute), "While away"))
	f.now = f.now.Add(2 * time.Minute)
	if _, v, _ := f.o.view(); v.Count != 1 || !v.New[4] {
		t.Errorf("a pin while away is new: %+v", v)
	}
}

func TestOutlineFocusIsPerTerminal(t *testing.T) {
	f := newOutline(t, threePins()...)
	f.o.Focus("w9:t9") // another terminal has the focus
	f.now = f.now.Add(time.Hour)
	f.o.Focus("")
	if _, v, _ := f.o.view(); v.Count != 1 {
		t.Errorf("the focus on another terminal marks nothing read here: %+v", v)
	}
}

func TestOutlineCardShowsOnHoverAndHidesOnLeave(t *testing.T) {
	f := newOutline(t, threePins()...)
	if f.tt.HasText("The tag needs a name before Friday.") {
		t.Fatal("a card shows before the hover")
	}
	f.hover(t, "Decide the release name")
	for _, want := range []string{"Decide the release name", "The tag needs a name before Friday.", "12:00"} {
		if !f.tt.HasText(want) {
			t.Errorf("card has no %q; texts %q", want, f.tt.Texts())
		}
	}
	card := f.rect(t, LabelOutlineCard)
	tick := f.rect(t, "Decide the release name")
	if card.X+card.W > tick.X || card.W != theme.SizeOutlineCardWidth {
		t.Errorf("card %+v should sit at the left of the tick %+v, %d wide", card, tick, theme.SizeOutlineCardWidth)
	}
	// One at a time: the next tick takes the card.
	f.hover(t, "Review the sidebar PR")
	if f.tt.HasText("The tag needs a name before Friday.") || !f.tt.HasText("to do") {
		t.Errorf("one card at a time; texts %q", f.tt.Texts())
	}
	f.tt.Move(10, 10)
	f.tt.Advance(700 * time.Millisecond)
	f.tt.Frame()
	if _, ok := f.tt.Find(LabelOutlineCard); ok {
		t.Errorf("the card stays after the pointer left; texts %q", f.tt.Texts())
	}
}

func TestOutlineCardShowsOnKeyboardFocusAndEscapeHidesIt(t *testing.T) {
	f := newOutline(t, threePins()...)
	for i := 0; i < 4 && !f.tt.HasText("The tag needs a name before Friday."); i++ {
		f.tt.Key(0, ui.KeyTab)
	}
	if !f.tt.HasText("The tag needs a name before Friday.") {
		t.Fatalf("Tab does not reach the first tick; texts %q", f.tt.Texts())
	}
	// ↓ moves between the ticks, and the card follows.
	f.tt.Key(0, ui.KeyDown)
	if f.tt.HasText("The tag needs a name before Friday.") || !f.tt.HasText("Review the sidebar PR") {
		t.Errorf("↓ moves to the next tick; texts %q", f.tt.Texts())
	}
	f.tt.Key(0, ui.KeyEscape)
	if _, ok := f.tt.Find(LabelOutlineCard); ok {
		t.Errorf("Esc leaves the card; texts %q", f.tt.Texts())
	}
}

func TestOutlineClickMarksTheMessageAndTheOnesBeforeRead(t *testing.T) {
	f := newOutline(t,
		pin("T1", outlineClockStart.Add(time.Minute), "First"),
		pin("T2", outlineClockStart.Add(2*time.Minute), "Second\nSecond detail."),
		pin("T3", outlineClockStart.Add(3*time.Minute), "Third"),
	)
	if _, v, _ := f.o.view(); v.Count != 3 {
		t.Fatalf("three new: %+v", v)
	}
	if err := f.tt.Click("Second, new"); err != nil {
		t.Fatal(err)
	}
	f.tt.Frame()
	_, v, _ := f.o.view()
	if v.Count != 1 || v.New[0] || v.New[1] || !v.New[2] {
		t.Errorf("click on T2 marks T1 and T2 read: %+v", v)
	}
	if !f.tt.HasText("Second detail.") {
		t.Errorf("the click opens the card; texts %q", f.tt.Texts())
	}
	// The card goes when the pointer leaves.
	f.tt.Move(10, 10)
	f.tt.Frame()
	f.tt.Frame()
	if _, ok := f.tt.Find(LabelOutlineCard); ok {
		t.Errorf("the card of the click stays; texts %q", f.tt.Texts())
	}
	if err := f.tt.Click("Third, new"); err != nil {
		t.Fatal(err)
	}
	if _, v, _ := f.o.view(); v.Count != 0 {
		t.Errorf("all read: %+v", v)
	}
	f.tt.Frame()
	if f.tt.HasText(LabelOutlineCount) {
		t.Errorf("no count when all are read; texts %q", f.tt.Texts())
	}
}

func TestOutlineTakesNoKeysUnlessFocused(t *testing.T) {
	f := newOutline(t, threePins()...)
	var grid int
	f.tt = ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Box(c).Fill().Children(func() {
			// The terminal stand-in has the focus; the overlay sits above it.
			ui.ButtonBase(c).Label("grid").AutoFocus().Size(200, 100).OnClick(func() { grid++ })
			f.ov.View(c)
		})
	}, 900, 600)
	for _, k := range []ui.Key{ui.KeyEnter, ui.KeyDown, ui.KeyUp, ui.KeyEscape, ui.KeySpace} {
		f.tt.Key(0, k)
	}
	if _, v, _ := f.o.view(); v.Count != 1 {
		t.Errorf("keys changed the outline: %+v", v)
	}
	if _, ok := f.tt.Find(LabelOutlineCard); ok {
		t.Errorf("a card shows from keys with no focus on a tick; texts %q", f.tt.Texts())
	}
	if grid == 0 {
		t.Error("the terminal did not get the keys")
	}
}

// The view reads only the snapshot copy. The file is read on goroutines of the
// model, which the file watch drives.
func TestOutlineViewDoesNoFileReads(t *testing.T) {
	dir := t.TempDir()
	store := &owner.Store{Path: filepath.Join(dir, "owner.json"), Now: func() time.Time { return outlineClockStart.Add(time.Minute) }}
	if _, err := store.Pin(owner.Todo{Project: "contr", Text: "Ship the outline"}); err != nil {
		t.Fatal(err)
	}
	updated := make(chan struct{}, 16)
	m := agentstate.New(agents.New(&needsCore{snap: hc.Snapshot{}}, agents.DefaultConfig()), store, func() {
		select {
		case updated <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	defer func() { cancel(); m.Wait() }()

	sel := state.NewSelection(nil)
	sel.Select(state.Selected{Kind: state.KindOrchestrator, ID: apptest.OrchAddr})
	o := NewOutline(func() agentstate.Snapshot {
		s := m.Snapshot()
		s.Orchestrators = []agents.Worker{{Address: apptest.OrchAddr, Project: "contr", Role: agents.RoleOrchestrator}}
		return s
	}, sel, view.Env{})
	o.start = outlineClockStart
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c); ui.Box(c).Fill().Children(func() { o.View(c) }) }, 900, 600)
	if _, ok := tt.Find("Ship the outline, new"); !ok {
		t.Fatalf("the tick of the pin shows; texts %q", tt.Texts())
	}
	// The file is bad: a view that read it would lose the tick. On a bad file
	// the model keeps its last good items, so the tick stays at each model
	// read. Do not remove the file: a missing file reads as no pins, and the
	// first read of the watch goroutine can clear the tick before the frame
	// (#554).
	good, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Ship the outline, new"); !ok {
		t.Error("the view read the file: the tick went with it")
	}
	// A new pin reaches the view through the watch and the model.
	if err := os.WriteFile(store.Path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	for len(updated) > 0 {
		<-updated
	}
	if _, err := store.Pin(owner.Todo{Project: "contr", Text: "Second message"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		tt.Frame()
		if _, ok := tt.Find("Second message, new"); ok {
			break
		}
		select {
		case <-updated:
		case <-deadline:
			t.Fatalf("the watch did not bring the new pin; texts %q", tt.Texts())
		}
	}
}

func TestOutlineFocusIgnoresANilOutline(t *testing.T) {
	var o *Outline
	o.Focus("x") // the frame has no outline without an agent source
}

func TestOutlineFocusedIsTheLastFocus(t *testing.T) {
	f := newOutline(t)
	if got := f.o.Focused(); got != "" {
		t.Errorf("focused %q before any focus", got)
	}
	f.o.Focus(apptest.OrchAddr)
	if got := f.o.Focused(); got != apptest.OrchAddr {
		t.Errorf("focused %q, want %q", got, apptest.OrchAddr)
	}
	f.o.Focus("")
	if got := f.o.Focused(); got != "" {
		t.Errorf("focused %q after a blur", got)
	}
	var none *Outline
	if got := none.Focused(); got != "" {
		t.Errorf("nil outline focused %q", got)
	}
}
