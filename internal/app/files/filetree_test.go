package files

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
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
		kit.Panel(c, kit.PanelOpts{Name: "Files", Head: func() { r.ft.Head(c) }, Body: func() { r.ft.View(c) }})
	}, theme.SizeFilesWidth, 600)
	return r
}

func (r *treeRig) frame() { r.tt.Frame() }

// reset forgets the folder, so the next frame reads it again with the hooks
// the test set after the tester built its first frame.
func (r *treeRig) reset() { r.ft.setDir(""); r.stops = 0 }

func (r *treeRig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

// all builds a frame, picks All on the switch and builds the next frame.
func (r *treeRig) all() {
	r.t.Helper()
	r.frame()
	if err := r.tt.Click(LabelAll); err != nil {
		r.t.Fatal(err)
	}
	r.frame()
}

// changedRepo is a repo on branch "feature" with a modified, a new, a deleted
// and an ignored file, and a folder with a file in it.
func changedRepo(t *testing.T) string {
	s := apptest.NewRepo(t)
	apptest.Write(t, s.Dir, ".gitignore", "*.log\nbuild/\n")
	apptest.Write(t, s.Dir, "mod.go", "1\n2\n3\n")
	apptest.Write(t, s.Dir, "gone.md", "a\nb\n")
	apptest.Write(t, s.Dir, "src/deep/inner.go", "x\n")
	apptest.Write(t, s.Dir, "same.txt", "same\n")
	s.Git("add", "-A")
	s.Git("commit", "-m", "files")
	s.Git("checkout", "-b", "feature")
	apptest.Write(t, s.Dir, "mod.go", "1\n2\n3\n4\n5\n6\n") // +3
	os.Remove(filepath.Join(s.Dir, "gone.md"))
	apptest.Write(t, s.Dir, "fresh.go", "a\nb\nc\nd\n")
	apptest.Write(t, s.Dir, "noise.log", "ignored\n")
	apptest.Write(t, s.Dir, "build/out.bin", "x")
	return s.Dir
}

func TestFileTreeShowsFilesWithCountsAndHidesIgnored(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.all()
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
	r.all()
	src, _ := r.tt.Find("src")
	first, _ := r.tt.Find("base.txt")
	if src.Y >= first.Y {
		t.Errorf("folder src at y %v is not above the file base.txt at y %v", src.Y, first.Y)
	}
}

func TestFileTreeReadsAFolderWhenItOpens(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.all()
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

// A folder with changes opens on Changed: only the changed files, with the
// count in the switch (D264). All shows the whole tree again.
func TestFileTreeOpensOnChangedWhenTheFolderHasChanges(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.frame()
	if !r.ft.OnlyChanged() {
		t.Fatal("a folder with changes does not open on Changed")
	}
	if !r.has(ChangedLabel(3, true)) || ChangedLabel(3, true) != "Changed 3" {
		t.Errorf("no segment %q; texts %q", ChangedLabel(3, true), r.tt.Texts())
	}
	for _, want := range []string{"mod.go, +3 −0", "fresh.go, +4", "gone.md, −2"} {
		if !r.has(want) {
			t.Errorf("Changed: no row %q; texts %q", want, r.tt.Texts())
		}
	}
	for _, no := range []string{"same.txt", "base.txt", "src"} {
		if r.has(no) {
			t.Errorf("Changed: unchanged %q shows", no)
		}
	}
	r.all()
	if r.ft.OnlyChanged() || !r.has("same.txt") {
		t.Errorf("All: the unchanged file is not back; texts %q", r.tt.Texts())
	}
	if err := r.tt.Click("Changed 3"); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if !r.ft.OnlyChanged() || r.has("same.txt") {
		t.Errorf("Changed again: texts %q", r.tt.Texts())
	}
}

// A folder without changes opens on All.
func TestFileTreeOpensOnAllWithoutChanges(t *testing.T) {
	s := apptest.NewRepo(t)
	r := newTreeRig(t, s.Dir)
	r.frame()
	if r.ft.OnlyChanged() || !r.has("base.txt") || !r.has("Changed 0") {
		t.Errorf("a clean folder: Changed %v, texts %q", r.ft.OnlyChanged(), r.tt.Texts())
	}
}

// The owner's pick stays with its folder while the app runs; another folder
// opens on its own default.
func TestFileTreePickStaysWithTheFolder(t *testing.T) {
	a, b := changedRepo(t), changedRepo(t)
	r := newTreeRig(t, a)
	r.all()
	r.dir = b
	r.frame()
	if !r.ft.OnlyChanged() {
		t.Error("the pick of folder a moved to folder b")
	}
	r.dir = a
	r.frame()
	if r.ft.OnlyChanged() || !r.has("same.txt") {
		t.Errorf("folder a lost its pick of All; texts %q", r.tt.Texts())
	}
}

// With no folder the head has no switch.
func TestFileTreeNoFolderHasNoSwitch(t *testing.T) {
	r := newTreeRig(t, "")
	r.frame()
	if r.has(LabelFilesShown) || r.has(LabelAll) {
		t.Errorf("the switch shows without a folder: %q", r.tt.Texts())
	}
}

// The Changed view has one level of path rows: each folder with changed files
// is one row named by its path, its files under it; no unchanged rows (D264).
func TestFileTreeChangedViewCollapsesFoldersToPathRows(t *testing.T) {
	dir := changedRepo(t)
	apptest.Write(t, dir, "src/deep/inner.go", "x\ny\n")
	apptest.Write(t, dir, "src/top.go", "t\n")
	r := newTreeRig(t, dir)
	r.frame()
	for _, want := range []string{"src", "src/deep", "inner.go, +1 −0", "top.go, +1", "mod.go, +3 −0"} {
		if !r.has(want) {
			t.Errorf("no row %q; texts %q", want, r.tt.Texts())
		}
	}
	if r.has("deep") {
		t.Error("a folder of a path shows as a row of its own")
	}
	deep, inner := apptest.Box(t, r.tt, "src/deep"), apptest.Box(t, r.tt, "inner.go")
	src, top, mod := apptest.Box(t, r.tt, "src"), apptest.Box(t, r.tt, "top.go"), apptest.Box(t, r.tt, "mod.go")
	if !(src.Y < top.Y && top.Y < deep.Y && deep.Y < inner.Y && inner.Y < mod.Y) {
		t.Errorf("order: src %v top %v src/deep %v inner %v mod %v", src.Y, top.Y, deep.Y, inner.Y, mod.Y)
	}
	if got, want := inner.X-mod.X, float32(theme.Space16+theme.Space4); got != want {
		t.Errorf("a file under a path row indents %v, want one level %v", got, want)
	}
	r.tt.Click("src/deep")
	r.frame()
	if r.has("inner.go") {
		t.Error("a click does not close a path row")
	}
}

func TestFileInksAndCounts(t *testing.T) {
	p := theme.Dark
	cases := []struct {
		name         string
		f            review.File
		changed      bool
		pathRow      bool
		ink, icon    ui.Color
		strike       bool
		counts       bool
		only         int
		added, rmved ui.Color
	}{
		{"default", review.File{}, false, false, p.Ink48, p.Ink30, false, false, 0, ui.Color{}, ui.Color{}},
		{"changed", review.File{Status: review.Modified, Added: 2, Removed: 1}, true, false, p.Ink88, p.Ink30, false, true, 0, p.Ink48, p.Ink48},
		{"new", review.File{Status: review.Added, Added: 5}, true, false, p.Ink88, p.Ink30, false, true, 1, p.Ink48, p.Ink48},
		{"deleted", review.File{Status: review.Deleted, Removed: 3}, true, false, p.Ink48, p.Ink30, true, true, 2, p.Ink48, p.Ink48},
		{"path row", review.File{}, false, true, p.Ink88, p.Ink30, false, false, 0, ui.Color{}, ui.Color{}},
		{"binary", review.File{Status: review.Modified, Binary: true}, true, false, p.Ink88, p.Ink30, false, false, 0, ui.Color{}, ui.Color{}},
	}
	for _, c := range cases {
		ink, icon, strike := fileInks(p, c.f, c.changed, c.pathRow)
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
	apptest.Write(t, a, "alpha.txt", "x")
	apptest.Write(t, b, "beta.txt", "x")
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
	if r.has("beta.txt") || !r.tt.HasText("Files of the open project show here") {
		t.Errorf("no folder shows %q", r.tt.Texts())
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
	apptest.Write(t, dir, "one.txt", "x")
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
	apptest.Write(t, dir, "two.txt", "x")
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
	r.all()
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
	apptest.Write(t, dir, "plain.txt", "x\n")
	apptest.Write(t, dir, "skip.log", "x\n")
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
	if err := r.tt.Click("Changed 0"); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if !r.tt.HasText("No changes") {
		t.Errorf("Changed, nothing changed: %q", r.tt.Texts())
	}
}

func TestFileTreeKeysMoveAndOpen(t *testing.T) {
	r := newTreeRig(t, changedRepo(t))
	r.all()
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

// The content of a row sits 20px inside the panel line: panel padding 10 plus
// row padding 10, then the 15px icon and space-7 before the name. Each level
// indents space-16 plus space-4 more (D228).
func TestFileTreeRowsSit20pxFromThePanelSide(t *testing.T) {
	dir := changedRepo(t)
	apptest.Write(t, dir, "src/deep/inner.go", "x\ny\n")
	r := newTreeRig(t, dir)
	r.all()
	r.tt.Click("src")
	r.frame()
	r.tt.Click("deep")
	r.frame()
	panel := apptest.Box(t, r.tt, "Files")
	// A changed row is labeled with its counts, so these find the name texts.
	top, nested := apptest.Box(t, r.tt, "mod.go"), apptest.Box(t, r.tt, "inner.go")
	const line = 1 // the panel line
	if got, want := top.X-panel.X, float32(line+theme.Space10+theme.Space10+15+theme.Space7); got != want {
		t.Errorf("top-level name at %v from the panel side, want %v", got, want)
	}
	if got, want := nested.X-top.X, float32(2*(theme.Space16+theme.Space4)); got != want {
		t.Errorf("a name two levels down indents %v, want %v", got, want)
	}
}

// The indent of a nested row is plain: no guide line per level (D258).
func TestFileTreeHasNoGuides(t *testing.T) {
	dir := changedRepo(t)
	apptest.Write(t, dir, "src/deep/inner.go", "x\ny\n")
	r := newTreeRig(t, dir)
	r.frame()
	panel, name := apptest.Box(t, r.tt, "Files"), apptest.Box(t, r.tt, "inner.go")
	img := r.tt.Image()
	y := int(name.Y + name.H/2)
	// From the row's start (inside the panel line and padding) to its icon:
	// the indent of two levels.
	x0 := int(panel.X) + 1 + theme.Space10 + 2
	want := img.RGBAAt(x0, y)
	for x := x0; x < int(name.X)-15-theme.Space7-2; x++ {
		if got := img.RGBAAt(x, y); got != want {
			t.Fatalf("%v at x %d in the indent of inner.go %+v, want %v", got, x, name, want)
		}
	}
}

// worktreesRepo is changedRepo with two nested worktrees under
// .claude/worktrees: git lists each as one untracked folder whose path ends
// with "/" (#693). It also adds a file in src/deep, so the view has a path row
// of a tracked folder.
func worktreesRepo(t *testing.T) string {
	dir := changedRepo(t)
	apptest.Write(t, dir, "src/deep/new.go", "n\n")
	for _, name := range []string{"agent-a004bd4", "agent-b19f2c0"} {
		wt := filepath.Join(dir, ".claude", "worktrees", name)
		apptest.Write(t, wt, "main.go", "package main\n")
		cmd := exec.Command("git", "init", "-q", "-b", "main")
		cmd.Dir = wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	return dir
}

// An untracked folder is one row with its path, no row under it and no counts
// (#693). A click opens nothing; ⌘-click reveals it.
func TestFileTreeUntrackedFolderIsOneRow(t *testing.T) {
	dir := worktreesRepo(t)
	r := newTreeRig(t, dir)
	r.frame()
	if _, ok := r.ft.byPath[".claude/worktrees/agent-a004bd4/"]; !ok {
		t.Fatalf("git did not list the worktree as an untracked folder: %v", r.ft.changes)
	}
	a := apptest.Box(t, r.tt, ".claude/worktrees/agent-a004bd4")
	b := apptest.Box(t, r.tt, ".claude/worktrees/agent-b19f2c0")
	if b.Y-a.Y != theme.SizeFileRowHeight {
		t.Errorf("rows at %v and %v: want the second right under the first, with no row between", a.Y, b.Y)
	}
	if !apptest.Absent(r.tt, "main.go") {
		t.Errorf("a file of the worktree shows: %q", r.tt.Texts())
	}
	if err := r.tt.Click(".claude/worktrees/agent-a004bd4"); err != nil {
		t.Fatal(err)
	}
	r.frame()
	if c := apptest.Box(t, r.tt, ".claude/worktrees/agent-b19f2c0"); c.Y != b.Y || len(r.opened) != 0 {
		t.Errorf("a click changed the rows (next row at %v, was %v) or opened %v", c.Y, b.Y, r.opened)
	}
	if err := r.tt.ClickWith(ui.Cmd, ".claude/worktrees/agent-a004bd4"); err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(dir, ".claude", "worktrees", "agent-a004bd4")}; !slices.Equal(r.revealed, want) {
		t.Errorf("revealed %v, want %v", r.revealed, want)
	}
	// In All the folder is a folder as any other: it opens on a click.
	r.all()
	r.tt.Click(".claude")
	r.frame()
	if !r.has("worktrees") {
		t.Errorf("All: .claude did not open: %q", r.tt.Texts())
	}
}

// A path row of the Changed view gives its whole path as a tip, as a long
// path clips at its end (#693).
func TestFileTreePathRowHasTheWholePathAsATip(t *testing.T) {
	r := newTreeRig(t, worktreesRepo(t))
	r.frame()
	for _, label := range []string{".claude/worktrees/agent-a004bd4", "src/deep"} {
		if a, ok := r.tt.Access(label); !ok || a.Description != label {
			t.Errorf("%s: description %q (found %v), want the path", label, a.Description, ok)
		}
	}
	if a, _ := r.tt.Access("mod.go, +3 −0"); a.Description != "" {
		t.Errorf("a file row has a tip: %q", a.Description)
	}
}

// longTree is a tree that has read a folder with 20 changes, 18 of them
// untracked folders: taller than the panel (#693). It runs no read.
func longTree() *FileTree {
	t := &FileTree{dir: "/p", changesOK: true, Dir: func() string { return "/p" }}
	var list []review.File
	var root []review.Entry
	for i := range 18 {
		list = append(list, review.File{Path: fmt.Sprintf(".claude/worktrees/agent-a004bd4%02d/", i), Status: review.Added, Binary: true})
	}
	list = append(list, review.File{Path: "a.go", Status: review.Modified, Added: 2, Removed: 1},
		review.File{Path: "z.go", Status: review.Modified, Added: 1})
	for i := range 30 {
		root = append(root, review.Entry{Name: fmt.Sprintf("f%02d.go", i)})
	}
	t.setChanges(list)
	t.loaded = map[string][]review.Entry{"": root}
	t.loading, t.opened, t.openFiltered = map[string]bool{}, map[string]*bool{}, map[string]*bool{}
	return t
}

// A list taller than its area scrolls inside it, in Changed and in All; the
// block above, the head and the footer keep their places (#693).
func TestFileTreeLongListScrollsInItsArea(t *testing.T) {
	for _, all := range []bool{false, true} {
		tree := longTree()
		last := "z.go, +1 −0"
		if all {
			tree.pick(false)
			last = "f29.go"
		}
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			kit.Panel(c, kit.PanelOpts{Name: "Files",
				Top:    func() { ui.Text(c, "top block") },
				Head:   func() { tree.Head(c) },
				Body:   func() { tree.View(c) },
				Footer: func() { ui.Text(c, "footer") }})
		}, theme.SizeFilesWidth, 400)
		tt.Frame()
		fixed := []string{"top block", "Files name", LabelFilesShown, "footer"}
		before := map[string]ui.Rect{}
		for _, l := range fixed {
			before[l] = apptest.Box(t, tt, l)
		}
		foot := before["footer"]
		if b, _ := tt.Find(last); b.H > 0 {
			t.Fatalf("all %v: %s shows before a scroll: %+v", all, last, b)
		}
		area := apptest.Box(t, tt, LabelFileTree)
		tt.Scroll(area.X+area.W/2, area.Y+area.H/2, 0, 2000)
		tt.Frame()
		b, _ := tt.Find(last)
		if b.H == 0 || b.Y+b.H > foot.Y {
			t.Errorf("all %v: after a scroll %s is at %+v, want it shown above the footer at %v", all, last, b, foot.Y)
		}
		for _, l := range fixed {
			if got := apptest.Box(t, tt, l); got != before[l] {
				t.Errorf("all %v: %s moved from %+v to %+v", all, l, before[l], got)
			}
		}
	}
}
