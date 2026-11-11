package sidebar

// The projects of the sidebar: one group for each repo that an hc terminal
// sits in, and one empty group for each stored project that no terminal sits
// in (D116). The view is in sidebar.go. Ported from the Rust contr module
// spaces; the worktree rows, the diff stats and the rescan timer are gone
// (D52, docs/spec/ProjectRow.md).
//
// MyGo builds the whole view on a redraw. The git scan runs on a goroutine
// and its result reaches the list through Env. A branch change is
// not polled: the list watches the HEAD file of each repo (WatchHead) and
// scans again when it changes.

import (
	"context"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/kit"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/view"
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
	// OnRetry runs when the offline This Mac row is clicked: connect to hc
	// at once (ServerRow spec). Nil: the row has no click.
	OnRetry func()
	// Plans gives the plan meters of the sidebar foot, left of the Settings
	// button (D196). Nil: no meters.
	Plans func() []kit.Meter
	// OnNewIn runs when `+ new` of an empty project is clicked, with the
	// project's folder (D116). Nil: the button does nothing.
	OnNewIn func(dir string)
	// OnHide runs when "Hide project" of a project row's menu is chosen, with
	// the project key (D219). Nil: the item is off.
	OnHide func(dir string)
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
	// Env runs the git scans, the HEAD watches and the agent actions off the
	// UI thread, and shows the result of an action.
	Env view.Env
	// Entry opens the pull request of an agent from its menu.
	Entry ghsource.WorkerEntry
	// ModelOf names the model of an agent profile for line 2. Nil: none.
	ModelOf func(profile string) string
	// EffortOf names the effort of an agent profile for line 2. Nil: none.
	EffortOf func(profile string) string
	// StatusModel gives the model that Claude Code's status line reported for
	// the agent in hc terminal address, for an agent with no --model (D174).
	// Nil: none.
	StatusModel func(address string) string
	// StatusEffort gives the effort that Claude Code's status line reported
	// for the terminal at address, or "" (D189).
	StatusEffort func(address string) string
	// Titles gives the issue titles of the worker rows. Nil: none.
	Titles *ghsource.IssueTitles
	// Ages is the one timer of all ages (D137). Nil: ages move on a state
	// change only.
	Ages *AgeClock
	// Now is the clock; nil means time.Now. Tests replace it.
	Now func() time.Time

	// Compact makes the rows icons only (D128). The frame sets it on each
	// frame from the window width.
	Compact bool
	// Hints shows the key chips: ⌘ is held (docs/spec/KeyHints.md). The frame sets it on each
	// frame.
	Hints bool

	snap      func() agentstate.Snapshot
	selection *state.Selection

	// scanFn reads git. It runs off the UI thread, through Env.
	scanFn func([]string) git.Scan

	groups []spaces.RepoGroup
	// arrivals tells when the open items of an orchestrator go up (D240).
	arrivals kit.Arrivals
	// rowsSeen holds the address of each agent row of the last build once the
	// projects settled; nil before. rowsNew holds the rows that came since it, in
	// this build: they come in (kit.RowIn, D263).
	rowsSeen, rowsNew map[string]bool
	// projects is the owner's order and hidden groups (D109). Nil keeps the
	// tree order and hides nothing. UI thread only.
	projects *projects.Store
	// Adopt makes the list add a project found from a live terminal to the
	// store (D149). The frame sets it once the store has loaded well, so that
	// a store that failed to read is never written over.
	Adopt bool
	// OnGroups runs on the UI thread after each rebuild of the groups. Nil
	// does nothing.
	OnGroups func()
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
	// reads drops a finished scan when a newer scan started or the scan was
	// dropped, so only the newest result lands.
	reads view.Latest
	scans int
	// scanning is true from the start of a scan until its result lands or it is dropped.
	scanning bool
	// list is the state of the project list.
	list ui.ListState
	// cursor is the row the arrow keys are on: -1, or the selected row.
	cursor int
	// ByKey (may be nil) wraps a selection made by the keys, so the frame knows
	// the new terminal must not take the focus from the list (D145).
	ByKey func(do func())
	// OnOpen runs on Enter in the list: the terminal takes the focus.
	OnOpen func()
	// OnPick runs after a click on an agent row: the terminal takes the
	// focus, so the next key goes to the agent's prompt (#465).
	OnPick func()
	// heads holds the cancel func of the HEAD watch of each repo.
	heads  map[string]context.CancelFunc
	closed bool
	// HoldLaunch makes the launch pick wait for the stored project order
	// ([SpacesList.ReleaseLaunch]). The frame sets it while it loads the store.
	HoldLaunch bool
	// launched is true once the launch pick ran, or the owner chose first.
	launched bool
	// want is the address of an agent to select once its row shows, and
	// wantUntil the time it is dropped ([SpacesList.SelectWhenShown]).
	want      string
	wantUntil time.Time
}

