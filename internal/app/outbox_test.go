package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/board"
)

const (
	outboxSession = "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f"
	outboxOther   = "other-session"
)

// fakeTimers is a clock that only moves when a test moves it.
type fakeTimers struct {
	mu     sync.Mutex
	now    uint64 // ms
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeTimers
	at      uint64
	f       func()
	stopped bool
	fired   bool
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

func (c *fakeTimers) Now() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeTimers) After(d time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now + uint64(d/time.Millisecond), f: f}
	c.timers = append(c.timers, t)
	return t
}

// advance moves the clock by ms and runs the timers that are due.
func (c *fakeTimers) advance(ms uint64) {
	c.mu.Lock()
	c.now += ms
	now := c.now
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && t.at <= now {
				next = t
				break
			}
		}
		if next != nil {
			next.fired = true
		}
		c.mu.Unlock()
		if next == nil {
			return
		}
		next.f()
	}
}

type outboxRig struct {
	t      *testing.T
	dir    string
	out    *BoardOutbox
	clock  *fakeTimers
	mu     sync.Mutex
	lines  []string
	change int
}

func newOutboxRig(t *testing.T, dir string, hasDir bool) *outboxRig {
	t.Helper()
	r := &outboxRig{t: t, dir: dir, clock: &fakeTimers{}}
	r.out = NewBoardOutbox(dir, hasDir, func() {
		r.mu.Lock()
		r.change++
		r.mu.Unlock()
	}, func(s string) {
		r.mu.Lock()
		r.lines = append(r.lines, s)
		r.mu.Unlock()
	})
	r.out.now = r.clock.Now
	r.out.after = r.clock.After
	t.Cleanup(r.out.Wait)
	return r
}

func (r *outboxRig) status() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

// show gives the outbox a board and waits for what that starts.
func (r *outboxRig) show(s *board.Snapshot) {
	r.out.Sync(s)
	r.out.Wait()
}

func (r *outboxRig) send(a board.Action) bool {
	ok := r.out.Send(a)
	r.out.Wait()
	return ok
}

func (r *outboxRig) advance(ms uint64) {
	r.clock.advance(ms)
	r.out.Wait()
}

func (r *outboxRig) counts() (writes, reads int) {
	r.out.mu.Lock()
	defer r.out.mu.Unlock()
	return r.out.writes, r.out.reads
}

func (r *outboxRig) timerSet() bool {
	r.out.mu.Lock()
	defer r.out.mu.Unlock()
	return r.out.timer != nil
}

func (r *outboxRig) queueSession() (string, bool) {
	r.out.mu.Lock()
	defer r.out.mu.Unlock()
	if r.out.queue == nil {
		return "", false
	}
	return r.out.queue.Session(), true
}

func (r *outboxRig) file(session string) (board.ActionsFile, bool) {
	data, err := os.ReadFile(board.ActionsPath(r.dir, session))
	if err != nil {
		return board.ActionsFile{}, false
	}
	return board.ParseActions(data)
}

func (r *outboxRig) seqs(session string) []uint64 {
	f, _ := r.file(session)
	var out []uint64
	for _, e := range f.Actions {
		out = append(out, e.Seq)
	}
	return out
}

func snap(session string, actions uint32, acted uint64, failed ...board.ActionFailure) *board.Snapshot {
	return &board.Snapshot{Version: 1, Session: session, Rev: 1, Actions: actions, Acted: acted, Failed: failed}
}

func answer(key string) board.Action { return board.Answer(key, 1, "two lines") }

func TestABoardThatTakesNoActionsSendsNothingAndWritesNothing(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 0, 0))

	if r.out.CanAct() {
		t.Error("CanAct is true")
	}
	if r.send(board.WrapUp(true)) {
		t.Error("Send returned true")
	}
	if w, _ := r.counts(); w != 0 {
		t.Errorf("writes = %d", w)
	}
	if _, ok := r.file(outboxSession); ok {
		t.Error("a file was written")
	}
}

func TestABoardWithoutADirectoryTakesNoActions(t *testing.T) {
	r := newOutboxRig(t, "", false)
	r.show(snap(outboxSession, 1, 0))
	if r.out.CanAct() || r.send(board.WrapUp(true)) {
		t.Error("an action was taken without a directory")
	}
}

