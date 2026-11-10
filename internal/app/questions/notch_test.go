package questions

import (
	"context"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// notchRig is a notch in a terminal-sized window. The project follows project.
type notchRig struct {
	n       *Notch
	src     *source
	tt      *ui.Tester
	project string
	opened  []agentstate.NeedTab
	closes  int
	isOpen  bool
}

func newNotchRig(t *testing.T, src *source, project string) *notchRig {
	t.Helper()
	r := &notchRig{src: src, project: project}
	r.n = &Notch{
		Snapshot: src.Snapshot,
		Project:  func() string { return r.project },
		Open:     func(tab agentstate.NeedTab) { r.opened, r.isOpen = append(r.opened, tab), true },
		Close:    func() { r.closes++; r.isOpen = false },
		Opened:   func() bool { return r.isOpen },
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		if c.Shortcut(ui.Cmd, ui.KeyJ) {
			r.n.Toggle()
		}
		ui.Box(c).Fill().Children(func() { r.n.View(c) })
	}, 900, 400)
	return r
}

func TestNotchLabelCountsInWords(t *testing.T) {
	q := agentstate.NeedTab{Kind: agentstate.NeedQuestion}
	todo := agentstate.NeedTab{Kind: agentstate.NeedTodo}
	blocked := agentstate.NeedTab{Kind: agentstate.NeedBlocked}
	for _, tc := range []struct {
		items []agentstate.NeedTab
		want  string
	}{
		{nil, ""},
		{[]agentstate.NeedTab{q}, "1 needs you"},
		{[]agentstate.NeedTab{q, q}, "2 need you"},
		{[]agentstate.NeedTab{q, q, todo}, "3 need you"},
		{[]agentstate.NeedTab{todo}, "1 needs you"},
		{[]agentstate.NeedTab{q, blocked}, "2 need you"},
	} {
		if got := NotchLabel(tc.items); got != tc.want {
			t.Errorf("NotchLabel(%d items) = %q, want %q", len(tc.items), got, tc.want)
		}
	}
}

