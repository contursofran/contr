package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/host"
)

// ---- Fake git layouts on disk (plain files, no `git` binary) ----

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mainRepo makes a main checkout at root with `.git/HEAD` = head and returns
// the git dir.
func mainRepo(t *testing.T, root, head string) string {
	t.Helper()
	gitDir := filepath.Join(root, ".git")
	writeFile(t, filepath.Join(gitDir, "HEAD"), head)
	return gitDir
}

// linkedWorktree makes a linked worktree at root, laid out as git writes it,
// and returns its admin dir.
func linkedWorktree(t *testing.T, common, name, root, head string) string {
	t.Helper()
	admin := filepath.Join(common, "worktrees", name)
	writeFile(t, filepath.Join(admin, "HEAD"), head)
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(root, ".git")+"\n")
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFile(t, filepath.Join(root, ".git"), "gitdir: "+admin+"\n")
	return admin
}

// canonicalTmp is a temporary directory in the canonical form that the scan's
// rows use.
func canonicalTmp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// oneWorktree makes a repo at base/repo with a linked worktree at base/wt1 on
// branch feature. It returns the worktree's root and its admin dir.
func oneWorktree(t *testing.T, base string) (string, string) {
	t.Helper()
	gitDir := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	root := filepath.Join(base, "wt1")
	return root, linkedWorktree(t, gitDir, "w1", root, "ref: refs/heads/feature\n")
}

// twoWorktrees makes base/wt1 on feature and base/wt2 on other.
func twoWorktrees(t *testing.T, base string) (string, string) {
	t.Helper()
	gitDir := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	wt1, wt2 := filepath.Join(base, "wt1"), filepath.Join(base, "wt2")
	linkedWorktree(t, gitDir, "w1", wt1, "ref: refs/heads/feature\n")
	linkedWorktree(t, gitDir, "w2", wt2, "ref: refs/heads/other\n")
	return wt1, wt2
}

// ---- Trees ----

func spPane(id, cwd string, agent *herdr.AgentState) herdr.Pane {
	p := herdr.Pane{ID: id, Tokens: map[string]string{}}
	if cwd != "" {
		p.Cwd = &cwd
	}
	if agent != nil {
		p.Agent = &herdr.AgentInfo{Name: "claude", Status: *agent}
	}
	return p
}

// spTree is one workspace with one tab, t1 labelled "a", holding panes.
func spTree(panes ...herdr.Pane) herdr.HerdrTree {
	return herdr.HerdrTree{Version: "0.9.3", Workspaces: []herdr.Workspace{{
		ID: "w1", Label: "contr", Status: herdr.StateIdle,
		Tabs: []herdr.Tab{{ID: "t1", Label: "a", Status: herdr.StateIdle, Panes: panes}},
	}}}
}

// ---- Harness ----

// spacesHarness shows the column alone in a window as wide as the sidebar.
// The scan runs at once on this goroutine, so a test sees its result.
type spacesHarness struct {
	t       *testing.T
	live    *fakeLive
	sel     *state.Selection
	list    *SpacesList
	board   *Board
	host    *fakeHost
	tt      *ui.Tester
	hidden  bool
	clock   time.Time
	focused []string
}

func newSpaces(t *testing.T, live herdr.LiveHerdr) *spacesHarness {
	t.Helper()
	h := &spacesHarness{t: t, live: &fakeLive{live: live}, sel: state.NewSelection(nil), board: NewDetachedBoard(),
		host: &fakeHost{}, clock: time.Unix(1_000_000, 0)}
	h.list = h.makeList(h.board)
	h.tt = ui.NewTester(h.list.View, theme.SidebarWidth, theme.WindowHeight)
	return h
}

func (h *spacesHarness) makeList(b *Board) *SpacesList {
	l := NewSpacesList(h.live, b, h.host, h.sel, nil, func() bool { return !h.hidden }, nil)
	l.start = func(f func()) { f() }
	l.now = func() time.Time { return h.clock }
	l.focus = func(id string, _ func(string)) { h.focused = append(h.focused, id) }
	l.Sync()
	return l
}

func spacesOf(tree herdr.HerdrTree) herdr.LiveHerdr { return connectedWith(tree) }

// send replaces the live state, as the reader would, and draws a frame. It
// returns what the sync changed.
func (h *spacesHarness) send(l herdr.LiveHerdr) SpacesChange {
	h.t.Helper()
	h.live.setLive(l)
	change := h.list.Sync()
	h.tt.Frame()
	return change
}

func (h *spacesHarness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *spacesHarness) group(g int) spaces.RepoGroup {
	h.t.Helper()
	groups := h.list.Groups()
	if g >= len(groups) {
		h.t.Fatalf("no group %d in %d groups", g, len(groups))
	}
	return groups[g]
}

func (h *spacesHarness) rowOf(g, r int) spaces.WorktreeRow { return h.group(g).Rows[r] }

