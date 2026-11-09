package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/review"
)

// treeRig is a file tree in the files panel, with reads that run at once and a
// watch that the test fires.
type treeRig struct {
	t        *testing.T
	ft       *FileTree
	tt       *ui.Tester
	dir      string
	opened   [][2]string
	revealed []string
	notify   func()
	stops    int
	starts   int
}

func newTreeRig(t *testing.T, dir string) *treeRig {
	t.Helper()
	r := &treeRig{t: t, dir: dir}
	r.ft = NewFileTree(func() string { return r.dir })
	r.ft.Env.Run.Go = func(fn func()) { r.starts++; fn() }
	r.ft.Debounce = -1
	r.ft.Open = func(d, rel string) { r.opened = append(r.opened, [2]string{d, rel}) }
	r.ft.Reveal = func(abs string) { r.revealed = append(r.revealed, abs) }
	r.ft.Watch = func(d string, notify func()) (func(), error) {
		r.notify = notify
		return func() { r.stops++ }, nil
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		kit.Panel(c, kit.PanelOpts{Name: LabelPanelFiles, Head: func() { r.ft.Head(c) }, Body: func() { r.ft.View(c) }})
	}, theme.SizeFilesWidth, 600)
	return r
}

func (r *treeRig) frame() { r.tt.Frame() }

// reset forgets the folder, so the next frame reads it again with the hooks
// the test set after the tester built its first frame.
func (r *treeRig) reset() { r.ft.setDir(""); r.stops = 0 }

