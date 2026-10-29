package app

// The test rig of the board panel, the Go form of the Rust test_support and
// recording_panel: a panel in a column as wide as the real one, in a window of
// the reference size, with Focus, Open and OpenPath recorded.

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// setForTest sets what the board shows, as a read does (the Rust board.set).
func (b *Board) setForTest(snap *board.Snapshot, problem string, bridge *board.BridgePane) {
	b.mu.Lock()
	b.snap, b.problem, b.bridge = snap, problem, bridge
	b.mu.Unlock()
}

// testBridge is the bridge pane of the tests: pane w1:p1, session s1, rev 1.
func testBridge() *board.BridgePane {
	return &board.BridgePane{PaneID: "w1:p1", Bridge: board.BridgeRef{Session: "s1", Rev: 1}}
}

// panelFixture is the board fixture of the board package.
func panelFixture(t *testing.T) *board.Snapshot { return cardFixture(t) }

type panelRig struct {
	t     *testing.T
	p     *BoardPanel
	b     *Board
	tt    *ui.Tester
	out   *BoardOutbox // nil unless the rig is queued
	clock *fakeTimers
	dir   string
	tree  *herdr.HerdrTree

	mu      sync.Mutex
	focused []string
	opened  []string
	paths   []string
	lines   []string
}

type panelOpt func(*panelRig)

// withTree gives the panel a herdr tree, so the Focus labels resolve.
func withTree(tree *herdr.HerdrTree) panelOpt { return func(r *panelRig) { r.tree = tree } }

// newPanelRig is a panel over a board that shows snap, problem and bridge,
// set by the test and not read from a file. It has no queue.
func newPanelRig(t *testing.T, snap *board.Snapshot, problem string, bridge *board.BridgePane, opts ...panelOpt) *panelRig {
	t.Helper()
	r := &panelRig{t: t, b: NewDetachedBoard(), clock: &fakeTimers{}}
	r.b.setForTest(snap, problem, bridge)
	for _, o := range opts {
		o(r)
	}
	r.start()
	return r
}

// newQueuedPanelRig is a panel over a board that reads and writes its files in
// a temp dir and shows snap on the bridge pane. The queue is set up as the
// frame does it, so the panel can send.
func newQueuedPanelRig(t *testing.T, snap *board.Snapshot, opts ...panelOpt) *panelRig {
	t.Helper()
	r := &panelRig{t: t, b: NewBoard(t.TempDir(), true, nil), clock: &fakeTimers{}}
	r.dir, _ = r.b.Dir()
	if snap.Actions == 0 {
		snap.Actions = 1 // the fixture's mod takes no actions; the queued rig's does
	}
	r.b.setForTest(snap, "", testBridge())
	for _, o := range opts {
		o(r)
	}
	r.out = NewBoardOutbox(r.dir, true, nil, r.record)
	r.out.now, r.out.after = r.clock.Now, r.clock.After
	t.Cleanup(r.out.Wait)
	r.out.Sync(snap)
	r.out.Wait()
	r.start()
	return r
}

func (r *panelRig) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, s)
}

func (r *panelRig) start() {
	r.p = NewBoardPanel(PanelDeps{
		Board:  r.b,
		Outbox: r.out,
		Tree:   func() *herdr.HerdrTree { return r.tree },
		Status: r.record,
		Focus: func(pane string, _ func(string)) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.focused = append(r.focused, pane)
		},
		Open: func(_ *ui.Context, url string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.opened = append(r.opened, url)
		},
		OpenPath: func(_ *ui.Context, path string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.paths = append(r.paths, path)
		},
	})
	r.t.Cleanup(r.p.report.wg.Wait)
	r.tt = ui.NewTester(r.host, theme.WindowWidth, theme.WindowHeight)
	r.tt.Frame()
}

// host holds the panel in a column of the frame's width, as the frame does, so
// the panel's bounds are the real ones in a window of the reference size.
func (r *panelRig) host(c *ui.Context) {
	ui.Row(c).Fill().Children(func() {
		ui.Box(c).Label("panel host").Width(theme.PanelWidth).FillHeight().Shrink(0).Children(func() {
			r.p.View(c)
		})
	})
}

// settle waits for the outbox and the report reads, then draws.
func (r *panelRig) settle() {
	if r.out != nil {
		r.out.Wait()
	}
	r.p.report.wg.Wait()
	r.tt.Frame()
}

// click clicks the element with label and lets what it starts settle.
func (r *panelRig) click(label string) {
	r.t.Helper()
	if err := r.tt.Click(label); err != nil {
		r.t.Fatalf("click %q: %v", label, err)
	}
	r.settle()
}

// set shows snap as a board read does, with the bridge, as the frame's
// syncBoard does: the outbox first, then the panel.
func (r *panelRig) set(snap *board.Snapshot) {
	r.t.Helper()
	r.b.setForTest(snap, "", testBridge())
	if r.out != nil {
		r.out.Sync(snap)
		r.out.Wait()
	}
	r.p.Sync()
	r.settle()
}

// has tells whether the last frame has an element with label or text.
func (r *panelRig) has(label string) bool {
	_, ok := r.tt.Find(label)
	return ok
}

// rect returns the box of the element with label.
func (r *panelRig) rect(label string) ui.Rect {
	r.t.Helper()
	rc, ok := r.tt.Find(label)
	if !ok {
		r.t.Fatalf("missing %q in %v", label, r.tt.Texts())
	}
	return rc
}

// status returns the status lines shown so far.
func (r *panelRig) status() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

// queued returns the actions written to the actions file of the queue session
// so far. ok is false when there is no file yet.
func (r *panelRig) queued() ([]board.Envelope, bool) {
	r.t.Helper()
	r.out.mu.Lock()
	session := ""
	if r.out.queue != nil {
		session = r.out.queue.Session()
	}
	r.out.mu.Unlock()
	data, err := os.ReadFile(board.ActionsPath(r.dir, session))
	if err != nil {
		return nil, false
	}
	f, ok := board.ParseActions(data)
	return f.Actions, ok
}

// reads is the number of reads that the panel's report view started.
func (r *panelRig) reads() int {
	r.p.report.mu.Lock()
	defer r.p.report.mu.Unlock()
	return r.p.report.reads
}

// showReport calls ShowReport, as a click on a work row does.
func (r *panelRig) showReport(key string) {
	r.p.ShowReport(key)
	r.settle()
}

// reportFile copies testdata/report_fixture.md into a new temp dir and returns
// the absolute path of the copy.
func reportFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "report_fixture.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "RESULT.md")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// snapshotWithReport is the fixture with row 0's report set to raw. The shared
// fixture keeps its own path.
func snapshotWithReport(t *testing.T, raw string) *board.Snapshot {
	t.Helper()
	snap := panelFixture(t)
	snap.Rows[0].Report = &raw
	return snap
}