func (h *spacesHarness) selectedID() string {
	id, _ := h.list.selectedWorktree()
	return id
}

// label is the label of row (g, r) as it shows now.
func (h *spacesHarness) label(g, r int) string {
	row := h.rowOf(g, r)
	return WorktreeRowLabel(row, row.ID() == h.selectedID())
}

func (h *spacesHarness) has(label string) bool { _, ok := h.tt.Find(label); return ok }

func (h *spacesHarness) click(g, r int) {
	h.t.Helper()
	if err := h.tt.Click(h.label(g, r)); err != nil {
		h.t.Fatal(err)
	}
	h.tt.Frame()
}

// selected tells whether row (g, r) shows the card.
func (h *spacesHarness) selected(g, r int) bool {
	return strings.HasSuffix(h.label(g, r), ", selected") && h.has(h.label(g, r))
}

func (h *spacesHarness) pick(s state.Selected) SpacesChange {
	h.sel.Select(s)
	change := h.list.Sync()
	h.tt.Frame()
	return change
}

func (h *spacesHarness) builds() map[string]int {
	out := map[string]int{}
	for k, v := range h.list.builds {
		out[k] = v
	}
	return out
}

// heldWorktree shows the repo with wt1 held by a done agent in pane w1:p2 and
// wt2 unheld. Row 0 is wt1, row 1 is wt2.
func heldWorktree(t *testing.T) (*spacesHarness, string, string) {
	t.Helper()
	base := canonicalTmp(t)
	wt1, wt2 := twoWorktrees(t, base)
	return newSpaces(t, spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateDone))))), wt1, wt2
}

func worktreeSel(id string) state.Selected { return state.Selected{Kind: state.KindWorktree, ID: id} }

// ---- Ports of the Rust scan and sync tests ----

// Port of the_scan_fills_the_groups_once_the_tree_is_known.
func TestTheScanFillsTheGroupsOnceTheTreeIsKnown(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	if len(h.list.Groups()) != 1 || h.group(0).Name != "repo" {
		t.Fatalf("groups %+v", h.list.Groups())
	}
	// The main checkout is not held, so only the worktree has a row.
	if rows := h.group(0).Rows; len(rows) != 1 || rows[0].ID() != wt {
		t.Errorf("rows %+v", rows)
	}
	if h.list.scans != 1 {
		t.Errorf("scans %d, want 1", h.list.scans)
	}
}

// Port of a_herdr_change_with_the_same_cwds_rebuilds_without_a_scan.
func TestAHerdrChangeWithTheSameCwdsRebuildsWithoutAScan(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.send(spacesOf(spTree(spPane("w1:p1", wt, st(herdr.StateWorking)))))
	if h.list.scans != 1 {
		t.Errorf("scans %d: same cwds, last scan fresh, want no scan", h.list.scans)
	}
	if got := h.rowOf(0, 0).Status; got != icons.Working {
		t.Errorf("status %v, want working", got)
	}
}

// Port of a_new_cwd_starts_one_more_scan.
func TestANewCwdStartsOneMoreScan(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.send(spacesOf(spTree(spPane("w1:p1", wt, nil), spPane("w1:p2", filepath.Join(base, "repo"), nil))))
	if h.list.scans != 2 {
		t.Errorf("scans %d, want 2", h.list.scans)
	}
	// The held main checkout comes first, then the worktree.
	rows := h.group(0).Rows
	if len(rows) != 2 || !rows[0].IsMain || rows[1].ID() != wt {
		t.Errorf("rows %+v", rows)
	}
}

// Port of a_change_that_moves_no_row_does_not_notify_the_view.
func TestAChangeThatMovesNoRowDoesNotNotifyTheView(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	// Only the focus flag differs: herdr notices, the rows do not change.
	focused := spPane("w1:p1", wt, nil)
	focused.Focused = true
	if change := h.send(spacesOf(spTree(focused))); change.Any() {
		t.Errorf("change %+v, want none", change)
	}
}

// Port of losing_the_tree_empties_the_groups.
func TestLosingTheTreeEmptiesTheGroups(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	if len(h.list.Groups()) != 1 {
		t.Fatal("want one group")
	}
	h.send(notRunningLive())
	if len(h.list.Groups()) != 0 {
		t.Errorf("groups %+v", h.list.Groups())
	}
}

// Port of a_cwd_outside_git_gives_no_group.
func TestACwdOutsideGitGivesNoGroup(t *testing.T) {
	base := canonicalTmp(t)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", plain, nil))))
	if len(h.list.Groups()) != 0 {
		t.Errorf("groups %+v", h.list.Groups())
	}
	if h.list.scans != 1 {
		t.Errorf("scans %d: it was still scanned, and found nothing", h.list.scans)
	}
}