func (r *treeRig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// changedRepo is a repo on branch "feature" with a modified, a new, a deleted
// and an ignored file, and a folder with a file in it.
func changedRepo(t *testing.T) string {
	s := reviewScratch(t)
	write(t, s.dir, ".gitignore", "*.log\nbuild/\n")
	write(t, s.dir, "mod.go", "1\n2\n3\n")
	write(t, s.dir, "gone.md", "a\nb\n")
	write(t, s.dir, "src/deep/inner.go", "x\n")
	write(t, s.dir, "same.txt", "same\n")
	s.git("add", "-A")
	s.git("commit", "-m", "files")
	s.git("checkout", "-b", "feature")
	write(t, s.dir, "mod.go", "1\n2\n3\n4\n5\n6\n") // +3
	os.Remove(filepath.Join(s.dir, "gone.md"))
	write(t, s.dir, "fresh.go", "a\nb\nc\nd\n")
	write(t, s.dir, "noise.log", "ignored\n")
	write(t, s.dir, "build/out.bin", "x")
	return s.dir
}

func TestFileTreeShowsFilesWithCountsAndHidesIgnored(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	for _, want := range []string{"mod.go, +3 −0", "fresh.go, +4", "gone.md, −2", "same.txt", "src", "base.txt"} {
		if !r.has(want) {
			t.Errorf("no row %q; texts %q", want, r.tt.Texts())
		}
	}
	for _, no := range []string{"noise.log", "build", ".git"} {
		if r.has(no) {
			t.Errorf("row %q shows but is ignored", no)
		}
	}
	// New shows "+N" only, deleted "−N" only, changed both.
	for _, s := range r.tt.Texts() {
		if s == "−4" && r.has("fresh.go, +4") {
			t.Error("a new file shows a removed count")
		}
	}
}

func TestFileTreeSortsFoldersFirst(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	src, _ := r.tt.Find("src")
	first, _ := r.tt.Find("base.txt")
	if src.Y >= first.Y {
		t.Errorf("folder src at y %v is not above the file base.txt at y %v", src.Y, first.Y)
	}
}

func TestFileTreeReadsAFolderWhenItOpens(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	if _, ok := r.ft.loaded["src"]; ok {
		t.Fatal("src was read before it opened")
	}
	if r.has("inner.go") || r.has("deep") {
		t.Fatal("a closed folder shows its entries")
	}
	if err := r.tt.Click("src"); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if _, ok := r.ft.loaded["src"]; !ok || !r.has("deep") {
		t.Fatalf("src not read after the click; loaded %v, texts %q", r.ft.loaded, r.tt.Texts())
	}
	if _, ok := r.ft.loaded["src/deep"]; ok {
		t.Error("the sub-folder was read with its parent")
	}
}

func TestFileTreeFilterShowsChangedFilesOnly(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	if err := r.tt.Click(LabelFileFilter); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if !r.ft.OnlyChanged() {
		t.Fatal("the filter is off after the click")
	}
	for _, want := range []string{"mod.go, +3 −0", "fresh.go, +4", "gone.md, −2"} {
		if !r.has(want) {
			t.Errorf("filter on: no row %q; texts %q", want, r.tt.Texts())
		}
	}
	for _, no := range []string{"same.txt", "base.txt", "src"} {
		if r.has(no) {
			t.Errorf("filter on: unchanged %q shows", no)
		}
	}
	if err := r.tt.Click(LabelFileFilter); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if !r.has("same.txt") {
		t.Error("filter off: the unchanged file is back")
	}
}

func TestFileTreeFilterFoldersOpenAndShowChangedNestedFiles(t *testing.T) {
	dir := changedRepo(t)
	write(t, dir, "src/deep/inner.go", "x\ny\n")
	r := newTreeRig(t, dir)
	r.ft.onlyChanged = true
	r.frame()
	for _, want := range []string{"src", "deep", "inner.go, +1 −0"} {
		if !r.has(want) {
			t.Errorf("no %q; texts %q", want, r.tt.Texts())
		}
	}
}

func TestFileInksAndCounts(t *testing.T) {
	p := theme.Dark
	cases := []struct {
		name         string
		f            review.File
		changed      bool
		ink, icon    ui.Color
		strike       bool
		counts       bool
		only         int
		added, rmved ui.Color
	}{
		{"default", review.File{}, false, p.Ink50, p.Ink38, false, false, 0, ui.Color{}, ui.Color{}},
		{"changed", review.File{Status: review.Modified, Added: 2, Removed: 1}, true, p.Ink90, p.Ink38, false, true, 0, p.Ink40, p.Ink24},
		{"new", review.File{Status: review.Added, Added: 5}, true, p.Ink90, p.Ink38, false, true, 1, p.Ink40, p.Ink24},
		{"deleted", review.File{Status: review.Deleted, Removed: 3}, true, p.Ink24, p.Ink22, true, true, 2, p.Ink22, p.Ink22},
		{"binary", review.File{Status: review.Modified, Binary: true}, true, p.Ink90, p.Ink38, false, false, 0, ui.Color{}, ui.Color{}},
	}
	for _, c := range cases {
		ink, icon, strike := fileInks(p, c.f, c.changed)
		if ink != c.ink || icon != c.icon || strike != c.strike {
			t.Errorf("%s: inks %v %v %v, want %v %v %v", c.name, ink, icon, strike, c.ink, c.icon, c.strike)
		}
		look, ok := fileCounts(p, c.f, c.changed)
		if ok != c.counts {
			t.Errorf("%s: counts shown %v, want %v", c.name, ok, c.counts)
			continue
		}
		if ok && (int(look.Only) != c.only || look.Added != c.added || look.Removed != c.rmved) {
			t.Errorf("%s: look %+v", c.name, look)
		}
	}
}

func TestFileTreeFollowsTheSelectedFolder(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "alpha.txt", "x")
	write(t, b, "beta.txt", "x")
	r := newTreeRig(t, a)
	r.frame()
	if !r.has("alpha.txt") || r.has("beta.txt") {
		t.Fatalf("folder a shows %q", r.tt.Texts())
	}
	r.dir = b
	r.frame()
	if r.has("alpha.txt") || !r.has("beta.txt") {
		t.Errorf("folder b shows %q", r.tt.Texts())
	}
	if r.stops != 1 {
		t.Errorf("the watch of folder a stopped %d times, want 1", r.stops)
	}
	r.dir = ""
	r.frame()
	if r.has("beta.txt") || !r.tt.HasText("No folder") {
		t.Errorf("no folder shows %q", r.tt.Texts())
	}
}

