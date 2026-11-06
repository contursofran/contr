package app

// The projects of the sidebar: one group for each repo that an hc terminal
// sits in, and one empty group for each stored project that no terminal sits
// in (D116). The view is in sidebar.go. Ported from the Rust contr module
// spaces; the worktree rows, the diff stats and the rescan timer are gone
// (D52, docs/spec/ProjectRow.md).
//
// MyGo builds the whole view on a redraw. The git scan runs on a goroutine
// and its result reaches the list through the post func. A branch change is
// not polled: the list watches the HEAD file of each repo (WatchHead) and
// scans again when it changes.

import (
	"context"
	"github.com/contursofran/contr/internal/app/kit"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
)

// hostSource is the host reading that the This Mac row shows. [host.Host] fits it.
type hostSource interface {
	Stats() (host.Stats, bool)
}

// SpacesList is the model of the sidebar's projects. Methods run on the UI
// thread.
type SpacesList struct {
	// OnNew runs when `+ new` of the Projects head is clicked. Nil: the
	// button does nothing.
	OnNew func()
	// OnProjects runs when the projects button is clicked (D113). Nil: the
	// button does nothing.
	OnProjects func()
	// OnSettings runs when the Settings button at the bottom of the sidebar is clicked
	// (D126). Nil: the button does nothing. UpdateReady tells whether a newer
	// contr waits; the button shows a dot then. Nil: no dot.
	OnSettings  func()
	UpdateReady func() bool
	// Plans gives the plan meters of the sidebar foot, left of the Settings
	// button (D189). Nil: no meters.
	Plans func() []kit.Meter
	// OnNewIn runs when `+ new` of an empty project is clicked, with the
	// project's folder (D116). Nil: the button does nothing.
	OnNewIn func(dir string)
	// OnReordered runs after a drag changed the order in the store, so the
	// owner can save it. Nil: nothing more happens.
	OnReordered func()
	// OnError tells a watch that failed. Nil: the error is dropped.
	OnError func(error)
	// WatchHead watches the file path until ctx ends, and calls changed on a
	// goroutine when the file changes (mygo.WatchFiles). Nil: no watch.
	WatchHead func(ctx context.Context, path string, changed func()) error
	// Host gives CPU and memory for the This Mac row. Nil shows none.
	Host hostSource

	// Source is what the context menu of an agent row calls. Nil: Interrupt
	// and Stop are off.
	Source AgentActions
	// Run runs the actions off the UI thread; Status tells their result.
	Run    Runner
	Status func(string)
	// Entry opens the pull request of an agent from its menu.
	Entry WorkerEntry
	// ModelOf names the model of an agent profile for line 2. Nil: none.
	ModelOf func(profile string) string
	// StatusModel gives the model that Claude Code's status line reported for
	// the agent in hc terminal address, for an agent with no --model (D174).
	// Nil: none.
	StatusModel func(address string) string
	// Titles gives the issue titles of the worker rows. Nil: none.
	Titles *IssueTitles
	// Ages is the one timer of all ages (D137). Nil: ages move on a state
	// change only.
	Ages *AgeClock
	// Now is the clock; nil means time.Now. Tests replace it.
	Now func() time.Time

	// compact makes the rows icons only (D128). The frame sets it on each
	// frame from the window width.
	compact bool

	snap      func() agentstate.Snapshot
	selection *state.Selection

	// scanFn reads git. It runs on a goroutine, started by start.
	scanFn func([]string) git.Scan
	start  func(func())
	// post runs fn on the UI thread (Window.Update). Nil runs it at once.
	post func(func())

	groups []spaces.RepoGroup
	// projects is the owner's order and hidden groups (D109). Nil keeps the
	// tree order and hides nothing. UI thread only.
	projects *projects.Store
	// Adopt makes the list add a project found from a live terminal to the
	// store (D149). The frame sets it once the store has loaded well, so that
	// a store that failed to read is never written over.
	Adopt bool
	// OnAdopted runs on the UI thread after the list added projects to the
	// store: the frame saves the store, off the UI thread.
	OnAdopted func()
	// now is the clock; tests replace it.
	now    func() time.Time
	online bool
	// line is the hc problem while contr does not follow the core, else "".
	line string
	// folded holds the CommonDir of the projects the owner folded.
	folded map[string]bool

	scan    git.Scan
	scanned []string
	// gen counts the scans started and the scans dropped. A finished scan of
	// an older generation is dropped, so only the newest result lands.
	gen   uint64
	scans int
	// list is the state of the project list.
	list ui.ListState
	// cursor is the row the arrow keys are on: -1, or the selected row.
	cursor int
	// ByKey (may be nil) wraps a selection made by the keys, so the frame knows
	// the new terminal must not take the focus from the list (D145).
	ByKey func(do func())
	// OnOpen runs on Enter in the list: the terminal takes the focus.
	OnOpen func()
	// heads holds the cancel func of the HEAD watch of each repo.
	heads  map[string]context.CancelFunc
	closed bool
	// HoldLaunch makes the launch pick wait for the stored project order
	// ([SpacesList.ReleaseLaunch]). The frame sets it while it loads the store.
	HoldLaunch bool
	// launched is true once the launch pick ran, or the owner chose first.
	launched bool
}