// Port of a_selected_worktree_that_stays_listed_stays_selected.
func TestASelectedWorktreeThatStaysListedStaysSelected(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.pick(worktreeSel(wt))
	h.send(spacesOf(spTree(spPane("w1:p1", wt, st(herdr.StateDone)))))
	if got, ok := h.sel.Current(); !ok || got != worktreeSel(wt) {
		t.Errorf("selection %+v, %v", got, ok)
	}
}

// Port of a_selected_worktree_that_is_pruned_is_cleared.
func TestASelectedWorktreeThatIsPrunedIsCleared(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	repo := filepath.Join(base, "repo")
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", repo, nil), spPane("w1:p2", wt, nil))))
	h.pick(worktreeSel(wt))
	// The worktree's checkout is gone, so git no longer lists it. A new cwd
	// forces a scan.
	if err := os.Remove(filepath.Join(wt, ".git")); err != nil {
		t.Fatal(err)
	}
	h.send(spacesOf(spTree(spPane("w1:p1", repo, nil), spPane("w1:p3", base, nil))))
	if rows := h.group(0).Rows; len(rows) != 1 || !rows[0].IsMain {
		t.Errorf("rows %+v, want only the main checkout", rows)
	}
	if got, ok := h.sel.Current(); ok {
		t.Errorf("selection %+v, want none", got)
	}
}

// Port of losing_the_tree_clears_a_worktree_selection.
func TestLosingTheTreeClearsAWorktreeSelection(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.pick(worktreeSel(wt))
	h.send(notRunningLive())
	if got, ok := h.sel.Current(); ok {
		t.Errorf("selection %+v, want none", got)
	}
}

// Port of an_agent_selection_is_left_alone.
func TestAnAgentSelectionIsLeftAlone(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	agent := state.Selected{Kind: state.KindAgent, ID: "w1:p1"}
	h.pick(agent)
	h.send(notRunningLive())
	if got, ok := h.sel.Current(); !ok || got != agent {
		t.Errorf("selection %+v, %v", got, ok)
	}
}

// ---- Rescans: Rust D15 replaces the timer with a check on herdr changes ----

// Port of the_timer_rescans_while_the_window_is_shown, without the timer (D15):
// a herdr change after the interval rescans.
func TestAHerdrChangeAfterTheIntervalRescansWhileShown(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := spacesOf(spTree(spPane("w1:p1", wt, nil)))
	h := newSpaces(t, live)
	h.advance(RescanAfter)
	h.send(live)
	if h.list.scans != 2 {
		t.Errorf("scans %d, want 2", h.list.scans)
	}
	h.advance(RescanAfter)
	h.send(live)
	if h.list.scans != 3 {
		t.Errorf("scans %d, want 3", h.list.scans)
	}
}

// Port of the_timer_waits_a_full_interval_after_a_fresh_scan.
func TestARescanWaitsAFullIntervalAfterAFreshScan(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := spacesOf(spTree(spPane("w1:p1", wt, nil)))
	h := newSpaces(t, live)
	h.advance(RescanAfter / 2)
	grown := spacesOf(spTree(spPane("w1:p1", wt, nil), spPane("w1:p2", filepath.Join(base, "repo"), nil)))
	h.send(grown)
	if h.list.scans != 2 {
		t.Fatalf("scans %d, want 2 (the new cwd, at 5 s)", h.list.scans)
	}
	h.advance(RescanAfter / 2)
	h.send(grown)
	if h.list.scans != 2 {
		t.Errorf("scans %d at 10 s: the scan at 5 s is fresh", h.list.scans)
	}
	h.advance(RescanAfter / 2)
	h.send(grown)
	if h.list.scans != 3 {
		t.Errorf("scans %d at 15 s, want a full interval after 5 s", h.list.scans)
	}
}

// Port of an_old_scan_is_not_refreshed_by_herdr_while_hidden, and of
// the_timer_is_off_while_the_window_is_hidden: nothing is scheduled.
func TestAnOldScanIsNotRefreshedByHerdrWhileHidden(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.hidden = true
	h.advance(RescanAfter * 6)
	h.send(spacesOf(spTree(spPane("w1:p1", wt, st(herdr.StateWorking)))))
	if h.list.scans != 1 {
		t.Errorf("scans %d: hidden, herdr starts no rescan", h.list.scans)
	}
	if got := h.rowOf(0, 0).Status; got != icons.Working {
		t.Errorf("status %v: the rows still follow herdr", got)
	}
	h.hidden = false
	h.send(spacesOf(spTree(spPane("w1:p1", wt, st(herdr.StateWorking)))))
	if h.list.scans != 2 {
		t.Errorf("scans %d: shown again, the next herdr change rescans", h.list.scans)
	}
}

