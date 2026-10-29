package app

// The queue of actions that contr sends to the coordinator mod, as the UI
// holds it. One [BoardOutbox] serves the session of the board. Its file is
// written on a goroutine, through a ".tmp" file and a rename, and writes run
// one after another. A held answer, done or dismiss is released by a timer,
// and the timer exists only while such an action is held. Nothing here is
// polled, and nothing blocks the UI thread. Ported from the Rust contr module
// board_outbox.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/contursofran/contr/internal/board"
)

// maxErrorChars is the longest status line for a failed write. The line is
// cut to this many characters.
const maxErrorChars = 60

// stopper is a timer that can be stopped. [time.Timer] fits it.
type stopper interface{ Stop() bool }

// writeJob is one write of an actions file.
type writeJob struct {
	path string
	file board.ActionsFile
}

// BoardOutbox holds the actions that contr has queued for the board's
// session. It is safe to use from several goroutines. Its callbacks run
// without its lock held.
type BoardOutbox struct {
	dir    string
	hasDir bool
	// onChange runs when what the views read changed. It should only queue
	// work with Window.Update or ask for a redraw.
	onChange func()
	// status shows a line in the status bar.
	status func(string)
	// now is the wall clock in milliseconds. after starts a timer. Tests
	// replace both.
	now   func() uint64
	after func(d time.Duration, f func()) stopper

	mu sync.Mutex
	// queue is nil until a board of a session that takes actions is read,
	// and after a board of another session replaces it.
	queue *board.Outbox
	// live is true while the latest board shows the session of queue and
	// that session takes actions.
	live bool
	// timer wakes when the earliest held action is due; nil while nothing is
	// held. timerGen drops a timer that fired after it was replaced.
	timer    stopper
	timerGen uint64
	// loading is the session whose actions file is being read; loadGen drops
	// a read that a newer one replaced.
	loading   string
	isLoading bool
	loadGen   uint64
	pending   []writeJob
	writing   bool
	wg        sync.WaitGroup
	writes    int // writes started; tests count them
	reads     int // reads started; tests count them
}

// NewBoardOutbox returns a queue that writes under dir. hasDir false means
// there is no board directory, so no board takes actions. onChange and
// status may be nil. Call [BoardOutbox.Sync] when the board changes.
func NewBoardOutbox(dir string, hasDir bool, onChange func(), status func(string)) *BoardOutbox {
	return &BoardOutbox{
		dir:      dir,
		hasDir:   hasDir,
		onChange: onChange,
		status:   status,
		now:      nowMS,
		after: func(d time.Duration, f func()) stopper {
			return time.AfterFunc(d, f)
		},
	}
}

// nowMS is the wall clock in milliseconds since the Unix epoch. 0 before it.
func nowMS() uint64 {
	ms := time.Now().UnixMilli()
	if ms < 0 {
		return 0
	}
	return uint64(ms)
}

// CanAct is true when an action can be sent: the latest board shows the
// queued session and that session takes actions.
func (o *BoardOutbox) CanAct() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.canActLocked()
}

func (o *BoardOutbox) canActLocked() bool { return o.live && o.queue != nil }

// Send queues action. It returns false, and queues nothing, when the board
// takes no actions. An answer, done or dismiss waits [board.UndoMS] before it
// is written. Any other action is written now.
func (o *BoardOutbox) Send(action board.Action) bool {
	o.mu.Lock()
	if !o.canActLocked() {
		o.mu.Unlock()
		return false
	}
	now := o.now()
	if _, written := o.queue.Push(action, now); written {
		o.saveLocked()
	}
	o.armTimerLocked(now)
	o.mu.Unlock()
	o.notify()
	return true
}

// Undo removes the held answer, done or dismiss for key. It returns false
// when none is held: the action was written already, or it never existed.
func (o *BoardOutbox) Undo(key string) bool {
	o.mu.Lock()
	removed := o.queue != nil && o.queue.Undo(key)
	if removed {
		o.armTimerLocked(o.now())
	}
	o.mu.Unlock()
	if removed {
		o.notify()
	}
	return removed
}

