package app

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

const boardSession = "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f"

//go:embed testdata/board_fixture_v1.json
var boardFixture []byte

func boardPane(id string, token *string) herdr.Pane {
	p := herdr.Pane{ID: id, Tokens: map[string]string{}}
	if token != nil {
		p.Tokens[board.BridgeToken] = *token
	}
	return p
}

func boardWorkspace(id string, focused bool, panes ...herdr.Pane) herdr.Workspace {
	return herdr.Workspace{ID: id, Label: id, Focused: focused, Tabs: []herdr.Tab{{ID: id + ":t1", Label: "t1", Panes: panes}}}
}

func boardTree(ws ...herdr.Workspace) *herdr.HerdrTree {
	return &herdr.HerdrTree{Version: "0.9.3", Workspaces: ws}
}

func boardToken(rev int) *string {
	s := fmt.Sprintf("%d %s %d", board.Version, boardSession, rev)
	return &s
}

func strp(s string) *string { return &s }

// coordinator is one focused workspace with one pane w1:p1 that carries token.
func coordinator(token *string) *herdr.HerdrTree {
	return boardTree(boardWorkspace("w1", true, boardPane("w1:p1", token)))
}

func boardDirWith(t *testing.T, text []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(board.Path(dir, boardSession), text, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newTestBoard returns a board on dir that counts its notifications.
func newTestBoard(dir string, hasDir bool) (*Board, *atomic.Int32) {
	var n atomic.Int32
	return NewBoard(dir, hasDir, func() { n.Add(1) }), &n
}

// push syncs the board with tree and waits for the reads to finish.
func push(b *Board, tree *herdr.HerdrTree) {
	b.Sync(tree)
	b.wg.Wait()
}

func (b *Board) readCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reads
}

func rowCount(v BoardView) int {
	if v.Snapshot == nil {
		return -1
	}
	return len(v.Snapshot.Rows)
}

func TestATokenReadsTheBoardOnceAndShowsItsRows(t *testing.T) {
	b, _ := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	v := b.View()
	if rowCount(v) != 3 || v.Problem != "" || b.readCount() != 1 || v.Bridge == nil || v.Bridge.PaneID != "w1:p1" {
		t.Fatalf("view = %+v, reads = %d", v, b.readCount())
	}
}

func boardDir(t *testing.T) string { return boardDirWith(t, boardFixture) }

func TestDirIsTheDirectoryTheBoardWasGiven(t *testing.T) {
	dir := t.TempDir()
	b, _ := newTestBoard(dir, true)
	if got, ok := b.Dir(); !ok || got != dir {
		t.Fatalf("Dir = %q, %v", got, ok)
	}
}

func TestDirIsNoneWithoutABoardDirectory(t *testing.T) {
	b, _ := newTestBoard("", false)
	if _, ok := b.Dir(); ok {
		t.Fatal("Dir should be none")
	}
}

func TestNothingIsReadWhileTheWindowIsHidden(t *testing.T) {
	dir := boardDir(t)
	b, _ := newTestBoard(dir, true)
	h, _ := detached(false)
	// The app wires Herdr's onChange to the board, as here.
	h.onChange = func() { b.SyncHerdr(h) }
	h.apply(herdr.Update{Tree: coordinator(boardToken(42))})
	b.wg.Wait()
	if b.readCount() != 0 {
		t.Fatalf("hidden changes were read: %d", b.readCount())
	}
	h.SetVisible(true)
	b.wg.Wait()
	if b.readCount() != 1 || rowCount(b.View()) != 3 {
		t.Fatalf("reads = %d, rows = %d; showing the window should read once", b.readCount(), rowCount(b.View()))
	}
}

func TestANewRevisionReadsAgainAndTheSameOneDoesNot(t *testing.T) {
	b, _ := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	push(b, coordinator(boardToken(42)))
	if b.readCount() != 1 {
		t.Fatalf("reads = %d, want 1", b.readCount())
	}
	// An unrelated pane: the bridge is the same.
	push(b, boardTree(boardWorkspace("w1", true, boardPane("w1:p1", boardToken(42)), boardPane("w1:p9", nil))))
	if b.readCount() != 1 {
		t.Fatalf("reads = %d, want 1", b.readCount())
	}
	push(b, coordinator(boardToken(43)))
	if b.readCount() != 2 {
		t.Fatalf("reads = %d, want 2", b.readCount())
	}
}

func TestABridgePaneThatMovesWithTheSameSessionAndRevDoesNotRead(t *testing.T) {
	b, _ := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	push(b, boardTree(boardWorkspace("w1", true, boardPane("w1:p2", boardToken(42)))))
	if v := b.View(); b.readCount() != 1 || v.Bridge == nil || v.Bridge.PaneID != "w1:p2" {
		t.Fatalf("view = %+v, reads = %d", v, b.readCount())
	}
}

func TestRemovingTheTokenClearsTheBoard(t *testing.T) {
	b, _ := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	if b.View().Snapshot == nil {
		t.Fatal("no snapshot")
	}
	push(b, coordinator(nil))
	if v := b.View(); v.Bridge != nil || v.Snapshot != nil || v.Problem != "" {
		t.Fatalf("view = %+v", v)
	}
}

func TestABoardOfAnotherSessionIsAProblemAndNotShown(t *testing.T) {
	dir := boardDirWith(t, []byte(`{"version": 1, "session": "other", "rev": 1}`))
	b, _ := newTestBoard(dir, true)
	push(b, coordinator(boardToken(1)))
	if v := b.View(); v.Snapshot != nil || v.Problem != board.OtherSession {
		t.Fatalf("view = %+v", v)
	}
}

func TestAV2FileKeepsTheLastGoodBoardAndShowsTheVersionLine(t *testing.T) {
	dir := boardDir(t)
	b, _ := newTestBoard(dir, true)
	push(b, coordinator(boardToken(42)))
	v2 := fmt.Sprintf(`{"version": 2, "session": "%s", "rev": 43}`, boardSession)
	if err := os.WriteFile(board.Path(dir, boardSession), []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	push(b, coordinator(boardToken(43)))
	if v := b.View(); rowCount(v) != 3 || v.Problem != "board v2 · this contr reads v1" {
		t.Fatalf("view = %+v", v)
	}
}

func TestANewSessionDropsTheOldBoardWhenItsOwnReadFails(t *testing.T) {
	b, _ := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	if b.View().Snapshot == nil {
		t.Fatal("no snapshot")
	}
	// The other session has no board file, so its read fails and the old board must go.
	push(b, coordinator(strp("1 other-session 1")))
	if v := b.View(); v.Snapshot != nil || v.Problem == "" {
		t.Fatalf("view = %+v", v)
	}
}

func TestAMissingFileIsAProblemReadOnceAndNotRetried(t *testing.T) {
	b, _ := newTestBoard(t.TempDir(), true)
	push(b, coordinator(boardToken(1)))
	if v := b.View(); b.readCount() != 1 || v.Snapshot != nil || v.Problem != board.NoBoardFile {
		t.Fatalf("view = %+v, reads = %d", v, b.readCount())
	}
}

func TestAnUnreadableFileIsACalmProblem(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(board.Path(dir, boardSession), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := newTestBoard(dir, true)
	push(b, coordinator(boardToken(1)))
	if v := b.View(); v.Snapshot != nil || v.Problem != board.BoardUnreadable {
		t.Fatalf("view = %+v", v)
	}
}

func TestATruncatedFileIsAProblem(t *testing.T) {
	b, _ := newTestBoard(boardDirWith(t, boardFixture[:len(boardFixture)/2]), true)
	push(b, coordinator(boardToken(42)))
	if v := b.View(); v.Snapshot != nil || !strings.HasPrefix(v.Problem, "board unreadable: ") {
		t.Fatalf("view = %+v", v)
	}
}

func TestNoBoardDirIsAProblemAndNothingIsRead(t *testing.T) {
	b, _ := newTestBoard("", false)
	push(b, coordinator(boardToken(42)))
	if v := b.View(); b.readCount() != 0 || v.Bridge == nil || v.Problem != board.NoBoardDir {
		t.Fatalf("view = %+v, reads = %d", v, b.readCount())
	}
}

func TestAnUnchangedBridgeDoesNotNotifyTheBoard(t *testing.T) {
	b, n := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	before := n.Load()
	push(b, boardTree(boardWorkspace("w1", true, boardPane("w1:p1", boardToken(42)), boardPane("w1:p9", nil))))
	if n.Load() != before {
		t.Fatalf("notified %d times", n.Load()-before)
	}
}

func TestTheFirstTokenNotifiesForTheBridgeAndAgainForTheRead(t *testing.T) {
	b, n := newTestBoard(boardDir(t), true)
	push(b, coordinator(boardToken(42)))
	if n.Load() != 2 {
		t.Fatalf("notifications = %d, want 2 (bridge, snapshot)", n.Load())
	}
}

func TestADetachedBoardReadsNothing(t *testing.T) {
	b := NewDetachedBoard()
	push(b, coordinator(boardToken(1)))
	if b.readCount() != 0 || b.View().Problem != board.NoBoardDir {
		t.Fatalf("view = %+v", b.View())
	}
}
