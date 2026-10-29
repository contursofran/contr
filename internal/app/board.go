package app

// The coordinator's board as the UI holds it. The bridge pane is the herdr
// pane whose pane token `contr_board` names the session and the board
// revision. The board file of that session is read on a goroutine, and only
// when the session or the revision changes. Nothing here polls or sets a
// timer: [Board.Sync] runs when [Herdr] announces a change, and Herdr stops
// announcing while the window is hidden. Ported from the Rust contr module
// board.

import (
	"reflect"
	"sync"

	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// Board is the coordinator's board for the bridge pane in the herdr tree.
// It is safe to use from several goroutines.
type Board struct {
	// dir is where board files are read from; empty when there is none.
	dir      string
	hasDir   bool
	onChange func()

	mu     sync.Mutex
	bridge *board.BridgePane
	snap   *board.Snapshot
	// problem is why the shown board is not the file as it is now.
	problem string
	// gen counts the reads started. A finished read of an older one is
	// dropped, as a newer read drops the older one.
	gen   uint64
	reads int
	wg    sync.WaitGroup
}

// BoardView is what the views read, taken in one lock.
type BoardView struct {
	// Bridge is nil when herdr is down or no pane carries a valid token.
	Bridge *board.BridgePane
	// Snapshot is the last board read that belongs to the bridge's session.
	// Shared: do not change it.
	Snapshot *board.Snapshot
	// Problem is a line for the panel; empty when there is none.
	Problem string
}

// NewBoard returns a board that reads files from the directory given, from
// [board.Dir] for the real app. hasDir false means there is no board
// directory. onChange runs when what the views read changed. It can run on
// a goroutine or on the UI thread, so it should only queue work with
// Window.Update or ask for a redraw. Call
// [Board.Sync] with the current tree to start.
func NewBoard(dir string, hasDir bool, onChange func()) *Board {
	return &Board{dir: dir, hasDir: hasDir, onChange: onChange}
}

// NewDetachedBoard returns a board that reads no files, for tests that build
// a frame or a panel (the Rust init_detached).
func NewDetachedBoard() *Board { return NewBoard("", false, nil) }

// Dir returns the board directory that board files are read from; false when
// there is none.
func (b *Board) Dir() (string, bool) { return b.dir, b.hasDir }

// View returns what the views read.
func (b *Board) View() BoardView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.viewLocked()
}

func (b *Board) viewLocked() BoardView {
	v := BoardView{Snapshot: b.snap, Problem: b.problem}
	if b.bridge != nil {
		p := *b.bridge
		v.Bridge = &p
	}
	return v
}

// SyncHerdr follows the tree of h. Call it from Herdr's onChange.
func (b *Board) SyncHerdr(h *Herdr) { b.Sync(h.Live().Tree) }

// Sync follows the bridge in tree (nil when herdr has none). A new session
// or revision starts a read; the same one does not.
func (b *Board) Sync(tree *herdr.HerdrTree) {
	b.mu.Lock()
	before := b.viewLocked()
	if next, ok := board.FindBridge(tree); !ok {
		b.bridge = nil
		b.gen++ // drop a read in flight
		b.snap = nil
		b.problem = ""
	} else {
		same := b.bridge != nil && b.bridge.Bridge == next.Bridge
		if !same {
			b.startReadLocked(next.Bridge.Session)
		}
		b.bridge = &next
	}
	changed := !sameView(before, b.viewLocked())
	b.mu.Unlock()
	if changed {
		b.notify()
	}
}

// startReadLocked starts reading the board of session on a goroutine,
// dropping any read in flight. The snapshot stays only while it belongs to
// session.
func (b *Board) startReadLocked(session string) {
	b.keepSnapshotOfLocked(session)
	b.problem = ""
	b.gen++
	if !b.hasDir {
		b.problem = board.NoBoardDir
		return
	}
	b.reads++
	gen, path := b.gen, board.Path(b.dir, session)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		snap, line := board.Read(path)
		b.loaded(gen, session, snap, line)
	}()
}

// loaded applies a finished read of session's board. A board of another
// session, or a read that failed, sets the problem and keeps the snapshot
// only if it belongs to session.
func (b *Board) loaded(gen uint64, session string, snap *board.Snapshot, line string) {
	b.mu.Lock()
	if gen != b.gen {
		b.mu.Unlock()
		return
	}
	before := b.viewLocked()
	switch {
	case line != "":
		b.keepSnapshotOfLocked(session)
		b.problem = line
	case snap.Session == session:
		b.snap = snap
		b.problem = ""
	default:
		b.keepSnapshotOfLocked(session)
		b.problem = board.OtherSession
	}
	changed := !sameView(before, b.viewLocked())
	b.mu.Unlock()
	if changed {
		b.notify()
	}
}

// keepSnapshotOfLocked drops the snapshot unless it belongs to session.
func (b *Board) keepSnapshotOfLocked(session string) {
	if b.snap == nil || b.snap.Session != session {
		b.snap = nil
	}
}

func (b *Board) notify() {
	if b.onChange != nil {
		b.onChange()
	}
}

func sameView(a, c BoardView) bool {
	if a.Problem != c.Problem {
		return false
	}
	if (a.Bridge == nil) != (c.Bridge == nil) || (a.Bridge != nil && *a.Bridge != *c.Bridge) {
		return false
	}
	return a.Snapshot == c.Snapshot || reflect.DeepEqual(a.Snapshot, c.Snapshot)
}
