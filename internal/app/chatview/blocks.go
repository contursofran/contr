package chatview

// Blocks (docs/spec/ChatView.md, Blocks; D267): a fenced block of Claude's text with the info
// string contr-status or contr-queue shows as a status board or a merge queue. The agent writes
// only the worker names, the issues, the labels and the order, as plain lines that read well in a
// terminal; contr fills the state, the PR and the checks from what the chips read (chips.go). A
// block with a line that does not parse shows as a code block.

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// The info strings of the blocks.
const (
	InfoStatus = "contr-status"
	InfoQueue  = "contr-queue"
)

// Labels and texts of the blocks.
const (
	// LabelBoard names a status board: "status board live" or "status board frozen".
	LabelBoard = "status board"
	// LabelQueue names a merge queue.
	LabelQueue = "merge queue"
	// LabelStep names a step of a queue, before its number: "queue step 2".
	LabelStep = "queue step"

	TextNotFound = "not found"
	TextNewer    = "a newer board is below"
)

// boardHeads are the heads of the columns of a board. A board without kept values shows only
// the first three: what the agent wrote.
var boardHeads = [...]string{"Worker", "Issue", "Builds", "State", "PR", "Checks"}

// BoardLine is a line of a status board: "b-649 | #649 | Rows and badge".
type BoardLine struct {
	Worker string
	Issue  int
	Builds string
}

// QueueStep is a line of a merge queue: "#656 Rows and badge -- waits for #667".
type QueueStep struct {
	Ref   int
	Label string
	Note  string
}

var (
	fenceRe  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*([^ \t`]*)[ \t]*$")
	statusRe = regexp.MustCompile(`^([^\s|]+)\s*\|\s*#(\d+)\s*\|\s*(\S.*?)\s*$`)
	queueRe  = regexp.MustCompile(`^#(\d+)\s+(.+?)(?:\s+--\s+(.+?))?\s*$`)
)

// fenced reads a fenced code block, fences included: its info string and its lines. The block
// must close with a fence of the same kind, at least as long.
func fenced(src string) (info string, lines []string, ok bool) {
	all := strings.Split(strings.TrimRight(src, "\n"), "\n")
	if len(all) < 2 {
		return "", nil, false
	}
	m := fenceRe.FindStringSubmatch(all[0])
	end := strings.TrimSpace(all[len(all)-1])
	if m == nil || len(end) < len(m[1]) || strings.Trim(end, m[1][:1]) != "" {
		return "", nil, false
	}
	return m[2], all[1 : len(all)-1], true
}

// ParseStatus reads a contr-status block: one line for each worker, blank lines skipped.
func ParseStatus(src string) ([]BoardLine, bool) {
	info, lines, ok := fenced(src)
	if !ok || info != InfoStatus {
		return nil, false
	}
	var out []BoardLine
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := statusRe.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			return nil, false
		}
		n, _ := strconv.Atoi(m[2])
		out = append(out, BoardLine{Worker: m[1], Issue: n, Builds: m[3]})
	}
	return out, len(out) > 0
}

// ParseQueue reads a contr-queue block: one line for each step, in order, blank lines skipped.
func ParseQueue(src string) ([]QueueStep, bool) {
	info, lines, ok := fenced(src)
	if !ok || info != InfoQueue {
		return nil, false
	}
	var out []QueueStep
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := queueRe.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			return nil, false
		}
		n, _ := strconv.Atoi(m[1])
		out = append(out, QueueStep{Ref: n, Label: m[2], Note: m[3]})
	}
	return out, len(out) > 0
}

// WorkerInfo is what contr knows of a worker now: its chip and the number of the PR of its
// branch, 0 for none.
type WorkerInfo struct {
	Chip Chip
	PR   int
}

// RefInfo is what contr knows of a number now: its chip, whether it is done (a merged or closed
// PR, a closed issue) and the counts of its checks.
type RefInfo struct {
	Chip   Chip
	Done   bool
	Checks github.CheckSummary
}

// RefInfoOf is the RefInfo of a number that gh read.
func RefInfoOf(r github.Ref) RefInfo {
	return RefInfo{Chip: RefChip(r), Done: r.State == "MERGED" || r.State == "CLOSED", Checks: r.Checks}
}

// BoardRow is what a board shows of a worker beyond what the agent wrote. Comparable: a board
// keeps the time its rows last changed.
type BoardRow struct {
	Found bool
	// Worker is the state of the worker's dot.
	Worker ChipState
	// State is the state word, in grey (D267): blocked-text for a worker that needs the owner,
	// ink-45 for one that is not found, else ink-62.
	State string
	// PR is the number of the worker's PR, 0 for none, and PRState the state of its dot.
	PR      int
	PRState ChipState
	// Checks is "2 of 3", or "—".
	Checks string
}