// PendingAnswer is the answer text for key and true in undoable when it can
// still be undone. ok is false when there is none.
func (o *BoardOutbox) PendingAnswer(key string) (text string, undoable, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.queue == nil {
		return "", false, false
	}
	return o.queue.PendingAnswer(key)
}

// Sync follows the board that the views read. snap is nil when there is none.
// A board of the queued session applies its report. A board of another
// session replaces the queue, after the held actions of the old one are
// written.
func (o *BoardOutbox) Sync(snap *board.Snapshot) {
	o.mu.Lock()
	var lines []string
	if snap == nil {
		o.live = false
		o.dropLoadLocked()
	} else {
		takes := snap.Actions >= 1 && o.hasDir
		if o.queue != nil && o.queue.Session() == snap.Session {
			o.live = takes
			if takes {
				lines = o.applyLocked(snap)
			}
		} else {
			o.live = false
			o.dropQueueLocked()
			switch {
			case !takes:
				o.dropLoadLocked()
			case !o.readingLocked(snap.Session):
				o.loadLocked(snap)
			}
		}
	}
	o.mu.Unlock()
	o.show(lines)
	o.notify()
}

// readingLocked is true while the read of session's actions file is in
// flight. The read is not started again.
func (o *BoardOutbox) readingLocked(session string) bool {
	return o.queue == nil && o.isLoading && o.loading == session
}

func (o *BoardOutbox) dropLoadLocked() {
	o.isLoading = false
	o.loading = ""
	o.loadGen++
}

// dropQueueLocked ends the queue of another session. Its held actions are
// written first, so none is lost.
func (o *BoardOutbox) dropQueueLocked() {
	if o.queue != nil && o.queue.ReleaseAll() {
		o.saveLocked()
	}
	o.queue = nil
	o.stopTimerLocked()
}

// loadLocked reads the existing actions file of snap's session on a
// goroutine, then adopts it.
func (o *BoardOutbox) loadLocked(snap *board.Snapshot) {
	o.reads++
	o.loadGen++
	gen := o.loadGen
	o.loading, o.isLoading = snap.Session, true
	path := board.ActionsPath(o.dir, snap.Session)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		existing := readExisting(path)
		o.adopt(gen, snap, existing)
	}()
}

// adopt creates the queue from the file that was read, then applies the
// snapshot's report. A read that a newer one replaced is dropped.
func (o *BoardOutbox) adopt(gen uint64, snap *board.Snapshot, existing *board.ActionsFile) {
	o.mu.Lock()
	if gen != o.loadGen {
		o.mu.Unlock()
		return
	}
	o.isLoading, o.loading = false, ""
	o.queue = board.NewOutbox(snap.Session, snap.Acted, existing)
	o.live = true
	lines := o.applyLocked(snap)
	o.mu.Unlock()
	o.show(lines)
	o.notify()
}

// applyLocked applies the mod's report to the queue: the actions it took are
// dropped, and each new failure is returned once as a status line.
func (o *BoardOutbox) applyLocked(snap *board.Snapshot) []string {
	if o.queue == nil {
		return nil
	}
	acked := o.queue.Ack(snap.Acted)
	var lines []string
	for _, f := range o.queue.NewFailures(snap.Failed) {
		lines = append(lines, actionFailureLine(f))
	}
	if acked {
		o.saveLocked()
	}
	return lines
}

// saveLocked writes the whole queue in the background. A write starts after
// the write before it.
func (o *BoardOutbox) saveLocked() {
	if o.queue == nil || !o.hasDir {
		return
	}
	o.writes++
	o.pending = append(o.pending, writeJob{
		path: board.ActionsPath(o.dir, o.queue.Session()),
		file: o.queue.File(),
	})
	if o.writing {
		return
	}
	o.writing = true
	o.wg.Add(1)
	go o.drain()
}