// Port of a_branch_change_shows_after_the_next_timer_rescan.
func TestABranchChangeShowsAfterTheNextRescan(t *testing.T) {
	base := canonicalTmp(t)
	wt, admin := oneWorktree(t, base)
	live := spacesOf(spTree(spPane("w1:p1", wt, nil)))
	h := newSpaces(t, live)
	if got := h.rowOf(0, 0).Branch; got != "feature" {
		t.Fatalf("branch %q", got)
	}
	writeFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/renamed\n")
	h.send(live)
	if got := h.rowOf(0, 0).Branch; got != "feature" {
		t.Errorf("branch %q: the scan is fresh, so it is not read again", got)
	}
	h.advance(RescanAfter)
	h.send(live)
	if got := h.rowOf(0, 0).Branch; got != "renamed" {
		t.Errorf("branch %q, want renamed", got)
	}
}

// Port of an_old_scan_starts_one_on_a_herdr_change_while_shown.
func TestAnOldScanStartsOneOnAHerdrChangeWhileShown(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.advance(RescanAfter)
	h.send(spacesOf(spTree(spPane("w1:p1", wt, st(herdr.StateWorking)))))
	if h.list.scans != 2 {
		t.Errorf("scans %d: shown, the old scan starts one", h.list.scans)
	}
	if got := h.rowOf(0, 0).Status; got != icons.Working {
		t.Errorf("status %v", got)
	}
}

// Port of no_tree_starts_no_scan_however_long_the_timer_runs.
func TestNoTreeStartsNoScanHoweverLongItIs(t *testing.T) {
	h := newSpaces(t, herdr.LiveHerdr{State: connected()})
	h.advance(RescanAfter * 3)
	h.send(herdr.LiveHerdr{State: connected()})
	if h.list.scans != 0 || len(h.list.Groups()) != 0 {
		t.Errorf("scans %d, groups %+v", h.list.scans, h.list.Groups())
	}
}

// Port of a_pane_without_a_cwd_starts_no_scan.
func TestAPaneWithoutACwdStartsNoScan(t *testing.T) {
	live := spacesOf(spTree(spPane("w1:p1", "", nil)))
	h := newSpaces(t, live)
	h.advance(RescanAfter * 2)
	h.send(live)
	if h.list.scans != 0 {
		t.Errorf("scans %d, want 0", h.list.scans)
	}
}

// Port of losing_the_tree_stops_the_timer_scans.
func TestLosingTheTreeStopsTheScans(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p1", wt, nil))))
	h.send(notRunningLive())
	h.advance(RescanAfter * 3)
	h.send(notRunningLive())
	if h.list.scans != 1 {
		t.Errorf("scans %d: the lost tree drops its scan, and no later scan starts", h.list.scans)
	}
}

// Port of a_connection_change_alone_repaints_the_sidebar.
func TestAConnectionChangeAloneRepaintsTheSidebar(t *testing.T) {
	h := newSpaces(t, herdr.LiveHerdr{State: connected()})
	if !h.list.online {
		t.Fatal("want online")
	}
	change := h.send(notRunningLive())
	if h.list.online || !change.Online || !change.Any() {
		t.Errorf("online %v, change %+v: the dot changed, so the column repaints", h.list.online, change)
	}
}

// ---- Not in Rust: the scan goroutine ----

func TestAScanLandsThroughThePostFunc(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &fakeLive{live: spacesOf(spTree(spPane("w1:p1", wt, nil)))}
	var started, posted []func()
	l := NewSpacesList(live, nil, nil, state.NewSelection(nil), nil, nil, func(f func()) { posted = append(posted, f) })
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	if len(l.Groups()) != 0 || len(started) != 1 {
		t.Fatalf("groups %+v, started %d: the scan has not run", l.Groups(), len(started))
	}
	started[0]()
	if len(l.Groups()) != 0 || len(posted) != 1 {
		t.Fatalf("groups %+v, posted %d: the result waits for the UI thread", l.Groups(), len(posted))
	}
	posted[0]()
	if len(l.Groups()) != 1 {
		t.Errorf("groups %+v, want one", l.Groups())
	}
}

func TestAScanThatLostTheTreeIsDropped(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &fakeLive{live: spacesOf(spTree(spPane("w1:p1", wt, nil)))}
	var started []func()
	l := NewSpacesList(live, nil, nil, state.NewSelection(nil), nil, nil, nil)
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	live.setLive(notRunningLive())
	l.Sync()
	started[0]() // the old scan ends now; post is nil, so it lands at once
	live.setLive(spacesOf(spTree(spPane("w1:p1", wt, nil))))
	if got := len(l.Groups()); got != 0 {
		t.Errorf("groups %d: a dropped scan came back", got)
	}
}