// NewSpacesList returns the list. snap (may be nil) gives the hc terminals,
// selection the clicks. post (may be nil) runs a func on the UI thread, as
// Window.Update does. The list is empty until [SpacesList.Sync], which the
// owner calls at the start and when the agents changed.
func NewSpacesList(snap func() agentstate.Snapshot, selection *state.Selection, post func(func())) *SpacesList {
	return &SpacesList{
		snap: snap, selection: selection, post: post,
		scanFn: git.ScanCwds, start: func(f func()) { go f() },
		folded: map[string]bool{}, heads: map[string]context.CancelFunc{},
	}
}

// Groups returns the projects shown. Empty without terminals or stored
// projects.
func (l *SpacesList) Groups() []spaces.RepoGroup { return l.groups }

// Online tells whether contr follows the hc core, and the hc problem when it
// does not.
func (l *SpacesList) Online() (bool, string) { return l.online, l.line }

// SetProjects sets the store that orders and hides the groups (D109) and
// rebuilds the list. Call it on the UI thread.
func (l *SpacesList) SetProjects(s *projects.Store) {
	l.projects = s
	l.HoldLaunch = false
	l.rebuild()
}

// ReleaseLaunch ends the wait of the launch pick when the store could not be
// read, and tries the pick with the order that it has.
func (l *SpacesList) ReleaseLaunch() {
	l.HoldLaunch = false
	l.pickLaunch()
}

// pickLaunch selects, once, the first live orchestrator in sidebar order, else
// the first live worker (D173). It does nothing when the owner chose a row
// first, and it stays quiet while nothing live shows, so a later sync tries
// again. After the pick the owner's choices are never replaced.
func (l *SpacesList) pickLaunch() {
	if l.launched || l.HoldLaunch || l.selection == nil {
		return
	}
	if _, ok := l.selection.Current(); ok {
		l.launched = true
		return
	}
	var worker *agentLine
	for _, it := range l.items() {
		a := it.agent
		if a == nil || !a.w.Live {
			continue
		}
		if a.orchestrator {
			l.launched = true
			l.selection.Select(l.target(it))
			return
		}
		if worker == nil {
			worker = a
		}
	}
	if worker != nil {
		l.launched = true
		l.selection.Select(state.Selected{Kind: state.KindWorker, ID: worker.w.Address})
	}
}

// applyProjects puts groups in the stored order and leaves the hidden ones
// out (D109). Without a store it returns groups as they are.
func (l *SpacesList) applyProjects(groups []spaces.RepoGroup) []spaces.RepoGroup {
	if l.projects == nil {
		return groups
	}
	groups = l.withStored(groups)
	if len(groups) == 0 {
		return groups
	}
	dirs := make([]string, len(groups))
	byDir := make(map[string]spaces.RepoGroup, len(groups))
	for i, g := range groups {
		dirs[i] = g.CommonDir
		byDir[g.CommonDir] = g
	}
	out := make([]spaces.RepoGroup, 0, len(groups))
	var empty []spaces.RepoGroup
	for _, d := range l.projects.Order(dirs) {
		switch g := byDir[d]; {
		case l.projects.Hidden(d):
		case g.Path != "":
			empty = append(empty, g) // nothing open: after the others (#443, D174)
		default:
			out = append(out, g)
		}
	}
	return append(out, empty...)
}

// withStored adds an empty group for each stored project that no hc terminal
// sits in (D116). It waits for the first scan of the terminals, so that a
// project with terminals does not show as empty for a moment.
func (l *SpacesList) withStored(groups []spaces.RepoGroup) []spaces.RepoGroup {
	if len(l.scanned) > 0 && l.scan.Repos == nil {
		return groups
	}
	for _, d := range l.projects.Folders() {
		if slices.ContainsFunc(groups, func(g spaces.RepoGroup) bool { return g.CommonDir == d }) {
			continue
		}
		groups = append(groups, spaces.RepoGroup{CommonDir: d, Name: storedName(d, l.projects.Path(d)), Path: l.projects.Path(d)})
	}
	return groups
}