func TestNotchNewestIsTheLatestItem(t *testing.T) {
	at := func(s int64) time.Time { return time.Unix(s, 0) }
	for _, tc := range []struct {
		name  string
		items []agentstate.NeedTab
		want  string
	}{
		{"none", nil, ""},
		{"latest time", []agentstate.NeedTab{{Key: "D1", Title: "new", Since: at(300)}, {Key: "T1", Title: "old", Since: at(100)}}, "new"},
		{"tie: the last", []agentstate.NeedTab{{Key: "D1", Title: "first", Since: at(5)}, {Key: "D2", Title: "second", Since: at(5)}}, "second"},
		{"no times: the last", []agentstate.NeedTab{{Key: "D1", Title: "first"}, {Key: "D2", Title: "second"}}, "second"},
		{"no title: the key", []agentstate.NeedTab{{Key: "b-250", Since: at(1)}}, "b-250"},
	} {
		if got := NotchNewest(tc.items); got != tc.want {
			t.Errorf("%s: NotchNewest = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The notch shows the count, then the newest item; in a narrow terminal the
// item clips first and the label stays whole (D258).
func TestNotchShowsTheNewestItemAndClipsItFirst(t *testing.T) {
	long := "b-250 asks to run go test -tags hccore ./internal/hc/... and then some more words"
	src := &source{Snap: agentstate.Snapshot{Questions: []owner.Question{
		{ID: "D1", Project: "a", Title: "older", Asked: time.Unix(100, 0)},
		{ID: "D2", Project: "a", Title: long, Asked: time.Unix(200, 0)},
	}}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText(long) || r.tt.HasText("older") {
		t.Fatalf("texts %q, want the newest item only", r.tt.Texts())
	}
	label, item := apptest.Box(t, r.tt, "2 need you"), apptest.Box(t, r.tt, long)
	if item.X <= label.X+label.W {
		t.Errorf("the item %+v is not after the label %+v", item, label)
	}
	if item.W > notchNewestMax {
		t.Errorf("item width %v, want at most %v", item.W, notchNewestMax)
	}
	wide := label.W
	r.tt.SetSize(260, 400)
	r.tt.Frame()
	notch := apptest.Box(t, r.tt, "2 need you. Open")
	label, item = apptest.Box(t, r.tt, "2 need you"), apptest.Box(t, r.tt, long)
	if label.W < wide-0.01 {
		t.Errorf("label width %v in a narrow terminal, want %v", label.W, wide)
	}
	if notch.X < 0 || notch.X+notch.W > 260 {
		t.Errorf("notch %+v does not fit the 260px terminal", notch)
	}
	if item.X+item.W > notch.X+notch.W-theme.Space16 {
		t.Errorf("item %+v runs into the end padding of the notch %+v", item, notch)
	}
}

func TestNotchShowsAndHidesWithTheData(t *testing.T) {
	src := &source{}
	r := newNotchRig(t, src, "a")
	if len(r.tt.Texts()) != 0 {
		t.Fatalf("notch with no items shows %q", r.tt.Texts())
	}
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	if !r.tt.HasText("1 needs you") {
		t.Fatalf("texts %q, want 1 needs you", r.tt.Texts())
	}
	if _, ok := r.tt.Find("1 needs you. Open"); !ok {
		t.Error("no element labelled \"1 needs you. Open\"")
	}
	src.Snap.Questions = apptest.Questions("a", 2)
	r.tt.Frame()
	if _, ok := r.tt.Find("2 need you. Open"); !ok || !r.tt.HasText("2 need you") {
		t.Errorf("texts %q, want 2 need you", r.tt.Texts())
	}
	src.Snap.Questions = nil
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("notch stays after the data went: %q", r.tt.Texts())
	}
}

func TestNotchCountsOnlyItsProject(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{
		Questions: append(apptest.Questions("a", 1), owner.Question{ID: "D9", Project: "b", Title: "other"}, owner.Question{ID: "D8", Project: "b", Title: "other"}),
		Todos:     []owner.Todo{{ID: "T9", Project: "b", Kind: "do", Text: "other"}},
		Workers:   []agents.Worker{apptest.BlockedWorker("b-9", "b", "Allow rm?")},
	}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("1 needs you") {
		t.Errorf("project a: texts %q, want 1 needs you", r.tt.Texts())
	}
	r.project = "b"
	r.tt.Frame()
	if !r.tt.HasText("4 need you") {
		t.Errorf("project b: texts %q, want 4 need you", r.tt.Texts())
	}
	r.project = "c"
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("project c shows %q", r.tt.Texts())
	}
	r.project = ""
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("no project shows %q", r.tt.Texts())
	}
}

func TestNotchMixOfQuestionsTodosAndBlockedMenus(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{
		Questions: apptest.Questions("a", 2),
		Todos:     []owner.Todo{{ID: "T1", Project: "a", Kind: "know", Text: "CI is slow"}},
	}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("3 need you") {
		t.Fatalf("texts %q, want 3 need you", r.tt.Texts())
	}
	if _, ok := r.tt.Find("3 need you. Open"); !ok {
		t.Error("no element labelled \"3 need you. Open\"")
	}
	src.Snap.Todos = nil
	src.Snap.Workers = []agents.Worker{apptest.BlockedWorker("b-250", "a", "Allow rm?")}
	r.tt.Frame()
	if !r.tt.HasText("3 need you") {
		t.Errorf("a blocked menu does not count: texts %q", r.tt.Texts())
	}
}

func TestNotchIgnoresResolvedItemsAndABrokenFile(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 2), Resolved: []string{"D1"}}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("1 needs you") {
		t.Errorf("a resolved question counts: texts %q", r.tt.Texts())
	}
	src.Snap.OwnerProblem = "owner: owner.json: invalid character"
	r.tt.Frame()
	if r.tt.HasText("1 needs you") {
		t.Errorf("notch counts with a broken questions file: %q", r.tt.Texts())
	}
	// A problem of hc alone leaves the notch.
	src.Snap.OwnerProblem, src.Snap.Problem = "", "hc: gone"
	r.tt.Frame()
	if !r.tt.HasText("1 needs you") {
		t.Errorf("hc problem hides the notch: %q", r.tt.Texts())
	}
}

func TestNotchClickOpensTheCardForTheFirstItem(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{
		Questions: apptest.Questions("a", 1),
		Workers:   []agents.Worker{apptest.BlockedWorker("b-250", "a", "Allow rm?")},
	}}
	r := newNotchRig(t, src, "a")
	if err := r.tt.Click("2 need you. Open"); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 || r.opened[0].Key != "D1" {
		t.Fatalf("opened %+v, want the question D1 first, before the blocked worker b-250", r.opened)
	}
	// A second click closes the card.
	if err := r.tt.Click("2 need you. Open"); err != nil {
		t.Fatal(err)
	}
	if r.closes != 1 || len(r.opened) != 1 {
		t.Errorf("second click: closes %d, opens %d; want 1 and 1", r.closes, len(r.opened))
	}
}

func TestNotchCmdJOpensAndClosesTheCard(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if len(r.opened) != 1 || r.opened[0].Key != "D1" {
		t.Fatalf("⌘J opened %+v, want D1", r.opened)
	}
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if r.closes != 1 {
		t.Errorf("second ⌘J closes = %d, want 1", r.closes)
	}
}

func TestNotchCmdJWithNothingWaitingDoesNothing(t *testing.T) {
	r := newNotchRig(t, &source{}, "a")
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if len(r.opened) != 0 || r.closes != 0 {
		t.Errorf("⌘J with no items: opens %d, closes %d", len(r.opened), r.closes)
	}
}

func TestNotchKeysOpenItOnlyWhileFocused(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.tt.SetFocused(true)
	r.tt.Type(" ")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.opened) != 0 {
		t.Fatalf("an unfocused notch took a key: opened %+v", r.opened)
	}
	// A click gives it the focus; then Enter presses it.
	if err := r.tt.Click("1 needs you. Open"); err != nil {
		t.Fatal(err)
	}
	r.isOpen, r.opened = false, nil
	r.tt.Key(0, ui.KeyEnter)
	if len(r.opened) != 1 {
		t.Errorf("a focused notch ignores Enter: opened %d", len(r.opened))
	}
}