func TestOnlyTheNewestScanLands(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	repo := filepath.Join(base, "repo")
	live := &fakeLive{live: spacesOf(spTree(spPane("w1:p1", wt, nil)))}
	var started []func()
	l := NewSpacesList(live, nil, nil, state.NewSelection(nil), nil, nil, nil)
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	live.setLive(spacesOf(spTree(spPane("w1:p1", wt, nil), spPane("w1:p2", repo, nil))))
	l.Sync()
	started[1]()
	started[0]() // the older scan ends last and is dropped
	if rows := l.Groups()[0].Rows; len(rows) != 2 {
		t.Errorf("rows %+v, want the newest scan's two rows", rows)
	}
}

func TestTheScanFuncGetsTheCwdsInTreeOrder(t *testing.T) {
	live := &fakeLive{live: spacesOf(spTree(spPane("w1:p1", "/x/b", nil), spPane("w1:p2", "/x/a", nil), spPane("w1:p3", "/x/b", nil)))}
	var got []string
	l := NewSpacesList(live, nil, nil, nil, nil, nil, nil)
	l.scanFn = func(cwds []string) git.Scan { got = cwds; return git.Scan{} }
	l.start = func(f func()) { f() }
	l.Sync()
	if want := []string{"/x/b", "/x/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cwds %q, want %q", got, want)
	}
}

// ---- Ports of the Rust view tests ----

// Port of the_dot_and_the_name_colours_follow_the_connection_and_the_hold.
func TestTheDotAndTheNameColoursFollowTheConnectionAndTheHold(t *testing.T) {
	if onlineColour(true) != theme.Text2 || onlineColour(false) != theme.IconFaint {
		t.Error("the dot colours are wrong")
	}
	if nameColour(true) != theme.Text || nameColour(false) != theme.TextDim {
		t.Error("the name colours are wrong")
	}
}

// Port of the_rows_show_the_repo_its_worktrees_and_the_host_card.
func TestTheRowsShowTheRepoItsWorktreesAndTheHostCard(t *testing.T) {
	h, _, _ := heldWorktree(t)
	repo := box(t, h.tt, RepoLabel(h.group(0)))
	if repo.Y < 0 {
		t.Errorf("repo line at %v", repo)
	}
	held := box(t, h.tt, h.label(0, 0))
	if held.H != theme.WorktreeRowHeight || held.W != theme.SidebarWidth-2*theme.CardInsetX {
		t.Errorf("held row %v, want %d high", held, theme.WorktreeRowHeight)
	}
	if !h.tt.HasText("a done") {
		t.Errorf("held: a hold line; texts %q", h.tt.Texts())
	}
	if !h.has(h.label(0, 1)) {
		t.Error("an unheld linked worktree should have a row")
	}
	if strings.Contains(h.label(0, 1), ", a ") {
		t.Errorf("unheld: no hold line, label %q", h.label(0, 1))
	}
	if h.selected(0, 0) {
		t.Error("nothing is selected")
	}
	for _, label := range []string{LabelSpacesNew, LabelHostLocal, LabelHostOnline, LabelSpacesHeader} {
		if !h.has(label) {
			t.Errorf("%q is missing; texts %q", label, h.tt.Texts())
		}
	}
	if !h.tt.HasText("repo") || !h.tt.HasText("main") {
		t.Errorf("the repo line shows its name and branch; texts %q", h.tt.Texts())
	}
}

// Port of the_cpu_text_rounds_or_shows_a_dash.
func TestTheCPUTextRoundsOrShowsADash(t *testing.T) {
	for _, tc := range []struct {
		cpu  float32
		want string
	}{{38.4, "CPU 38%"}, {37.5, "CPU 38%"}} {
		if got := CPUText(readingHost(stats(tc.cpu, 0, 1))); got != tc.want {
			t.Errorf("cpu %v: %q, want %q", tc.cpu, got, tc.want)
		}
	}
	if got := CPUText(&fakeHost{}); got != "CPU –" {
		t.Errorf("no reading: %q", got)
	}
	if got := CPUText(nil); got != "CPU –" {
		t.Errorf("no host: %q", got)
	}
}

// Port of the_host_card_fills_the_memory_bar_to_the_share.
func TestTheHostCardFillsTheMemoryBarToTheShare(t *testing.T) {
	h := newSpaces(t, herdr.LiveHerdr{State: connected()})
	h.host.set(stats(38, 50, 100))
	h.tt.Frame()
	card := box(t, h.tt, LabelHostLocal)
	bar := box(t, h.tt, LabelCardMem)
	if bar.W != theme.MemBarWidth || bar.H != theme.MemBarHeight {
		t.Errorf("bar %v", bar)
	}
	if bar.X+bar.W > card.X+card.W {
		t.Errorf("the bar %v sits outside the card %v", bar, card)
	}
	if fill := box(t, h.tt, LabelCardMemFill); fill.W != 30 {
		t.Errorf("fill %v, want 30 wide", fill)
	}
	if !h.has(LabelCardCPU) || !h.tt.HasText("CPU 38%") {
		t.Errorf("the CPU is missing; texts %q", h.tt.Texts())
	}
}