// storedName is the name of a stored project: the repo folder, as the scan
// names it.
func storedName(dir, path string) string {
	if filepath.Base(dir) == ".git" {
		return filepath.Base(filepath.Dir(dir))
	}
	return filepath.Base(path)
}

// Sync brings the list up to the hc terminals and the connection. It starts
// a scan when a terminal sits in another folder. With no folder there is
// nothing to scan, and the scan is dropped. It then rebuilds the groups from
// the scan that it has.
func (l *SpacesList) Sync() {
	cwds := spaces.Dirs(l.terms())
	switch {
	case len(cwds) == 0:
		l.forgetScan()
	case !slices.Equal(cwds, l.scanned):
		l.startScan(cwds)
	}
	l.dropGoneSelection()
	l.rebuild()
}

// Close stops the HEAD watches for good.
func (l *SpacesList) Close() {
	l.closed = true
	for dir, cancel := range l.heads {
		cancel()
		delete(l.heads, dir)
	}
}

// snapshot is the agent state, empty without a source.
func (l *SpacesList) snapshot() agentstate.Snapshot {
	if l.snap == nil {
		return agentstate.Snapshot{}
	}
	return l.snap()
}

// terms are the hc terminals: orchestrators, workers, then plain terminals.
func (l *SpacesList) terms() []spaces.Term {
	return spaceTerms(l.snapshot())
}

func spaceTerms(s agentstate.Snapshot) []spaces.Term {
	var out []spaces.Term
	for _, ws := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
		for _, w := range ws {
			if !w.Live && !w.Orchestrator() && !w.Plain() { // an ended worker holds no worktree (D112)
				continue
			}
			out = append(out, spaces.Term{Name: w.DisplayName(), Address: w.Address, Dir: w.Worktree, Status: workerIcon(w)})
		}
	}
	return out
}

func (l *SpacesList) forgetScan() {
	l.gen++
	l.scan, l.scanned = git.Scan{}, nil
}

func (l *SpacesList) startScan(cwds []string) {
	l.gen++
	gen := l.gen
	l.scanned = slices.Clone(cwds)
	l.scans++
	scanFn := l.scanFn
	l.start(func() {
		scan := scanFn(cwds)
		l.later(func() {
			if gen != l.gen {
				return
			}
			l.scan = scan
			l.rebuild()
		})
	})
}

// later runs fn on the UI thread.
func (l *SpacesList) later(fn func()) {
	if l.post != nil {
		l.post(fn)
		return
	}
	fn()
}

func (l *SpacesList) rebuild() {
	snap := l.snapshot()
	built := spaces.Build(spaceTerms(snap), l.scan)
	adopted := l.adopt(built)
	groups := l.applyProjects(built)
	l.online, l.line = true, ""
	if l.snap != nil && snap.CoreVersion == "" {
		l.online, l.line = false, snap.Problem
	}
	l.groups = groups
	l.syncHeads()
	l.syncAges()
	l.pickLaunch()
	if adopted && l.OnAdopted != nil {
		l.OnAdopted()
	}
}

// adopt adds each project of groups that the store does not know to the
// store, after the projects shown (D149). A hidden project stays hidden. It
// changes the store in memory only; OnAdopted saves it off the UI thread. It
// tells if it added any.
func (l *SpacesList) adopt(groups []spaces.RepoGroup) bool {
	if l.projects == nil || !l.Adopt {
		return false
	}
	var fresh []spaces.RepoGroup
	for _, g := range groups {
		if !l.projects.Known(g.CommonDir) {
			fresh = append(fresh, g)
		}
	}
	if len(fresh) == 0 {
		return false
	}
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	for _, g := range fresh {
		path := l.scan.Repos[g.CommonDir].MainRoot
		l.projects.AddLive(g.CommonDir, path, now())
	}
	return true
}

// syncHeads watches the HEAD file of each repo that has terminals, and stops
// the watch of a repo that has none any more.
func (l *SpacesList) syncHeads() {
	if l.WatchHead == nil || l.closed {
		return
	}
	want := map[string]bool{}
	for _, g := range l.groups {
		if g.Path == "" {
			want[g.CommonDir] = true
		}
	}
	for dir, cancel := range l.heads {
		if !want[dir] {
			cancel()
			delete(l.heads, dir)
		}
	}
	for dir := range want {
		if _, ok := l.heads[dir]; !ok {
			l.watchHead(dir)
		}
	}
}

