// Package files is the files panel: the file tree of the selected folder, its
// diff summary for the title bar, and the agent's session (todos and
// artifacts) above the tree.
package files

import (
	"context"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/reviewview"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/review"
)

// Element labels of the files panel. Tests and the e2e driver find its parts
// by them.
const (
	LabelFileTree   = "file tree"
	LabelFileFilter = "Changed files only"
)

// fileIndent is the indent of one level: space-16 plus space-4. Only the
// indent shows the level: no guide lines (D258).
const fileIndent = theme.Space16 + theme.Space4

// fileReadDebounce waits for a burst of file events to end before the tree is
// read again.
const fileReadDebounce = 150 * time.Millisecond

// FileTree is the tree of the files panel (docs/spec/FileTree.md): the
// folder of the selected agent, with +/− counts on the changed files. Methods
// and View run on the UI thread. The git reads and the folder reads run on
// goroutines and land through Post; a watch of the folder starts a new read.
type FileTree struct {
	// Dir gives the folder to show: the selected worktree, else the project
	// folder. "" shows an empty panel.
	Dir func() string
	// Open opens a file of the tree in the Review view; Reveal shows a path
	// in Finder. Either may be nil.
	Open   func(dir, rel string)
	Reveal func(abs string)
	// Env runs the reads off the UI thread, redraws and shows read errors.
	// The zero Env (a tree that no window owns) reads at once, so nothing
	// reaches the tree from another goroutine.
	Env view.Env
	// Watch watches dir and calls notify on a change, from any goroutine.
	// Nil uses mygo.WatchFiles.
	Watch func(dir string, notify func()) (stop func(), err error)
	// Debounce is the quiet time before a watch event reads again; nil uses
	// fileReadDebounce, and a negative value reads at once.
	Debounce time.Duration
	// Git runs git; nil runs the `git` command. Timeout is the time limit of
	// one read; 0 is reviewview.GitReadTimeout.
	Git     git.InputRunner
	Timeout time.Duration

	dir     string
	repo    review.Repo
	changes []review.File
	byPath  map[string]review.File
	// changesOK is true when the last read of the changes worked.
	changesOK bool
	// loaded holds the entries of each folder that was read, by path ("" is
	// the root); loading, the folders being read.
	loaded  map[string][]review.Entry
	loading map[string]bool
	opened  map[string]*bool
	// onlyChanged is the filter toggle; its folders have a state of their own.
	onlyChanged  bool
	openFiltered map[string]*bool
	err          string
	// reads drops the result of an older read and ends its context: a newer
	// read started or the tree closed. session counts the folders shown, so a
	// watch event of an old folder is dropped.
	reads     view.Latest
	session   uint64
	stopWatch func()
	mu        sync.Mutex
	timer     *time.Timer
}

// NewFileTree returns a tree that shows dir() once it is built.
func NewFileTree(dir func() string) *FileTree {
	return &FileTree{Dir: dir}
}

// Sync points the tree at the folder that Dir gives, even when no view draws it. The frame calls
// it after each change that can move the folder (#585), so the diff summary stays current while
// the files panel is hidden. Cheap when the folder did not change; it reads on a goroutine.
func (t *FileTree) Sync() { t.sync() }

// Summary is the total of the changes that the tree lists: what the DiffSummary shows. ok is
// false until the folder was read, and after a read that failed.
func (t *FileTree) Summary() (sum review.Summary, ok bool) {
	if t.dir == "" || !t.changesOK {
		return review.Summary{}, false
	}
	return review.Total(t.changes), true
}

// Shown is the folder that the tree shows now.
func (t *FileTree) Shown() string { return t.dir }

// OnlyChanged tells whether the filter shows the changed files only.
func (t *FileTree) OnlyChanged() bool { return t.onlyChanged }

// Close ends the watch and the reads in flight. Call it when the window ends.
func (t *FileTree) Close() { t.setDir("") }

// sync points the tree at the folder that Dir gives. Cheap when it did not
// change.
func (t *FileTree) sync() {
	dir := ""
	if t.Dir != nil {
		dir = t.Dir()
	}
	if dir != t.dir {
		t.setDir(dir)
	}
}