// Port of no_reading_leaves_the_bar_empty.
func TestNoReadingLeavesTheBarEmpty(t *testing.T) {
	h := newSpaces(t, herdr.LiveHerdr{State: connected()})
	if !h.has(LabelCardCPU) || !h.tt.HasText("CPU –") {
		t.Errorf("the CPU dash is missing; texts %q", h.tt.Texts())
	}
	if fill, ok := h.tt.Find(LabelCardMemFill); ok && fill.W != 0 {
		t.Errorf("fill %v, want empty", fill)
	}
}

// Port of a_new_reading_repaints_the_host_card.
func TestANewReadingRepaintsTheHostCard(t *testing.T) {
	h := newSpaces(t, herdr.LiveHerdr{State: connected()})
	h.host.set(stats(10, 75, 100))
	h.tt.Frame()
	if fill := box(t, h.tt, LabelCardMemFill); fill.W != 45 {
		t.Errorf("fill %v, want 45 wide", fill)
	}
}

// Port of a_click_selects_the_row_and_focuses_its_holder.
func TestAClickSelectsTheRowAndFocusesItsHolder(t *testing.T) {
	h, wt1, _ := heldWorktree(t)
	h.click(0, 0)
	if !h.selected(0, 0) {
		t.Error("the card is missing")
	}
	if got, ok := h.sel.Current(); !ok || got != worktreeSel(wt1) {
		t.Errorf("selection %+v, %v", got, ok)
	}
	if !reflect.DeepEqual(h.focused, []string{"w1:p2"}) {
		t.Errorf("focused %q", h.focused)
	}
	h.click(0, 0)
	if !reflect.DeepEqual(h.focused, []string{"w1:p2", "w1:p2"}) {
		t.Errorf("focused %q: a second click focuses again", h.focused)
	}
}

// Port of a_click_on_an_unheld_row_moves_the_card_without_a_focus_call.
func TestAClickOnAnUnheldRowMovesTheCardWithoutAFocusCall(t *testing.T) {
	h, _, _ := heldWorktree(t)
	h.click(0, 0)
	h.click(0, 1)
	if !h.selected(0, 1) || h.selected(0, 0) {
		t.Errorf("card on the wrong rows; texts %q", h.tt.Texts())
	}
	if !reflect.DeepEqual(h.focused, []string{"w1:p2"}) {
		t.Errorf("focused %q: only the held row focuses", h.focused)
	}
}

// Port of an_agent_selection_hides_the_worktree_card.
func TestAnAgentSelectionHidesTheWorktreeCard(t *testing.T) {
	h, _, _ := heldWorktree(t)
	h.click(0, 0)
	if !h.selected(0, 0) {
		t.Fatal("the card is missing")
	}
	h.pick(state.Selected{Kind: state.KindAgent, ID: "w1:p2"})
	if h.selected(0, 0) || strings.Contains(h.label(0, 0), "selected") {
		t.Error("an agent selection should clear the worktree card")
	}
}

// Port of a_badge_in_the_model_shows_on_its_row.
func TestABadgeInTheModelShowsOnItsRow(t *testing.T) {
	h, _, _ := heldWorktree(t)
	if h.has(LabelBadge) {
		t.Fatal("no badge until the bridge sends one")
	}
	l := h.list
	l.groups[0].Rows[0].Badge = &spaces.Badge{Kind: spaces.BadgePR, PR: 640}
	l.groups[0].Rows[1].Badge = &spaces.Badge{Kind: spaces.BadgeYou, You: "T12"}
	h.tt.Frame()
	if !h.tt.HasText("#640") || !h.tt.HasText("T12") {
		t.Errorf("badges are missing; texts %q", h.tt.Texts())
	}
}

// Port of a_spinning_worktree_row_redraws_only_itself. The working icon draws
// its own turn: frames of the turn build no row.
func TestASpinningWorktreeRowRedrawsOnlyItself(t *testing.T) {
	base := canonicalTmp(t)
	wt1, _ := twoWorktrees(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateWorking)))))
	before := h.builds()
	for i := 0; i < 3; i++ {
		h.tt.Advance(100 * time.Millisecond)
	}
	if !reflect.DeepEqual(h.builds(), before) {
		t.Errorf("builds %v -> %v: frames of the turn built rows", before, h.builds())
	}
	if !h.tt.FramePending() {
		t.Error("the working icon should ask for frames")
	}
	idle, _, _ := heldWorktree(t)
	idle.tt.Advance(100 * time.Millisecond)
	if idle.tt.FramePending() {
		t.Error("a column with no working row asks for frames")
	}
}