// watchHead starts the watch of dir's HEAD on a goroutine. A change scans
// again, so the branch is read again; a failed watch is reported.
func (l *SpacesList) watchHead(dir string) {
	ctx, cancel := context.WithCancel(context.Background())
	l.heads[dir] = cancel
	watch := l.WatchHead
	changed := func() { l.later(l.rescan) }
	l.start(func() {
		if err := watch(ctx, filepath.Join(dir, "HEAD"), changed); err != nil && ctx.Err() == nil {
			l.later(func() {
				if l.OnError != nil {
					l.OnError(err)
				}
			})
		}
	})
}

// rescan reads git again for the folders of the last scan.
func (l *SpacesList) rescan() {
	if l.closed || len(l.scanned) == 0 {
		return
	}
	l.startScan(l.scanned)
}

// Folded tells whether the owner folded the project.
func (l *SpacesList) Folded(commonDir string) bool { return l.folded[commonDir] }

// Fold folds or opens the project.
func (l *SpacesList) Fold(commonDir string) {
	if l.folded[commonDir] {
		delete(l.folded, commonDir)
	} else {
		l.folded[commonDir] = true
	}
	l.syncAges()
}

// Reorder moves the project rows before row to (len for the end) and stores
// the order (D109). OnReordered then lets the owner save it. The rows are
// those of the list, which holds the agent rows too: an agent row does not
// move, and a project goes before the project whose rows come at or after
// to.
func (l *SpacesList) Reorder(rows []int, to int) {
	if l.projects == nil {
		return
	}
	items := l.items()
	var moved []int
	for _, r := range rows {
		if r >= 0 && r < len(items) && items[r].agent == nil {
			moved = append(moved, items[r].group)
		}
	}
	if len(moved) == 0 {
		return
	}
	rows = moved
	target := len(l.groups)
	for i := max(to, 0); i < len(items); i++ {
		if items[i].agent == nil {
			target = items[i].group
			break
		}
	}
	if target == len(l.groups) {
		// At the end: before the empty projects, which show last.
		for i, g := range l.groups {
			if g.Path != "" {
				target = i
				break
			}
		}
	}
	// The stored order holds the empty projects at their own place. Move the
	// dragged projects there, before the project they were dropped on, and
	// not in the displayed order (D109, D174).
	shown := make([]string, len(l.groups))
	for i, g := range l.groups {
		shown[i] = g.CommonDir
	}
	stored := l.projects.Order(shown)
	drag := map[string]bool{}
	var dragged []string
	for _, r := range rows {
		if r >= 0 && r < len(l.groups) && !drag[l.groups[r].CommonDir] {
			drag[l.groups[r].CommonDir] = true
		}
	}
	before := ""
	if target < len(l.groups) {
		before = l.groups[target].CommonDir
	}
	out := make([]string, 0, len(stored))
	insert := func() { out = append(out, dragged...) }
	for _, d := range stored {
		if drag[d] {
			dragged = append(dragged, d)
		}
	}
	placed := false
	for _, d := range stored {
		if d == before && !placed {
			insert()
			placed = true
		}
		if !drag[d] {
			out = append(out, d)
		}
	}
	if !placed {
		insert()
	}
	l.projects.SetOrder(out)
	if l.OnReordered != nil {
		l.OnReordered()
		return
	}
	l.rebuild()
}

// Activate is a click on a project row: it selects the project's
// orchestrator, else the project. A click on the project that is already
// selected folds or opens it.
func (l *SpacesList) Activate(g spaces.RepoGroup) {
	if l.selection == nil {
		return
	}
	if sel, ok := l.selection.Current(); ok && l.holds(g, sel) {
		l.Fold(g.CommonDir)
		return
	}
	for _, o := range l.snapshot().Orchestrators {
		if l.holdsAddress(g, o.Address) {
			l.selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: o.Address})
			return
		}
	}
	l.selection.Select(state.Selected{Kind: state.KindWorktree, ID: projectRoot(g)})
}

// projectRoot is the folder that a project selects: its main checkout, else
// the first row's, else the folder of an empty project.
func projectRoot(g spaces.RepoGroup) string {
	for _, r := range g.Rows {
		if r.IsMain {
			return r.Root
		}
	}
	if len(g.Rows) > 0 {
		return g.Rows[0].Root
	}
	return g.Path
}

func (l *SpacesList) holdsAddress(g spaces.RepoGroup, address string) bool {
	for _, r := range g.Rows {
		if slices.ContainsFunc(r.Holders, func(h spaces.Holder) bool { return h.Address == address }) {
			return true
		}
	}
	return false
}

// holds tells whether the selection is the project or one of its terminals.
func (l *SpacesList) holds(g spaces.RepoGroup, sel state.Selected) bool {
	if sel.Kind == state.KindWorktree {
		return sel.ID == projectRoot(g)
	}
	return l.holdsAddress(g, sel.ID)
}