// setDir shows dir: it drops what was read, reads the changes and the root
// folder on a goroutine, and watches the folder.
func (t *FileTree) setDir(dir string) {
	if t.stopWatch != nil {
		t.stopWatch()
		t.stopWatch = nil
	}
	t.mu.Lock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.mu.Unlock()
	t.dir, t.session = dir, t.session+1
	t.reads.Reset()
	t.repo = review.Repo{Dir: dir, Base: reviewview.ReviewBase, Git: t.Git}
	t.changes, t.byPath, t.err, t.changesOK = nil, nil, "", false
	t.loaded, t.loading = map[string][]review.Entry{}, map[string]bool{}
	t.opened, t.openFiltered = map[string]*bool{}, map[string]*bool{}
	if dir == "" {
		t.Env.Redraw()
		return
	}
	t.read(nil)
	t.watch(dir)
}

// read reads the changes and the folders in rels again (and the root), on a
// goroutine. It also reads the folders that are loading, so a result that a
// newer read dropped is not lost.
func (t *FileTree) read(rels []string) {
	t.reads.Reset()
	repo := t.repo
	want := map[string]bool{"": true}
	for _, r := range rels {
		want[r] = true
	}
	for r := range t.loading {
		want[r] = true
	}
	for r := range want {
		t.loading[r] = true
	}
	t.reads.Read(t.Env, t.limit(), func(ctx context.Context) func() {
		changes, cerr := repo.Changes(ctx)
		entries := map[string][]review.Entry{}
		errs := map[string]error{}
		for rel := range want {
			es, err := repo.Entries(ctx, rel)
			entries[rel], errs[rel] = es, err
		}
		return func() {
			t.setChanges(changes)
			t.changesOK = cerr == nil
			for rel := range want {
				delete(t.loading, rel)
				t.loaded[rel] = entries[rel] // nil after an error, so it is not read again
				if errs[rel] != nil {
					t.fail(errs[rel])
				}
			}
			if cerr != nil {
				t.fail(cerr)
			}
			t.Env.Redraw()
		}
	})
}

// loadDir reads one folder when the owner opens it.
func (t *FileTree) loadDir(rel string) {
	if _, ok := t.loaded[rel]; ok || t.loading[rel] {
		return
	}
	t.loading[rel] = true
	repo := t.repo
	t.reads.Read(t.Env, t.limit(), func(ctx context.Context) func() {
		es, err := repo.Entries(ctx, rel)
		return func() {
			delete(t.loading, rel)
			t.loaded[rel] = es // nil after an error, so it is not read again
			if err != nil {
				t.fail(err)
			}
			t.Env.Redraw()
		}
	})
}

// limit is the time limit of one read.
func (t *FileTree) limit() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return reviewview.GitReadTimeout
}

func (t *FileTree) fail(err error) {
	t.err = err.Error()
	t.Env.Show(t.err)
}

func (t *FileTree) setChanges(list []review.File) {
	t.changes = list
	t.byPath = make(map[string]review.File, len(list))
	for _, f := range list {
		t.byPath[f.Path] = f
	}
}

// watch starts the watch of dir. A watch that cannot start leaves the tree
// without live updates and says why.
func (t *FileTree) watch(dir string) {
	session := t.session
	watch := t.Watch
	if watch == nil {
		watch = watchFolder
	}
	stop, err := watch(dir, func() { t.changed(session) })
	if err != nil {
		t.fail(err)
		return
	}
	t.stopWatch = stop
}

// changed is a watch event. It waits for a quiet moment, then reads again.
// It runs on any goroutine.
func (t *FileTree) changed(session uint64) {
	wait := t.Debounce
	if wait == 0 {
		wait = fileReadDebounce
	}
	fire := func() {
		t.Env.Post(func() {
			if session != t.session {
				return
			}
			t.refresh()
		})
	}
	if wait < 0 {
		fire()
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
	}
	t.timer = time.AfterFunc(wait, fire)
}

// refresh reads the changes and the folders that are open again.
func (t *FileTree) refresh() {
	if t.dir == "" {
		return
	}
	rels := make([]string, 0, len(t.loaded))
	for rel := range t.loaded {
		rels = append(rels, rel)
	}
	t.read(rels)
}