func TestAMissingOrInertBoardSendsNothingButAHeldAnswerIsStillWritten(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	if !r.send(answer("D7")) {
		t.Fatal("answer not sent")
	}

	r.show(nil)
	if r.send(board.WrapUp(true)) {
		t.Error("sent with no board")
	}
	r.show(snap(outboxSession, 0, 0))
	if r.out.CanAct() {
		t.Error("CanAct on an inert board")
	}

	r.advance(board.UndoMS)
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Errorf("seqs = %v", got)
	}
	r.show(snap(outboxSession, 1, 0))
	if !r.out.CanAct() {
		t.Error("CanAct false after the board takes actions")
	}
}

func TestWrapUpIsWrittenAtOnceWithTheNextSeq(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 4))

	if !r.send(board.WrapUp(true)) {
		t.Fatal("not sent")
	}
	f, ok := r.file(outboxSession)
	if !ok || len(f.Actions) != 1 || f.Actions[0].Seq != 5 || f.Actions[0].Action != board.WrapUp(true) {
		t.Fatalf("file = %+v", f)
	}
	if w, _ := r.counts(); w != 1 {
		t.Errorf("writes = %d", w)
	}
	if r.timerSet() {
		t.Error("nothing is held, so no timer")
	}
}

func TestAnAnswerIsWrittenOnlyAfterTheUndoWindow(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))

	r.advance(board.UndoMS - 1)
	if _, ok := r.file(outboxSession); ok {
		t.Error("written too early")
	}
	if w, _ := r.counts(); w != 0 {
		t.Errorf("writes = %d", w)
	}
	if !r.timerSet() {
		t.Error("no timer while an answer is held")
	}

	r.advance(1)
	f, ok := r.file(outboxSession)
	if !ok || f.Actions[0].Seq != 1 || f.Actions[0].Action != answer("D7") {
		t.Fatalf("file = %+v", f)
	}
	if w, _ := r.counts(); w != 1 {
		t.Errorf("writes = %d", w)
	}
	if r.timerSet() {
		t.Error("timer still set")
	}
}

func TestUndoBeforeTheReleaseKeepsTheAnswerOutOfTheFile(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))
	if text, undoable, ok := r.out.PendingAnswer("D7"); !ok || text != "two lines" || !undoable {
		t.Fatalf("pending = %q %v %v", text, undoable, ok)
	}

	if !r.out.Undo("D7") {
		t.Error("undo failed")
	}
	if r.out.Undo("D7") {
		t.Error("a second undo has nothing to remove")
	}
	if r.out.Undo("D9") {
		t.Error("an unknown key has nothing to remove")
	}
	r.advance(board.UndoMS)
	if _, ok := r.file(outboxSession); ok {
		t.Error("file written")
	}
	if w, _ := r.counts(); w != 0 {
		t.Errorf("writes = %d", w)
	}
	if _, _, ok := r.out.PendingAnswer("D7"); ok {
		t.Error("answer still pending")
	}
	if r.timerSet() {
		t.Error("timer set with nothing held")
	}
}

func TestAWrittenAnswerCanNoLongerBeUndone(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))
	r.advance(board.UndoMS)

	if text, undoable, ok := r.out.PendingAnswer("D7"); !ok || text != "two lines" || undoable {
		t.Errorf("pending = %q %v %v", text, undoable, ok)
	}
	if r.out.Undo("D7") {
		t.Error("undo of a written answer")
	}
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Errorf("seqs = %v", got)
	}
}

func TestTwoQuickSendsLeaveTheNewerFileOnDisk(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))

	r.out.Send(board.WrapUp(true))
	r.out.Send(board.WrapUp(false))
	r.out.Wait()
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1, 2}) {
		t.Errorf("seqs = %v", got)
	}
	if w, _ := r.counts(); w != 2 {
		t.Errorf("writes = %d", w)
	}
}

func TestANewActedRewritesTheFileWithoutTheTakenActions(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.WrapUp(true))
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Fatalf("seqs = %v", got)
	}

	r.show(snap(outboxSession, 1, 1))
	if got := r.seqs(outboxSession); len(got) != 0 {
		t.Errorf("seqs = %v", got)
	}
	if w, _ := r.counts(); w != 2 {
		t.Errorf("writes = %d", w)
	}
}

func TestAReportThatChangesNothingWritesNothing(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.WrapUp(true))

	r.show(snap(outboxSession, 1, 0))
	if w, _ := r.counts(); w != 1 {
		t.Errorf("writes = %d", w)
	}
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Errorf("seqs = %v", got)
	}
}