func TestFilesDirFollowsTheSelectedAgent(t *testing.T) {
	f := NewFrame()
	f.Selection = state.NewSelection(nil)
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: "o1", Project: "p", Worktree: "/work/main"}},
			Workers:       []agents.Worker{{Address: "w1", Project: "p", Worktree: "/work/wt-1"}},
		}
	}
	f.takeSnapshot()
	if got := f.filesDir(); got != "" {
		t.Errorf("no selection and no project: %q", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	if got := f.filesDir(); got != "/work/wt-1" {
		t.Errorf("worker selected: %q", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	if got := f.filesDir(); got != "/work/main" {
		t.Errorf("orchestrator selected: %q", got)
	}
}

func TestFileTreeSkipsTheWatchlessFolderAndShowsReadErrors(t *testing.T) {
	r := newTreeRig(t, filepath.Join(t.TempDir(), "missing"))
	var shown []string
	r.ft.Env.Status = func(s string) { shown = append(shown, s) }
	r.reset()
	r.frame()
	if len(shown) == 0 {
		t.Error("a folder that cannot be read says nothing")
	}
	starts := r.starts
	r.frame()
	r.frame()
	if r.starts != starts {
		t.Errorf("a failed folder is read again on each frame: %d reads", r.starts-starts)
	}
}

// A watch event starts a new read on a goroutine, not on the UI thread.
func TestFileTreeWatchEventReadsAgainOffTheUIThread(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "x")
	r := newTreeRig(t, dir)
	var mu sync.Mutex
	var jobs []func()
	r.ft.Env.Run.Go = func(fn func()) { mu.Lock(); jobs = append(jobs, fn); mu.Unlock() }
	r.frame()
	run := func() {
		mu.Lock()
		list := jobs
		jobs = nil
		mu.Unlock()
		for _, j := range list {
			j()
		}
	}
	run()
	r.frame()
	if !r.has("one.txt") {
		t.Fatalf("first read missing: %q", r.tt.Texts())
	}
	write(t, dir, "two.txt", "x")
	// The event comes from the watch goroutine; the read is queued, not run.
	posted := 0
	r.ft.Env.Run.Post = func(fn func()) { posted++; fn() }
	r.notify()
	mu.Lock()
	queued := len(jobs)
	mu.Unlock()
	if queued == 0 {
		t.Fatal("a watch event started no read")
	}
	r.frame()
	if r.has("two.txt") {
		t.Fatal("the read ran on the UI thread")
	}
	run()
	r.frame()
	if !r.has("two.txt") {
		t.Errorf("the new file does not show after the read: %q", r.tt.Texts())
	}
}

func TestFileTreeWatchEventsAreDebounced(t *testing.T) {
	dir := t.TempDir()
	r := newTreeRig(t, dir)
	r.ft.Debounce = 30 * time.Millisecond
	reads := make(chan struct{}, 8)
	r.ft.Env.Run.Go = func(fn func()) { reads <- struct{}{}; fn() }
	r.reset()
	r.frame()
	<-reads
	for i := 0; i < 5; i++ {
		r.notify()
	}
	select {
	case <-reads:
	case <-time.After(2 * time.Second):
		t.Fatal("no read after the events")
	}
	select {
	case <-reads:
		t.Error("five events in a burst made more than one read")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFileTreeClickOpensAndCmdClickReveals(t *testing.T) {
	dir := changedRepo(t)
	r := newTreeRig(t, dir)
	r.frame()
	if err := r.tt.Click("mod.go, +3 −0"); err != nil {
		t.Fatal(err)
	}
	if want := [][2]string{{dir, "mod.go"}}; !slices.Equal(r.opened, want) {
		t.Errorf("opened %v, want %v", r.opened, want)
	}
	if err := r.tt.ClickWith(ui.Cmd, "fresh.go, +4"); err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(dir, "fresh.go")}; !slices.Equal(r.revealed, want) {
		t.Errorf("revealed %v, want %v", r.revealed, want)
	}
	if len(r.opened) != 1 {
		t.Errorf("⌘-click also opened a file: %v", r.opened)
	}
}

func TestFileTreeFolderClickOpensIt(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	r.tt.Click("src")
	r.frame()
	if !r.has("deep") {
		t.Fatalf("the folder did not open: %q", r.tt.Texts())
	}
	if len(r.opened) != 0 {
		t.Errorf("a folder click opened a file: %v", r.opened)
	}
	r.tt.Click("src")
	r.frame()
	if r.has("deep") {
		t.Error("a second click did not close the folder")
	}
}

func TestFileTreeNotAGitFolderHasNoCounts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plain.txt", "x\n")
	write(t, dir, "skip.log", "x\n")
	r := newTreeRig(t, dir)
	r.frame()
	if !r.has("plain.txt") || !r.has("skip.log") {
		t.Errorf("rows missing: %q", r.tt.Texts())
	}
	for _, s := range r.tt.Texts() {
		if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "−") {
			t.Errorf("count %q outside git", s)
		}
	}
}

func TestFileTreeEmptyStates(t *testing.T) {
	r := newTreeRig(t, t.TempDir())
	r.frame()
	if !r.tt.HasText("Empty folder") {
		t.Errorf("empty folder: %q", r.tt.Texts())
	}
	r.tt.Click(LabelFileFilter)
	r.frame()
	if !r.tt.HasText("No changes") {
		t.Errorf("filter on, nothing changed: %q", r.tt.Texts())
	}
}

// The frame has the tree in the files slot, and the board is gone from it.
func TestFramePanelShowsTheFileTreeNotTheBoard(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	tt.Frame()
	box(t, tt, LabelFileTree)
	box(t, tt, LabelFileFilter)
	if !absent(tt, LabelPanel) || !absent(tt, "[panel]") {
		t.Errorf("the board panel shows: %q", tt.Texts())
	}
}

// A click on a file opens it in Review at the current branch.
func TestFileClickOpensTheFileInReview(t *testing.T) {
	s := reviewScratch(t)
	s.git("checkout", "-b", "other")
	s.commit("o", map[string]string{"o.txt": "o\n"})
	s.git("checkout", "-b", "work")
	s.commit("w", map[string]string{"first.txt": "1\n", "wanted.txt": "w1\nw2\n"})
	f := NewFrame()
	f.Review, _ = newTestReview()
	f.openFileInReview(s.dir, "wanted.txt")
	if !f.ReviewOpen() {
		t.Fatal("Review did not open")
	}
	if f.Review.Branch() != "work" || f.Review.File() != "wanted.txt" {
		t.Errorf("review at %q file %q, want work wanted.txt", f.Review.Branch(), f.Review.File())
	}
	// A file the branch does not change opens the first file.
	f.openFileInReview(s.dir, "base.txt")
	if f.Review.File() != "first.txt" {
		t.Errorf("unchanged file: review file %q, want the first", f.Review.File())
	}
}

func TestFileTreeKeysMoveAndOpen(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	// Tab focuses the first row; ⏎ on a folder opens it, → on it moves in.
	r.tt.Click("src")
	r.frame()
	r.tt.Key(0, ui.KeyLeft)
	r.frame()
	if r.has("deep") {
		t.Errorf("← did not close the folder: %q", r.tt.Texts())
	}
	r.tt.Key(0, ui.KeyRight)
	r.frame()
	if !r.has("deep") {
		t.Errorf("→ did not open the folder: %q", r.tt.Texts())
	}
}

// With the board disabled (D122), selecting an orchestrator or a worker, or
// pressing the key that opened the context sheet, leaves the file tree in the slot.
func TestFramePanelStaysTheFileTreeWhateverIsSelected(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.Selection = state.NewSelection(nil)
	f.Worker = &WorkerPanel{Source: panelSource(), Selection: f.Selection}
	for _, sel := range []state.Selected{
		{Kind: state.KindOrchestrator, ID: "o1"}, {Kind: state.KindWorker, ID: "t1"}, {Kind: state.KindTerminal, ID: "x"},
	} {
		f.Selection.Select(sel)
		tt.Key(ui.Cmd, ui.KeyJ)
		tt.Frame()
		box(t, tt, LabelFileTree)
		if !absent(tt, LabelWorkerPanel) || !absent(tt, LabelPanel) {
			t.Errorf("%v: the board or worker panel shows", sel.Kind)
		}
	}
}

// The head says "Files", not the project name, now that the agent's todos
// and artifacts sit above it (D197).
func TestFilesPanelHeadSaysFiles(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.openProject = "contr"
	tt.Frame()
	box(t, tt, LabelPanelFiles+" name")
	if !absent(tt, "contr name") {
		t.Errorf("the head still shows the project: %q", tt.Texts())
	}
	box(t, tt, LabelPanelFiles) // the group keeps its name
}

// Without a title the head shows the panel name.
func TestPanelTitleFallsBackToName(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		kit.Panel(c, kit.PanelOpts{Name: "Files", Body: func() {}})
	}, 300, 200)
	tt.Frame()
	if !slices.Contains(tt.Texts(), "Files name") {
		t.Errorf("no head text: %q", tt.Texts())
	}
}

// The tree draws no disclosure arrow: its color is clear, and the hover fill
// is the token's.
func TestTreeThemeHidesTheArrow(t *testing.T) {
	base := ui.DarkTheme()
	got := treeTheme(base)
	if got.TextMuted != ui.Transparent {
		t.Error("the arrow color is not clear")
	}
	if got.SurfaceHover != theme.Current().Hover {
		t.Error("the hover fill is not the hover token")
	}
	if base.TextMuted == ui.Transparent {
		t.Error("treeTheme changed the window theme")
	}
}

// Folders show a closed or open folder icon; Go and Markdown files their own.
func TestFileIconByKind(t *testing.T) {
	for _, c := range []struct {
		name      string
		dir, open bool
		want      string
	}{
		{"src", true, false, "folder"}, {"src", true, true, "folder-open"},
		{"main.go", false, false, "file-go"}, {"A.MD", false, false, "file-md"},
		{"go.mod", false, false, "file"}, {"NOTICE", false, false, "file"},
	} {
		if got := fileIcon(c.name, c.dir, c.open); got != c.want {
			t.Errorf("fileIcon(%q, %v, %v) = %q, want %q", c.name, c.dir, c.open, got, c.want)
		}
	}
}