var workerWords = map[ChipState]string{StateNeedsYou: "needs you", StateWorking: "building", StateDone: "done", StateWaiting: "waiting"}

var prWords = map[ChipState]string{StateOpen: "PR open", StateRunning: "CI running", StatePassed: "CI passed",
	StateFailed: "CI failed", StateMerged: "merged", StateClosed: "closed"}

// BoardRowOf is the row of a worker: a worker that needs the owner says so; else the state of
// its PR when it has one; else the worker's own state. An unknown worker is "not found".
func BoardRowOf(w WorkerInfo, found bool, pr RefInfo, prKnown bool) BoardRow {
	if !found {
		return BoardRow{State: TextNotFound, Checks: "—"}
	}
	row := BoardRow{Found: true, Worker: w.Chip.State, State: workerWords[w.Chip.State], PR: w.PR, Checks: "—"}
	if w.PR == 0 {
		return row
	}
	row.PRState = StateOpen
	if prKnown {
		row.PRState = pr.Chip.State
		row.Checks = checksText(pr.Checks)
	}
	if w.Chip.State != StateNeedsYou {
		row.State = prWords[row.PRState]
	}
	return row
}

// checksText is "2 of 3": the passed checks of the checks that count (skipped ones do not), or
// "—" for none.
func checksText(s github.CheckSummary) string {
	total := s.Pass + s.Fail + s.Pending
	if total == 0 {
		return "—"
	}
	return strconv.Itoa(s.Pass) + " of " + strconv.Itoa(total)
}

// StepView is how a step of a queue shows: Done dims it; Merged draws a check on the rail;
// Blocked draws it in blocked; else Tone colors its dot (StateNone: ink-30). Its words are grey
// but for a blocked step (D267).
type StepView struct {
	Done, Merged, Blocked bool
	Tone                  ChipState
	Words                 string
}

// StepViewOf is the view of step s with what contr knows of its number: merged or closed it is
// done; with failed checks it is blocked; else its checks in words. The note follows the state;
// a note alone waits.
func StepViewOf(s QueueStep, r RefInfo, known bool) StepView {
	var v StepView
	var words string
	if known {
		switch st := r.Chip.State; {
		case r.Done:
			v.Done, v.Merged = true, st == StateMerged
			words = "closed"
			if v.Merged {
				words, v.Tone = "merged", StateMerged
			}
		case st == StateFailed:
			v.Blocked = true
			switch r.Checks.Fail {
			case 0:
				words = "blocked: checks failed"
			case 1:
				words = "blocked: 1 check failed"
			default:
				words = "blocked: " + strconv.Itoa(r.Checks.Fail) + " checks failed"
			}
		case st == StateRunning || st == StatePassed:
			words, v.Tone = prWords[st], st
			if t := checksText(r.Checks); t != "—" {
				words += " · " + t
			}
		case st == StateOpen:
			words, v.Tone = "open", st
		}
	}
	switch {
	case s.Note == "":
	case words == "":
		words, v.Tone = s.Note, StateWaiting
	case !v.Done:
		words += " · " + s.Note
	}
	v.Words = words
	return v
}

// blockSnap is what a board showed while it was live: its rows, and when they last changed.
type blockSnap struct {
	rows []BoardRow
	at   time.Time
}

// newestBoard is the key of the newest status board of the thread, "" for none: the last one of
// the last text that has one. It is found again only when the rows change.
func (ch *Chat) newestBoard() string {
	rows := ch.model.Rows()
	if ch.newestN == len(rows) && ch.newestOK {
		return ch.newest
	}
	ch.newest, ch.newestN, ch.newestOK = "", len(rows), true
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.Kind != RowSaid {
			continue
		}
		src := ch.model.Entry(r.Entry).Text
		if !strings.Contains(src, InfoStatus) {
			continue
		}
		parts := Parts(src)
		for k := len(parts) - 1; k >= 0; k-- {
			if parts[k].Board != nil {
				ch.newest = boardKey(r.ID, k)
				return ch.newest
			}
		}
	}
	return ""
}

func boardKey(id string, part int) string { return id + "/" + strconv.Itoa(part) }