func fail(seq uint64, key, text string) board.ActionFailure {
	f := board.ActionFailure{Seq: seq, Text: text}
	if key != "" {
		f.Key = &key
	}
	return f
}

func TestANewFailureIsShownAndItsAnswerIsDropped(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))
	r.advance(board.UndoMS)

	r.show(snap(outboxSession, 1, 1, fail(1, "D7", "D7 is not open")))
	if got := r.status(); !slices.Equal(got, []string{"D7 not sent: D7 is not open"}) {
		t.Errorf("status = %q", got)
	}
	if _, _, ok := r.out.PendingAnswer("D7"); ok {
		t.Error("answer still pending")
	}
	if got := r.seqs(outboxSession); len(got) != 0 {
		t.Errorf("seqs = %v", got)
	}
}

func TestAFailureWithoutAKeyIsShownWithoutOne(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.ClearRows())

	r.show(snap(outboxSession, 1, 1, fail(1, "", "nothing to clear")))
	if got := r.status(); !slices.Equal(got, []string{"not sent: nothing to clear"}) {
		t.Errorf("status = %q", got)
	}
}

func TestAFailureIsShownOnce(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.WrapUp(true))

	f := fail(1, "D7", "D7 is not open")
	r.show(snap(outboxSession, 1, 1, f))
	r.show(snap(outboxSession, 1, 1, f))
	if got := r.status(); len(got) != 1 {
		t.Errorf("status = %q, want one line", got)
	}
}

func TestSeveralNewFailuresShareOneLine(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.show(snap(outboxSession, 1, 2, fail(1, "D1", "a"), fail(2, "", "b")))
	if got := r.status(); !slices.Equal(got, []string{"D1 not sent: a; not sent: b"}) {
		t.Errorf("status = %q", got)
	}
}

func TestASessionChangeStartsANewQueueAndLeavesTheOldFile(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.WrapUp(true))
	before, err := os.ReadFile(board.ActionsPath(r.dir, outboxSession))
	if err != nil {
		t.Fatal(err)
	}

	r.show(snap(outboxOther, 1, 0))
	if s, ok := r.queueSession(); !ok || s != outboxOther {
		t.Errorf("session = %q %v", s, ok)
	}
	r.send(board.WrapUp(false))

	if got := r.seqs(outboxOther); !slices.Equal(got, []uint64{1}) {
		t.Errorf("seqs = %v", got)
	}
	after, _ := os.ReadFile(board.ActionsPath(r.dir, outboxSession))
	if !bytes.Equal(before, after) {
		t.Error("the old file changed")
	}
}

func TestAHeldAnswerIsWrittenToItsOwnSessionWhenTheSessionChanges(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))

	r.show(snap(outboxOther, 1, 0))
	r.out.Wait()
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Fatalf("seqs = %v", got)
	}
	f, _ := r.file(outboxSession)
	if f.Actions[0].Action != answer("D7") {
		t.Errorf("action = %+v", f.Actions[0].Action)
	}
	if _, ok := r.file(outboxOther); ok {
		t.Error("a file was written for the new session")
	}
	if r.timerSet() {
		t.Error("timer of the old session is still set")
	}
}

func TestABoardOfAnotherSessionThatTakesNoActionsLeavesNoQueue(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.show(snap(outboxOther, 0, 0))

	if _, ok := r.queueSession(); ok {
		t.Error("a queue is left")
	}
	if r.out.CanAct() {
		t.Error("CanAct is true")
	}
}

func TestAnExistingFileKeepsTheActionsAboveActedAndTheSeqGoesOn(t *testing.T) {
	dir := t.TempDir()
	existing := `{"version":1,"session":"` + outboxSession + `","actions":[{"seq":1,"at":1,"do":"wrap_up","on":true},{"seq":3,"at":2,"do":"clear_rows"}]}`
	if err := os.WriteFile(board.ActionsPath(dir, outboxSession), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newOutboxRig(t, dir, true)
	r.show(snap(outboxSession, 1, 1))
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1, 3}) {
		t.Errorf("nothing is written on adopt, seqs = %v", got)
	}

	r.send(board.ClearItems())
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{3, 4}) {
		t.Errorf("seqs = %v", got)
	}
}