// watchFolder watches dir with mygo.WatchFiles: the changes under it, but not
// inside .git. Hints are enough: the tree reads again.
func watchFolder(dir string, notify func()) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{
		Recursive: true,
		HintsOnly: true,
		Ignore:    func(p string, isDir bool) bool { return isDir && path.Base(p) == ".git" },
	})
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer w.Close()
		for {
			if _, err := w.Next(ctx); err != nil {
				return // io.EOF once closed, or once the folder went away
			}
			notify()
		}
	}()
	return cancel, nil
}

// Head builds the filter toggle of the panel head.
func (t *FileTree) Head(c *ui.Context) {
	kit.IconButton(c, kit.IconButtonOpts{Icon: "filter", Label: LabelFileFilter, On: &t.onlyChanged,
		OnClick: t.Env.Redraw})
}

// View builds the tree. It reads nothing on the UI thread.
func (t *FileTree) View(c *ui.Context) {
	t.sync()
	ui.Column(c).Label(LabelFileTree).FillWidth().Children(func() {
		if t.dir == "" {
			t.note(c, "No folder")
			return
		}
		if t.err != "" && len(t.loaded) == 0 {
			t.note(c, t.err)
			return
		}
		shown := 0
		ui.Themed(c, treeTheme(c.Theme())).Background(ui.Transparent).FillWidth().Children(func() {
			ui.Tree(c, func() { shown = t.level(c, "", 0) })
		})
		if shown == 0 && t.ready() {
			if t.onlyChanged {
				t.note(c, "No changes")
			} else {
				t.note(c, "Empty folder")
			}
		}
	})
}

// treeArrow is the width of MyGo's disclosure arrow, which ui.Tree always
// builds in front of the row.
const treeArrow = 16

// treeTheme hides the disclosure arrow of ui.Tree (the mock has none: a
// folder shows its open or closed icon) and gives the row its hover fill.
// The arrow takes TextMuted, the hover fill SurfaceHover. Colors of the rows
// are set on the rows themselves.
func treeTheme(base *ui.Theme) *ui.Theme {
	t := *base
	t.TextMuted = ui.Transparent
	t.SurfaceHover = theme.Current().Hover
	return &t
}

// ready tells whether the root has been read, so an empty tree is empty and
// not still loading.
func (t *FileTree) ready() bool {
	_, ok := t.loaded[""]
	return ok
}

func (t *FileTree) note(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(theme.Current().Ink42).FontSize(theme.StyleSmall.Size).
		Padding(theme.Space4, theme.Space10).SingleLine()
}

// entries lists the folder rel: with the filter on, the changed paths under
// it; else the folder as read, with the deleted files put back.
func (t *FileTree) entries(rel string) []review.Entry {
	var list []review.Entry
	seen := map[string]bool{}
	add := func(name string, dir bool) {
		if !seen[name] {
			seen[name] = true
			list = append(list, review.Entry{Name: name, Dir: dir})
		}
	}
	if !t.onlyChanged {
		for _, e := range t.loaded[rel] {
			add(e.Name, e.Dir)
		}
	}
	prefix := ""
	if rel != "" {
		prefix = rel + "/"
	}
	for _, f := range t.changes {
		if !strings.HasPrefix(f.Path, prefix) {
			continue
		}
		if !t.onlyChanged && f.Status != review.Deleted {
			continue
		}
		rest := f.Path[len(prefix):]
		if name, _, nested := strings.Cut(rest, "/"); nested {
			add(name, true)
		} else {
			add(rest, false)
		}
	}
	review.SortEntries(list)
	return list
}

// openFor returns the open state of a folder. With the filter on, folders
// start open.
func (t *FileTree) openFor(rel string) *bool {
	m := t.opened
	if t.onlyChanged {
		m = t.openFiltered
	}
	if v, ok := m[rel]; ok {
		return v
	}
	v := new(bool)
	*v = t.onlyChanged
	m[rel] = v
	return v
}

// level builds the items of folder rel and returns how many it built.
func (t *FileTree) level(c *ui.Context, rel string, depth int) int {
	list := t.entries(rel)
	for _, e := range list {
		t.item(c, rel, e, depth)
	}
	return len(list)
}

func fileIcon(name string, dir, open bool) string {
	switch {
	case dir && open:
		return "folder-open"
	case dir:
		return "folder"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return "file-go"
	case ".md":
		return "file-md"
	}
	return "file"
}