// drain runs the queued writes in order, then ends. No goroutine stays while
// there is nothing to write.
func (o *BoardOutbox) drain() {
	defer o.wg.Done()
	for {
		o.mu.Lock()
		if len(o.pending) == 0 {
			o.writing = false
			o.mu.Unlock()
			return
		}
		job := o.pending[0]
		o.pending = o.pending[1:]
		o.mu.Unlock()
		if err := writeFile(job.path, job.file); err != nil {
			o.show([]string{saveErrorLine(err)})
		}
	}
}

// armTimerLocked sets the timer for the earliest held action, counted from
// now. There is no timer while nothing is held.
func (o *BoardOutbox) armTimerLocked(now uint64) {
	o.stopTimerLocked()
	if o.queue == nil {
		return
	}
	due, ok := o.queue.NextRelease()
	if !ok {
		return
	}
	var wait uint64
	if due > now {
		wait = due - now
	}
	gen := o.timerGen
	o.timer = o.after(time.Duration(wait)*time.Millisecond, func() { o.releaseDue(gen, due) })
}

func (o *BoardOutbox) stopTimerLocked() {
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	o.timerGen++
}

// releaseDue writes the held actions that are due at due, then waits for the
// next one.
func (o *BoardOutbox) releaseDue(gen, due uint64) {
	o.mu.Lock()
	if gen != o.timerGen {
		o.mu.Unlock()
		return
	}
	o.timer = nil
	released := o.queue != nil && o.queue.Release(due)
	if released {
		o.saveLocked()
	}
	o.armTimerLocked(due)
	o.mu.Unlock()
	if released {
		o.notify()
	}
}

// ReleaseForQuit writes every held action at once and stops the timer. Call
// [BoardOutbox.Wait] after it to wait for the last write.
func (o *BoardOutbox) ReleaseForQuit() {
	o.mu.Lock()
	if o.queue != nil && o.queue.ReleaseAll() {
		o.saveLocked()
	}
	o.stopTimerLocked()
	o.mu.Unlock()
}

// Wait returns when the writes and reads started so far have finished.
func (o *BoardOutbox) Wait() { o.wg.Wait() }

func (o *BoardOutbox) notify() {
	if o.onChange != nil {
		o.onChange()
	}
}

func (o *BoardOutbox) show(lines []string) {
	if len(lines) == 0 || o.status == nil {
		return
	}
	text := lines[0]
	for _, l := range lines[1:] {
		text += "; " + l
	}
	o.status(text)
}

// actionFailureLine is the status line for one failed action: "{key} not sent:
// {text}", or "not sent: {text}" without a key.
func actionFailureLine(f board.ActionFailure) string {
	if f.Key != nil && *f.Key != "" {
		return *f.Key + " not sent: " + f.Text
	}
	return "not sent: " + f.Text
}

// saveErrorLine is the status line for a failed write, cut to maxErrorChars
// characters.
func saveErrorLine(err error) string {
	r := []rune(fmt.Sprintf("actions not saved: %v", err))
	if len(r) > maxErrorChars {
		r = r[:maxErrorChars]
	}
	return string(r)
}

// readExisting reads the actions file at path. A missing, unreadable or
// unknown file is nil, and the queue starts empty.
func readExisting(path string) *board.ActionsFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	file, ok := board.ParseActions(data)
	if !ok {
		return nil
	}
	return &file
}

// writeFile writes file to path through "<path>.tmp" and a rename, creating
// the directory when it is missing. It uses the file's own JSON method, so the
// bytes match the Rust file and nothing is HTML-escaped.
func writeFile(path string, file board.ActionsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	err := os.WriteFile(tmp, file.JSON(), 0o644)
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		// A failed write or rename leaves no .tmp behind.
		_ = os.Remove(tmp)
	}
	return err
}