// Port of a_change_to_one_row_redraws_only_that_row. MyGo builds the whole
// column for a redraw, so what is checked is what Sync reports.
func TestAChangeToOneWorktreeRowRedrawsOnlyThatRow(t *testing.T) {
	base := canonicalTmp(t)
	wt1, _ := twoWorktrees(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateIdle)))))
	change := h.send(spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateBlocked)))))
	if change.Rebuilt || !reflect.DeepEqual(change.Rows, []RowRef{{0, 0}}) {
		t.Errorf("change %+v, want the held row only", change)
	}
	if !h.has("blocked") {
		t.Error("the blocked icon is missing")
	}
	if again := h.send(spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateBlocked))))); again.Any() {
		t.Errorf("the same tree changed %+v", again)
	}
}

// Port of a_selection_redraws_only_the_rows_whose_mark_changed.
func TestASelectionRedrawsOnlyTheRowsWhoseMarkChanged(t *testing.T) {
	h, wt1, wt2 := heldWorktree(t)
	if change := h.pick(worktreeSel(wt1)); change.Rebuilt || !reflect.DeepEqual(change.Rows, []RowRef{{0, 0}}) {
		t.Errorf("first change %+v, want the row that gained the mark", change)
	}
	if change := h.pick(worktreeSel(wt2)); change.Rebuilt || !reflect.DeepEqual(change.Rows, []RowRef{{0, 0}, {0, 1}}) {
		t.Errorf("second change %+v, want both rows", change)
	}
	if change := h.pick(worktreeSel(wt2)); change.Any() {
		t.Errorf("the same selection changed %+v", change)
	}
}

// Port of a_selected_row_keeps_its_card_when_the_rows_are_rebuilt.
func TestASelectedRowKeepsItsCardWhenTheRowsAreRebuilt(t *testing.T) {
	base := canonicalTmp(t)
	wt1, wt2 := twoWorktrees(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateDone)))))
	h.click(0, 0)
	if !h.selected(0, 0) {
		t.Fatal("the card is missing")
	}
	// The main checkout gains a pane: its group gets a row, so the rows are
	// made anew. Git lists the main checkout first.
	repo := filepath.Join(base, "repo")
	h.send(spacesOf(spTree(spPane("w1:p1", repo, nil), spPane("w1:p2", wt1, st(herdr.StateDone)))))
	var order []string
	for _, r := range h.group(0).Rows {
		order = append(order, r.Root)
	}
	if want := []string{repo, wt1, wt2}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order %q, want %q", order, want)
	}
	if !h.selected(0, 1) || h.selected(0, 2) {
		t.Error("the card moved with the rows")
	}
	// The new rows still take clicks.
	h.click(0, 2)
	if got, _ := h.sel.Current(); got != worktreeSel(wt2) {
		t.Errorf("selection %+v", got)
	}
	if !h.selected(0, 2) || h.selected(0, 1) {
		t.Error("the card did not follow the click")
	}
}

// ---- Board ----

func rowJSON(who string) string {
	return `[{"key": "k", "label": "#640", "pane_id": "w1:p2", "who": ` + who + `}]`
}

func (h *spacesHarness) putBoard(rows string) SpacesChange {
	h.t.Helper()
	putBoard(h.t, h.board, boardJSON("[]", rows))
	change := h.list.Sync()
	h.tt.Frame()
	return change
}

// Port of the_board_sets_the_second_line_and_badge.
func TestTheBoardSetsTheSecondLineAndBadge(t *testing.T) {
	h, _, _ := heldWorktree(t)
	h.putBoard(rowJSON(`{"text": "b-1151 5m", "kind": "text"}`))
	row := h.rowOf(0, 0)
	if row.Hold == nil || *row.Hold != "b-1151 5m" {
		t.Errorf("hold %v", row.Hold)
	}
	if row.Badge == nil || *row.Badge != (spaces.Badge{Kind: spaces.BadgePR, PR: 640}) {
		t.Errorf("badge %+v", row.Badge)
	}
	if !h.tt.HasText("#640") || !h.tt.HasText("b-1151 5m") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if h.has(LabelHoldYou) {
		t.Error("a text who is not the you style")
	}
}

// Port of a_you_line_draws_the_you_chip.
func TestAYouLineDrawsTheYouChip(t *testing.T) {
	h, _, _ := heldWorktree(t)
	h.putBoard(rowJSON(`{"text": "your test", "kind": "you"}`))
	if !h.tt.HasText("your test") || !h.has(LabelHoldYou) {
		t.Errorf("the you chip is missing; texts %q", h.tt.Texts())
	}
}

// Port of without_a_board_the_row_keeps_the_herdr_fallback.
func TestWithoutABoardTheRowKeepsTheHerdrFallback(t *testing.T) {
	h, _, _ := heldWorktree(t)
	row := h.rowOf(0, 0)
	if row.Hold == nil || *row.Hold != "a done" || row.Badge != nil {
		t.Errorf("row %+v", row)
	}
	if h.has(LabelBadge) || h.has(LabelHoldYou) {
		t.Error("a badge or you chip shows without a board")
	}
	bare := h.makeList(nil)
	if r := bare.Groups()[0].Rows[0]; r.Hold == nil || *r.Hold != "a done" {
		t.Errorf("a list with no board shows %+v", r)
	}
}