// FileLabel is the label of a row for the screen reader and the tests: the
// name, then the counts that show.
func FileLabel(name string, f review.File, changed bool) string {
	if !changed || f.Binary {
		return name
	}
	switch {
	case f.Status == review.Added:
		return name + ", " + "+" + strconv.Itoa(f.Added)
	case f.Status == review.Deleted:
		return name + ", −" + strconv.Itoa(f.Removed)
	case f.Added == 0 && f.Removed == 0:
		return name
	}
	return name + ", " + kit.CountsText(f.Added, f.Removed)
}

// fileInks gives the ink of the name and of the icon, and whether the name
// has a line through it (docs/spec/FileTree.md, States). changed is false for a
// folder and for a file git does not list.
func fileInks(p *theme.Palette, f review.File, changed bool) (name, icon ui.Color, strike bool) {
	switch {
	case changed && f.Status == review.Deleted:
		return p.Ink24, p.Ink22, true
	case changed:
		return p.Ink90, p.Ink38, false
	}
	return p.Ink50, p.Ink38, false
}

// fileCounts gives how the counts of a file look, and false when the row
// shows none: not changed, binary, or no line changed.
func fileCounts(p *theme.Palette, f review.File, changed bool) (kit.CountsLook, bool) {
	if !changed || f.Binary || (f.Added == 0 && f.Removed == 0) {
		return kit.CountsLook{}, false
	}
	look := kit.CountsLook{Added: p.Ink40, Removed: p.Ink24, Gap: theme.Space6, Style: theme.StyleMeta}
	switch f.Status {
	case review.Added:
		look.Only = kit.CountsAddedOnly
	case review.Deleted:
		look.Only, look.Added, look.Removed = kit.CountsRemovedOnly, p.Ink22, p.Ink22
	}
	return look, true
}

// item builds one row. A folder reads its entries when it opens.
func (t *FileTree) item(c *ui.Context, rel string, e review.Entry, depth int) {
	p := theme.Current()
	full := path.Join(rel, e.Name)
	f, changed := t.byPath[full]
	var open *bool
	var kids func()
	if e.Dir {
		open = t.openFor(full)
		kids = func() {
			if !t.onlyChanged {
				t.loadDir(full)
			}
			t.level(c, full, depth+1)
		}
	}
	isOpen := open != nil && *open
	name, icon, deleted := fileInks(p, f, changed && !e.Dir)
	label := FileLabel(e.Name, f, changed && !e.Dir)
	row := ui.TreeItem(c.Key(full), "", open, kids).Label(label).Height(theme.SizeFileRowHeight).
		Gap(0).Radius(theme.Radius6).Cursor(ui.CursorPointer).
		PaddingStart(theme.Space10 + float32(depth*fileIndent)).PaddingEnd(theme.Space10)
	row.Children(func() {
		// The margin takes back the room of the hidden arrow.
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space7).MarginStart(-treeArrow).Children(func() {
			if s := icons.Line(fileIcon(e.Name, e.Dir, isOpen)); s != nil {
				ui.Icon(c, s).Size(15, 15).Shrink(0).TextColor(icon)
			}
			n := ui.Text(c, e.Name).SingleLine().Grow(1).MinWidth(0).TextColor(name).
				Font(theme.StyleRowSmall.Stack()).FontSize(theme.StyleRowSmall.Size).FontWeight(theme.StyleRowSmall.Weight)
			if deleted {
				n.Strikethrough()
			}
			if look, ok := fileCounts(p, f, changed && !e.Dir); ok {
				kit.CountsWith(c, f.Added, f.Removed, look)
			}
		})
	})
	if row.Clicked() {
		t.click(row, full, e.Dir, open)
	}
}

// click acts on a row: ⌘-click reveals in Finder; a click opens a file in
// Review and opens or closes a folder.
func (t *FileTree) click(row ui.Element, rel string, dir bool, open *bool) {
	if row.ClickModifiers()&ui.Cmd != 0 {
		if t.Reveal != nil {
			t.Reveal(filepath.Join(t.dir, filepath.FromSlash(rel)))
		}
		return
	}
	if dir {
		*open = !*open
		t.Env.Redraw()
		return
	}
	if t.Open != nil {
		t.Open(t.dir, rel)
	}
}