// board is a status board, part part of the text row id that Claude sent at sent. The newest
// board of the thread is live; an older one shows what it kept while it was live, or only the
// lines the agent wrote, dimmed.
func (ch *Chat) board(c *ui.Context, id string, part int, lines []BoardLine, sent string) {
	key := boardKey(id, part)
	live := key == ch.newestBoard()
	snap := ch.boards[key]
	now := c.Now()
	if live {
		rows := make([]BoardRow, len(lines))
		addr := ch.src.Terminal()
		for k, l := range lines {
			if ch.Chips == nil {
				rows[k] = BoardRowOf(WorkerInfo{}, false, RefInfo{}, false)
				continue
			}
			w, found := ch.Chips.Worker(addr, l.Worker)
			var pr RefInfo
			known := false
			if found && w.PR > 0 {
				pr, known = ch.Chips.Ref(addr, w.PR)
			}
			rows[k] = BoardRowOf(w, found, pr, known)
		}
		if snap == nil || !slices.Equal(snap.rows, rows) {
			snap = &blockSnap{rows: rows, at: now}
			ch.boards[key] = snap
		}
	}
	var rows []BoardRow
	foot := TextNewer
	if snap != nil && len(snap.rows) == len(lines) {
		rows = snap.rows
		foot = "as of " + TimeText(snap.at, now) + " · " + foot
	} else if t, err := time.Parse(time.RFC3339Nano, sent); err == nil {
		foot = "as of " + TimeText(t, now) + " · " + foot
	}
	if live {
		foot = "live · updated " + TimeText(snap.at, now)
	}
	pal := theme.Current()
	label := LabelBoard + " frozen"
	if live {
		label = LabelBoard + " live"
	}
	box := ui.Column(c).Label(label).AlignSelf(ui.Stretch).Gap(theme.Space8)
	if !live {
		box.Opacity(0.6)
	}
	box.Children(func() {
		cols := len(boardHeads)
		if rows == nil {
			cols = 3
		}
		tracks := make([]ui.Track, cols)
		for k := range tracks {
			tracks[k] = ui.FitContent()
		}
		tracks[2] = ui.Fr(1)
		ui.Grid(c).ColumnTracks(tracks...).AlignSelf(ui.Stretch).Role(ui.RoleTable).Padding(theme.Space6, 0).
			BorderWidth(1, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
			for k, h := range boardHeads[:cols] {
				boardCell(c.Key(k), k == cols-1, ui.RoleColumnHeader, func() {
					kit.Styled(ui.Text(c, h), theme.StyleMeta).NoWrap().TextColor(pal.Ink45)
				})
			}
			for r, l := range lines {
				var row BoardRow
				if rows != nil {
					row = rows[r]
				}
				ch.boardRow(c, cols*(r+1), pal, l, row, rows != nil, cols)
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
			if live {
				ui.Box(c).Size(5, 5).Radius(2.5).Background(pal.ContentRunning)
			}
			kit.Styled(ui.Text(c, foot), theme.StyleMeta).NoWrap().TextColor(pal.Ink45)
		})
	})
}

// boardCell is a cell of a board: 16px of room at its right but in the last column.
func boardCell(c *ui.Context, last bool, role ui.Role, fn func()) {
	right := float32(theme.Space16)
	if last {
		right = 0
	}
	ui.Row(c).AlignItems(ui.Center).MinWidth(0).Padding(theme.Space4, right, theme.Space4, 0).Role(role).Children(fn)
}

// boardRow is the cells of line l, keyed from key: the worker with its dot, the issue, what it builds, and with
// values the state, the PR and the checks.
func (ch *Chat) boardRow(c *ui.Context, key int, pal *theme.Palette, l BoardLine, row BoardRow, values bool, cols int) {
	cell := func(k int, fn func()) { boardCell(c.Key(key+k), k == cols-1, ui.RoleCell, fn) }
	cell(0, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space7).Children(func() {
			ink := pal.Ink95
			if col, ok := ChipColor(pal, row.Worker); ok && values {
				ui.Box(c).Size(chipDot, chipDot).Radius(chipDot / 2).Background(col)
			} else if values && !row.Found {
				ink = pal.Ink45
			}
			kit.Styled(ui.Text(c, l.Worker), theme.StyleBody).NoWrap().TextColor(ink)
		})
	})
	cell(1, func() {
		kit.Styled(ui.Text(c, "#"+strconv.Itoa(l.Issue)), theme.StyleMeta).NoWrap().TextColor(pal.Ink62)
	})
	cell(2, func() {
		kit.Styled(ui.Text(c, l.Builds), theme.StyleBody).SingleLine().Ellipsis("…").MinWidth(0).TextColor(pal.Ink85)
	})
	if !values {
		return
	}
	cell(3, func() {
		ink := pal.Ink62
		switch {
		case row.Worker == StateNeedsYou:
			ink = pal.BlockedText
		case !row.Found:
			ink = pal.Ink45
		}
		kit.Styled(ui.Text(c, row.State), theme.StyleMeta).NoWrap().TextColor(ink)
	})
	cell(4, func() {
		if row.PR == 0 {
			kit.Styled(ui.Text(c, "—"), theme.StyleMeta).NoWrap().TextColor(pal.Ink45)
			return
		}
		ch.prChip(c, pal, row.PR, row.PRState)
	})
	cell(5, func() {
		kit.Styled(ui.Text(c, row.Checks), theme.StyleMeta).NoWrap().TextColor(pal.Ink62)
	})
}