// wantWait is how long [SpacesList.SelectWhenShown] waits for the row.
const wantWait = 30 * time.Second

// NewSpacesList returns the list. snap (may be nil) gives the hc terminals,
// selection the clicks. env runs the git reads off the UI thread. The list is empty until [SpacesList.Sync], which the
// owner calls at the start and when the agents changed.
func NewSpacesList(snap func() agentstate.Snapshot, selection *state.Selection, env view.Env) *SpacesList {
	return &SpacesList{
		snap: snap, selection: selection, Env: env, scanFn: git.ScanCwds,
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

// SelectWhenShown selects the agent at addr when its row shows: now, or after
// a later snapshot or scan (D233). It drops addr when the row does not show in
// wantWait. Call it on the UI thread.
func (l *SpacesList) SelectWhenShown(addr string) {
	l.want, l.wantUntil = addr, l.clock().Add(wantWait)
	l.pickWanted()
}

// pickWanted selects the wanted agent once a group holds its row, folded or
// not, and then forgets it.
func (l *SpacesList) pickWanted() {
	if l.want == "" || l.selection == nil {
		return
	}
	if l.clock().After(l.wantUntil) {
		l.want = ""
		return
	}
	s := l.Snapshot()
	for _, g := range l.groups {
		for _, a := range agentLines(g, s) {
			if a.w.Address != l.want {
				continue
			}
			l.want, l.launched = "", true
			l.selection.Select(l.target(sideItem{agent: &a}))
			return
		}
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
		groups = append(groups, spaces.RepoGroup{CommonDir: d, Name: spaces.StoredName(d, l.projects.Path(d)), Path: l.projects.Path(d)})
	}
	return groups
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

// Closed reports that Close ran. The frame tests check the order of the stops with it.
func (l *SpacesList) Closed() bool { return l.closed }

// Snapshot is the agent state, empty without a source.
func (l *SpacesList) Snapshot() agentstate.Snapshot {
	if l.snap == nil {
		return agentstate.Snapshot{}
	}
	return l.snap()
}

// terms are the hc terminals: orchestrators, workers, then plain terminals.
func (l *SpacesList) terms() []spaces.Term {
	return spaceTerms(l.Snapshot())
}

func spaceTerms(s agentstate.Snapshot) []spaces.Term {
	var out []spaces.Term
	for _, ws := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
		for _, w := range ws {
			if !w.Live && !w.Orchestrator() && !w.Plain() { // an ended worker holds no worktree (D112)
				continue
			}
			out = append(out, spaces.Term{Name: w.DisplayName(), Address: w.Address, Dir: w.Worktree, Status: tabmodel.WorkerIcon(w)})
		}
	}
	return out
}

// settled tells whether the groups are final: the stored projects are read and no scan
// runs. Until then an empty list is not "No projects yet" (D255).
func (l *SpacesList) settled() bool {
	return !l.HoldLaunch && !l.scanning
}

func (l *SpacesList) forgetScan() {
	l.reads.Reset()
	l.scan, l.scanned, l.scanning = git.Scan{}, nil, false
}

func (l *SpacesList) startScan(cwds []string) {
	l.reads.Reset()
	l.scanned = slices.Clone(cwds)
	l.scans++
	l.scanning = true
	scanFn := l.scanFn
	l.reads.Do(l.Env, func() func() {
		scan := scanFn(cwds)
		return func() {
			l.scan, l.scanning = scan, false
			l.rebuild()
		}
	})
}

func (l *SpacesList) rebuild() {
	snap := l.Snapshot()
	built := spaces.Build(spaceTerms(snap), l.scan)
	rekeyed := l.rekey()
	adopted := l.adopt(built) || rekeyed
	groups := l.applyProjects(built)
	l.online, l.line = true, ""
	if l.snap != nil && snap.CoreVersion == "" {
		l.online, l.line = false, snap.Problem
	}
	l.groups = groups
	l.syncHeads()
	l.syncAges()
	l.pickLaunch()
	l.pickWanted()
	if adopted && l.OnAdopted != nil {
		l.OnAdopted()
	}
	if l.OnGroups != nil {
		l.OnGroups()
	}
}

// rekey moves each stored project keyed by a folder that the scan found as
// the main checkout of a repository to the repository's common dir, so the
// project shows once after `git init` (D236). The fold state moves too. It
// reads the scan only; the scan read the files off the UI thread, and
// OnAdopted saves the store off the UI thread. It tells if it moved any.
func (l *SpacesList) rekey() bool {
	if l.projects == nil || !l.Adopt {
		return false
	}
	moved := false
	for dir, repo := range l.scan.Repos {
		if repo.MainRoot == "" || !l.projects.Rekey(repo.MainRoot, dir) {
			continue
		}
		moved = true
		if l.folded[repo.MainRoot] {
			delete(l.folded, repo.MainRoot)
			l.folded[dir] = true
		}
	}
	return moved
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
	changed := func() { l.Env.Post(l.rescan) }
	// The watch blocks until ctx ends, so it goes through Env.Run, which
	// starts a goroutine even in a zero Env.
	l.Env.Run.Do(func() func() {
		if err := watch(ctx, filepath.Join(dir, "HEAD"), changed); err != nil && ctx.Err() == nil {
			return func() {
				if l.OnError != nil {
					l.OnError(err)
				}
			}
		}
		return nil
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
	l.selectProject(g)
}

// Switch selects the project with this common dir, as a click on its row
// does, but never folds it (the projects modal, D207).
func (l *SpacesList) Switch(dir string) {
	if l.selection == nil {
		return
	}
	for _, g := range l.Groups() {
		if g.CommonDir == dir {
			l.selectProject(g)
			return
		}
	}
}

// openGroup is the index of the project that holds the selection; -1: none.
func (l *SpacesList) openGroup() int {
	if l.selection == nil {
		return -1
	}
	sel, ok := l.selection.Current()
	if !ok {
		return -1
	}
	for i, g := range l.groups {
		if l.holds(g, sel) {
			return i
		}
	}
	return -1
}

// projectStep is the index of the project that ⌘⌥← (d -1) or ⌘⌥→ (d 1) selects: the project
// shown before or after the open one, around the ends. Hidden projects are not shown, so they
// are not stepped to. With no open project ⌘⌥→ takes the first and ⌘⌥← the last. -1: none,
// with fewer than two projects.
func (l *SpacesList) projectStep(d int) int {
	n := len(l.groups)
	if n < 2 {
		return -1
	}
	at := l.openGroup()
	if at < 0 {
		if d < 0 {
			return n - 1
		}
		return 0
	}
	return (at + d + n) % n
}

// StepProject selects the project before (d -1) or after (d 1) the open one (⌘⌥←, ⌘⌥→), as a
// click on its row does: its orchestrator, else its main checkout. It is a window key, so the
// new terminal takes the focus as after a click. With one project it does nothing. UI thread.
func (l *SpacesList) StepProject(d int) {
	if i := l.projectStep(d); i >= 0 && l.selection != nil {
		l.selectProject(l.groups[i])
	}
}

// Orchestrated tells whether an orchestrator holds a terminal in project g.
func (l *SpacesList) Orchestrated(g spaces.RepoGroup) bool {
	for _, o := range l.Snapshot().Orchestrators {
		if l.holdsAddress(g, o.Address) {
			return true
		}
	}
	return false
}

// selectProject selects the orchestrator of g, else its main checkout.
func (l *SpacesList) selectProject(g spaces.RepoGroup) {
	for _, o := range l.Snapshot().Orchestrators {
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