func TestAWriteErrorIsShownCutToSixtyCharacters(t *testing.T) {
	parent := t.TempDir()
	// The board directory is a file, so the actions directory can not be made.
	blocked := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newOutboxRig(t, filepath.Join(blocked, "sub"), true)
	r.show(snap(outboxSession, 1, 0))

	r.send(board.WrapUp(true))
	lines := r.status()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "actions not saved: ") {
		t.Fatalf("status = %q", lines)
	}
	if n := len([]rune(lines[0])); n > maxErrorChars {
		t.Errorf("line has %d characters", n)
	}
}

func TestAFailedRenameLeavesNoTmpFile(t *testing.T) {
	dir := t.TempDir()
	// The actions file is a directory, so the rename of the .tmp file onto it fails.
	if err := os.Mkdir(board.ActionsPath(dir, outboxSession), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newOutboxRig(t, dir, true)
	r.show(snap(outboxSession, 1, 0))

	r.send(board.WrapUp(true))
	lines := r.status()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "actions not saved: ") {
		t.Fatalf("status = %q", lines)
	}
	if _, err := os.Stat(board.ActionsPath(dir, outboxSession) + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the .tmp file is not removed: %v", err)
	}
}

func TestASnapshotOfTheSameSessionDuringAReadDoesNotReadAgain(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	// Two snapshots of one session before the read of its file finishes.
	r.out.Sync(snap(outboxSession, 1, 0))
	r.out.Sync(snap(outboxSession, 1, 0))
	r.out.Wait()

	if _, reads := r.counts(); reads != 1 {
		t.Errorf("reads = %d, want 1", reads)
	}
	if s, ok := r.queueSession(); !ok || s != outboxSession {
		t.Errorf("session = %q %v", s, ok)
	}
	if !r.out.CanAct() {
		t.Error("CanAct is false")
	}
}

func TestASessionThatReturnsAfterItsQueueWasDroppedIsReadAgain(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.show(snap(outboxOther, 1, 0))
	r.show(snap(outboxSession, 1, 0))

	if _, reads := r.counts(); reads != 3 {
		t.Errorf("reads = %d, want 3", reads)
	}
	if s, ok := r.queueSession(); !ok || s != outboxSession {
		t.Errorf("session = %q %v", s, ok)
	}
	if !r.out.CanAct() {
		t.Error("CanAct is false")
	}
}

func TestQuitWritesTheHeldActionsAtOnce(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(answer("D7"))

	r.out.ReleaseForQuit()
	r.out.Wait()
	if got := r.seqs(outboxSession); !slices.Equal(got, []uint64{1}) {
		t.Errorf("seqs = %v", got)
	}
	if w, _ := r.counts(); w != 1 {
		t.Errorf("writes = %d", w)
	}
	r.advance(board.UndoMS)
	if w, _ := r.counts(); w != 1 {
		t.Errorf("the held answer was written twice, writes = %d", w)
	}
}

func TestALongWriteErrorIsCutToSixtyCharacters(t *testing.T) {
	line := saveErrorLine(errors.New(strings.Repeat("x", 100)))
	if n := len([]rune(line)); n != maxErrorChars {
		t.Errorf("line has %d characters", n)
	}
	if !strings.HasPrefix(line, "actions not saved: x") {
		t.Errorf("line = %q", line)
	}
}

func TestAFailureLineDropsTheKeyWhenItIsMissingOrEmpty(t *testing.T) {
	keyed := fail(1, "D7", "D7 is not open")
	empty := ""
	blank := board.ActionFailure{Seq: 1, Key: &empty, Text: "D7 is not open"}
	if got := actionFailureLine(keyed); got != "D7 not sent: D7 is not open" {
		t.Errorf("keyed = %q", got)
	}
	if got := actionFailureLine(blank); got != "not sent: D7 is not open" {
		t.Errorf("blank = %q", got)
	}
}

// The file is written with the file's own JSON, so text is not HTML-escaped.
func TestTheFileIsWrittenByteExactWithoutHTMLEscapes(t *testing.T) {
	r := newOutboxRig(t, t.TempDir(), true)
	r.show(snap(outboxSession, 1, 0))
	r.send(board.Answer("D7", 1, "a<b>&c"))
	r.advance(board.UndoMS)

	got, err := os.ReadFile(board.ActionsPath(r.dir, outboxSession))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := r.file(outboxSession)
	if !bytes.Equal(got, f.JSON()) {
		t.Errorf("bytes differ from ActionsFile.JSON:\n%s\n%s", got, f.JSON())
	}
	if !bytes.Contains(got, []byte("a<b>&c")) || bytes.Contains(got, []byte(`\u003c`)) {
		t.Errorf("text is escaped: %s", got)
	}
}