// prChip is the PR of a board row: its dot and its number in mono on hover; a click opens it as
// a chip does.
func (ch *Chat) prChip(c *ui.Context, pal *theme.Palette, n int, st ChipState) {
	name := strconv.Itoa(n)
	chip := Chip{Kind: ChipPR, State: st}
	b := ui.ButtonBase(c).Label("PR #"+name).Shrink(0).Height(20).Padding(0, theme.Space6).Radius(5).
		Background(pal.Hover).Tooltip(chipTip(chip, name)).OnClick(func() {
		if ch.Chips != nil {
			ch.Chips.Open(ch.src.Terminal(), chip, name)
		}
	})
	b.Cursor(ui.CursorPointer).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
			if col, ok := ChipColor(pal, st); ok {
				ui.Box(c).Size(chipDot, chipDot).Radius(chipDot / 2).Background(col)
			}
			ui.Text(c, "#"+name).Font(theme.FontFamily).FontSize(12).NoWrap().TextColor(pal.Ink95)
		})
	})
}

// queue is a merge queue: its steps in order on one rail, each with its number, its label and
// its state in words.
func (ch *Chat) queue(c *ui.Context, steps []QueueStep) {
	pal := theme.Current()
	addr := ch.src.Terminal()
	ui.Column(c).Label(LabelQueue).Role(ui.RoleList).Children(func() {
		for k, s := range steps {
			var r RefInfo
			known := false
			if ch.Chips != nil {
				r, known = ch.Chips.Ref(addr, s.Ref)
			}
			v := StepViewOf(s, r, known)
			last := k == len(steps)-1
			c := c.Key(k)
			ui.Row(c).Label(LabelStep + " " + strconv.Itoa(k+1)).Role(ui.RoleListItem).Gap(theme.Space12).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Width(14).Shrink(0).AlignItems(ui.Center).Padding(markTop(v), 0, 0, 0).Children(func() {
					stepMark(c, pal, v)
					if !last {
						ui.Box(c).Width(1).Grow(1).Margin(3, 0).Background(pal.Hairline)
					}
				})
				bottom := float32(theme.Space10)
				if last {
					bottom = 0
				}
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Baseline).Gap(theme.Space10).Padding(0, 0, bottom, 0).Children(func() {
					kit.Styled(ui.Text(c, strconv.Itoa(k+1)), theme.StyleMeta).NoWrap().TextColor(pal.Ink45)
					ink := pal.Ink95
					if v.Done {
						ink = pal.Ink62
					}
					kit.Styled(ui.Text(c, "#"+strconv.Itoa(s.Ref)+" "+s.Label), theme.StyleProse).Shrink(1).MinWidth(0).TextColor(ink)
					if v.Words != "" {
						kit.Styled(ui.Text(c, v.Words), theme.StyleMeta).Shrink(1).MinWidth(0).TextColor(stepInk(pal, v))
					}
				})
			})
		}
	})
}

// markTop is the room above the mark of a step, so that its middle is near the middle of the
// label's first line.
func markTop(v StepView) float32 {
	if v.Merged {
		return theme.Space4
	}
	return theme.Space6
}

// stepMark is the mark of a step on the rail: a check for a merged step, else an 8px dot.
func stepMark(c *ui.Context, pal *theme.Palette, v StepView) {
	if v.Merged {
		ink := pal.ContentDone
		ui.Box(c).Size(12, 12).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			path := &ui.Path{}
			path.MoveTo(r.X+r.W*0.25, r.Y+r.H*0.52).LineTo(r.X+r.W*0.41, r.Y+r.H*0.68).LineTo(r.X+r.W*0.75, r.Y+r.H*0.33)
			p.StrokePath(path, 1.5, ink)
		})
		return
	}
	col := pal.Ink30
	switch {
	case v.Blocked:
		col = pal.Blocked
	case !v.Done:
		if t, ok := ChipColor(pal, v.Tone); ok {
			col = t
		}
	}
	ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Background(col)
}

// stepInk is the ink of a step's words: blocked-text for a blocked step, else ink-62. Only the
// rail's mark has the step's color (D267).
func stepInk(pal *theme.Palette, v StepView) ui.Color {
	if v.Blocked {
		return pal.BlockedText
	}
	return pal.Ink62
}