// Port of a_board_that_drops_the_pane_restores_the_fallback.
func TestABoardThatDropsThePaneRestoresTheFallback(t *testing.T) {
	h, _, _ := heldWorktree(t)
	h.putBoard(rowJSON(`{"text": "your test", "kind": "you"}`))
	if !h.has(LabelHoldYou) {
		t.Fatal("the you chip is missing")
	}
	h.putBoard("[]")
	row := h.rowOf(0, 0)
	if row.Hold == nil || *row.Hold != "a done" || row.Badge != nil {
		t.Errorf("row %+v", row)
	}
	if h.has(LabelHoldYou) {
		t.Error("the you chip stayed")
	}
}

// Port of a_board_change_redraws_only_the_row_it_changes.
func TestABoardChangeRedrawsOnlyTheRowItChanges(t *testing.T) {
	base := canonicalTmp(t)
	wt1, wt2 := twoWorktrees(t, base)
	h := newSpaces(t, spacesOf(spTree(spPane("w1:p2", wt1, st(herdr.StateDone)), spPane("w1:p3", wt2, st(herdr.StateDone)))))
	change := h.putBoard(rowJSON(`{"text": "b-1151 5m", "kind": "text"}`))
	if change.Rebuilt || !reflect.DeepEqual(change.Rows, []RowRef{{0, 0}}) {
		t.Errorf("change %+v, want the marked row only", change)
	}
}

// ---- Not in Rust: labels, activation and the wiring ----

func TestWorktreeRowLabelHasItsParts(t *testing.T) {
	hold := "a done"
	row := spaces.WorktreeRow{Name: "wt1", Status: icons.Done, Hold: &hold, Badge: &spaces.Badge{Kind: spaces.BadgePR, PR: 640}}
	if got := WorktreeRowLabel(row, false); got != "wt1, done, a done, #640" {
		t.Errorf("label %q", got)
	}
	if got := WorktreeRowLabel(row, true); got != "wt1, done, a done, #640, selected" {
		t.Errorf("label %q", got)
	}
	if got := WorktreeRowLabel(spaces.WorktreeRow{Name: "wt2", Status: icons.Idle}, false); got != "wt2, idle" {
		t.Errorf("label %q", got)
	}
}

func TestBadgeTextIsThePRNumberOrTheOwnersValue(t *testing.T) {
	if got := BadgeText(spaces.Badge{Kind: spaces.BadgePR, PR: 0}); got != "#0" {
		t.Errorf("text %q", got)
	}
	if got := BadgeText(spaces.Badge{Kind: spaces.BadgeYou, You: "blocked"}); got != "blocked" {
		t.Errorf("text %q", got)
	}
}

func TestActivateGivesTheWorktreeFocusTheStatusMessage(t *testing.T) {
	status := state.NewStatusMessage(nil)
	l := NewSpacesList(nil, nil, nil, state.NewSelection(nil), status, nil, nil)
	l.focus = func(_ string, report func(string)) { report("herdr is not running") }
	pane := "p1"
	l.Activate(spaces.WorktreeRow{Root: "/x", FocusPane: &pane})
	if text, ok := status.Text(); !ok || text != "herdr is not running" {
		t.Errorf("status %q, %v", text, ok)
	}
}

func TestAnUnheldRowIsSelectedAndFocusesNothing(t *testing.T) {
	sel := state.NewSelection(nil)
	l := NewSpacesList(nil, nil, nil, sel, nil, nil, nil)
	l.focus = func(id string, _ func(string)) { t.Errorf("focused %q", id) }
	l.Activate(spaces.WorktreeRow{Root: "/x"})
	if got, ok := sel.Current(); !ok || got != worktreeSel("/x") {
		t.Errorf("selection %+v, %v", got, ok)
	}
}

func TestAnEmptyColumnStillShowsTheHostCard(t *testing.T) {
	h := newSpaces(t, notRunningLive())
	if !h.has(LabelHostLocal) || !h.has(LabelSpacesHeader) {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if len(h.list.Groups()) != 0 || h.list.online {
		t.Errorf("groups %+v, online %v", h.list.Groups(), h.list.online)
	}
}

func TestAttachPutsTheSpacesColumnInTheSidebar(t *testing.T) {
	w := &queueWindow{fakeWindow: fakeWindow{visible: true}}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	stop := f.Attach(t.Context(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	if f.SpacesList == nil || f.Slots.Spaces == nil {
		t.Fatal("Attach should make the spaces list")
	}
	f.syncHerdr()
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	if _, ok := tt.Find(LabelHostLocal); !ok {
		t.Errorf("the host card is missing from the sidebar; texts %q", tt.Texts())
	}
}