// The notch reads the agent state, which reads owner.json on a goroutine. A
// store whose path is a pipe blocks any read forever, so a notch that reads
// the file on the UI thread would hang here.
func TestNotchReadsNoFileOnTheUIThread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("no pipe: %v", err)
	}
	m := agentstate.New(agents.New(&needsCore{snap: hc.Snapshot{}}, agents.DefaultConfig()), &owner.Store{Path: path}, nil)
	n := &Notch{Snapshot: m.Snapshot, Project: func() string { return "a" }}
	done := make(chan int)
	go func() {
		tt := ui.NewTester(n.View, 400, 100)
		tt.Frame()
		done <- len(n.Items())
	}()
	select {
	case got := <-done:
		if got != 0 {
			t.Errorf("items = %d, want 0 before the model reads", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the notch blocked on the questions file")
	}
}

func TestNotchFollowsTheFileThroughTheModel(t *testing.T) {
	store := &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")}
	for i := 0; i < 2; i++ {
		if _, err := store.Ask(owner.Question{Project: "a", Title: "b-250 checks", Question: "Merge?"}); err != nil {
			t.Fatal(err)
		}
	}
	m := agentstate.New(agents.New(&needsCore{snap: hc.Snapshot{}}, agents.DefaultConfig()), store, nil)
	n := &Notch{Snapshot: m.Snapshot, Project: func() string { return "a" }}
	if got := len(n.Items()); got != 0 {
		t.Fatalf("items before the model starts = %d, want 0: the UI thread read the file", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	defer func() { cancel(); m.Wait() }()
	if got := NotchLabel(n.Items()); got != "2 need you" {
		t.Errorf("label after the read = %q, want 2 need you", got)
	}
}

func TestNotchShowsAProblemOfTheQuestionsFile(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 1)}}
	r := newNotchRig(t, src, "a")
	if _, ok := r.tt.Find(LabelOwnerProblem); ok {
		t.Fatal("the problem shows while the file reads well")
	}
	src.Snap.OwnerProblem = "owner: owner.json: invalid character"
	r.tt.Frame()
	apptest.Box(t, r.tt, LabelOwnerProblem)
	if !r.tt.HasText(TextOwnerProblem) {
		t.Errorf("no problem text in %q", r.tt.Texts())
	}
	// It takes no click and opens no card.
	if err := r.tt.Click(LabelOwnerProblem); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 0 {
		t.Errorf("the problem opened a card: %v", r.opened)
	}
	// It goes when the file reads again.
	src.Snap.OwnerProblem = ""
	r.tt.Frame()
	if _, ok := r.tt.Find(LabelOwnerProblem); ok {
		t.Error("the problem stays after the file reads well")
	}
}

func TestNotchProblemClosesAnOpenCard(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.n.Toggle()
	src.Snap.OwnerProblem = "owner: owner.json is locked"
	r.tt.Frame()
	if r.closes != 1 || r.isOpen {
		t.Errorf("card closes = %d, open = %v", r.closes, r.isOpen)
	}
	apptest.Box(t, r.tt, LabelOwnerProblem)
}

// Shown follows what the notch draws: an open item or the broken file (D234, for the pill).
func TestNotchShownFollowsItemsAndTheFileProblem(t *testing.T) {
	src := &source{}
	n := &Notch{Snapshot: src.Snapshot, Project: func() string { return "a" }}
	if n.Shown() {
		t.Error("Shown with nothing waiting")
	}
	src.Snap.Questions = apptest.Questions("b", 1)
	if n.Shown() {
		t.Error("Shown for a question of another project")
	}
	src.Snap.Questions = apptest.Questions("a", 1)
	if !n.Shown() {
		t.Error("not Shown with a question")
	}
	src.Snap.Questions, src.Snap.OwnerProblem = nil, "owner: owner.json: locked"
	if !n.Shown() {
		t.Error("not Shown with the file problem")
	}
	if (&Notch{}).Shown() {
		t.Error("a notch without data is Shown")
	}
}

func TestNotchShowsTheCmdJChipWhileCmdIsHeld(t *testing.T) {
	src := &source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.tt.Frame()
	if _, ok := r.tt.Find(kit.KeyHintLabel("J")); ok {
		t.Fatal("a chip without Hints")
	}
	r.n.Hints = true
	r.tt.Frame()
	if _, ok := r.tt.Find(kit.KeyHintLabel("J")); !ok || !slices.Contains(r.tt.Texts(), "J") {
		t.Errorf("no J chip: %q", r.tt.Texts())
	}
	// On the dot, inside the notch and the window (#645).
	chip := apptest.ChipOn(t, r.tt, kit.KeyHintLabel("J"), NotchAccessible(r.n.Items()), 900, 400)
	apptest.Centered(t, "chip", chip, "the dot", apptest.Box(t, r.tt, LabelNotchDot))
}
