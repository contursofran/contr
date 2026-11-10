// Package chatview is the chat view of a Claude Code session (docs/spec/ChatView.md, D246): the
// session from its transcript (package transcript), read only, in the place of the terminal grid.
package chatview

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/transcript"
)

// RowKind is the kind of a row of the chat.
type RowKind int

// The row kinds.
const (
	// RowPrompt is a text the owner typed.
	RowPrompt RowKind = iota
	// RowSaid is a text of Claude, in Markdown.
	RowSaid
	// RowGroup is the work between two texts: tool calls, kit notes and background tasks, folded
	// into one row.
	RowGroup
	// RowOwnerNote is a kit note or a background task that needs the owner (NeedsOwner). It shows
	// outside the fold.
	RowOwnerNote
)

// Row is one row of the chat. Entry is the index of the entry of a prompt, a text or an owner
// note; Notes (kit notes and background tasks) and Calls are the indexes of the entries of a work
// group, in order.
type Row struct {
	Kind  RowKind
	ID    string
	Entry int
	Notes []int
	Calls []int
}

// Model holds the entries of one transcript and its rows. Apply adds a batch of the reader in
// place: new entries append, a result updates its call. UI thread only.
type Model struct {
	entries []transcript.Entry
	ids     map[string]int // stable id -> entry index
	rows    []Row
	// open tells that the last row is a work group that takes the next tool calls and notes.
	open bool
	// pending holds the texts the owner sent that the transcript has not brought yet.
	pending []Pending
	seq     int
}

// Pending is a text the owner sent from the composer, shown dimmed at the end until the
// transcript brings its prompt entry.
type Pending struct {
	ID, Text string
}

// AddPending shows text as a pending prompt and returns its id.
func (m *Model) AddPending(text string) string {
	m.seq++
	id := "pending:" + strconv.Itoa(m.seq)
	m.pending = append(m.pending, Pending{ID: id, Text: text})
	return id
}

// DropPending removes the pending prompt id: its send failed.
func (m *Model) DropPending(id string) {
	m.pending = slices.DeleteFunc(m.pending, func(p Pending) bool { return p.ID == id })
}

// Pendings are the pending prompts, in the order they were sent. The caller must not change them.
func (m *Model) Pendings() []Pending { return m.pending }

// matchPending drops the first pending prompt with the text of a prompt entry.
func (m *Model) matchPending(text string) {
	text = strings.TrimSpace(text)
	if i := slices.IndexFunc(m.pending, func(p Pending) bool { return strings.TrimSpace(p.Text) == text }); i >= 0 {
		m.pending = slices.Delete(m.pending, i, i+1)
	}
}

// Reset clears the model, for a read again from offset 0.
func (m *Model) Reset() { *m = Model{} }

// Rows are the rows, in order. The caller must not change them.
func (m *Model) Rows() []Row { return m.rows }

// Entry is the entry at index i.
func (m *Model) Entry(i int) transcript.Entry { return m.entries[i] }

// Len is the count of entries, hidden ones included.
func (m *Model) Len() int { return len(m.entries) }

// Apply adds what one read gave: the results of earlier calls, then the new entries.
func (m *Model) Apply(b transcript.Batch) {
	for _, r := range b.Results {
		m.result(r)
	}
	for _, e := range b.Entries {
		m.add(e)
	}
}

// result puts a result in its call: by the reader's index, else by the call id.
func (m *Model) result(r transcript.Result) {
	i := r.Index
	if i < 0 || i >= len(m.entries) || m.entries[i].Tool == nil || m.entries[i].Tool.ID != r.ID {
		var ok bool
		if i, ok = m.ids[callID(r.ID)]; !ok {
			return
		}
	}
	c := *m.entries[i].Tool
	c.Done, c.Error, c.Excerpt, c.End = true, r.Error, r.Excerpt, r.Time
	m.entries[i].Tool = &c
}

// callID is the stable id of a tool call.
func callID(id string) string { return "call:" + id }

// id gives entry i its stable id: the call id of a tool call, else the uuid. The blocks of one
// line share its uuid, so a uuid seen before, or none, takes the entry's index.
func (m *Model) id(e transcript.Entry, i int) string {
	id := e.UUID
	if e.Kind == transcript.Tool && e.Tool != nil && e.Tool.ID != "" {
		id = callID(e.Tool.ID)
	}
	if _, seen := m.ids[id]; id == "" || seen {
		id = "#" + strconv.Itoa(i)
	}
	if m.ids == nil {
		m.ids = map[string]int{}
	}
	m.ids[id] = i
	return id
}

// add appends one entry and places it in the rows. Hidden entries, thinking marks and the lines
// of subagents keep their place among the entries but show in no row.
func (m *Model) add(e transcript.Entry) {
	i := len(m.entries)
	m.entries = append(m.entries, e)
	id := m.id(e, i)
	if e.Sidechain {
		return
	}
	switch e.Kind {
	case transcript.Prompt:
		m.matchPending(e.Text)
		m.rows, m.open = append(m.rows, Row{Kind: RowPrompt, ID: id, Entry: i}), false
	case transcript.Said:
		m.rows, m.open = append(m.rows, Row{Kind: RowSaid, ID: id, Entry: i}), false
	case transcript.Note:
		if NeedsOwner(e) {
			m.rows, m.open = append(m.rows, Row{Kind: RowOwnerNote, ID: id, Entry: i}), false
			return
		}
		g := m.group(id)
		g.Notes = append(g.Notes, i)
	case transcript.Tool:
		g := m.group(id)
		g.Calls = append(g.Calls, i)
	}
}

// group returns the open work group, or a new one named after the entry id that starts it.
func (m *Model) group(id string) *Row {
	if !m.open {
		m.rows, m.open = append(m.rows, Row{Kind: RowGroup, ID: "group:" + id}), true
	}
	return &m.rows[len(m.rows)-1]
}

// TextThinking and TextWorking are what the live line says the agent does when no tool call runs.
const (
	TextThinking = "Thinking"
	TextWorking  = "Working"
)

// Activity is what the agent does now, from the end of the transcript: the running tool call
// ("Bash · go test ./..."), "Thinking" when a thinking mark is the last entry, else "Working".
// start is the time of the last owner prompt; ok is false when it has no time. Hidden entries
// and the lines of subagents do not count.
func (m *Model) Activity() (what string, start time.Time, ok bool) {
	what = TextWorking
	last := true
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if e.Sidechain || e.Kind == transcript.Hidden {
			continue
		}
		if e.Kind == transcript.Prompt {
			t, err := time.Parse(time.RFC3339Nano, e.Time)
			return what, t, err == nil
		}
		if last && e.Kind == transcript.Thinking {
			what = TextThinking
		}
		last = false
		if what == TextWorking && e.Kind == transcript.Tool && e.Tool != nil && !e.Tool.Done {
			what = ToolName(e.Tool.Name)
			if e.Tool.Summary != "" {
				what += " · " + e.Tool.Summary
			}
		}
	}
	return what, time.Time{}, false
}

// ElapsedText is the time of a turn as the live line shows it: "8s", "2m 5s", "1h 4m".
func ElapsedText(d time.Duration) string {
	s := int(d / time.Second)
	switch {
	case s < 60:
		return strconv.Itoa(max(s, 0)) + "s"
	case s < 3600:
		return strconv.Itoa(s/60) + "m " + strconv.Itoa(s%60) + "s"
	}
	return strconv.Itoa(s/3600) + "h " + strconv.Itoa(s%3600/60) + "m"
}

// Running tells that a call of the group has no result yet: work goes on.
func (m *Model) Running(g Row) bool {
	for _, i := range g.Calls {
		if c := m.entries[i].Tool; c != nil && !c.Done {
			return true
		}
	}
	return false
}

// Senders are the hc addresses of the workers that sent notes in the group, each once, in the
// order of their first note.
func (m *Model) Senders(g Row) []string {
	var out []string
	for _, i := range g.Notes {
		n := m.entries[i].Note
		if n == nil || n.Addr == "" || containsString(out, n.Addr) {
			continue
		}
		out = append(out, n.Addr)
	}
	return out
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// NeedsOwner tells that a kit note needs the owner: an hc message of a worker that ended its turn
// without a report, or that says the worker is blocked ("NAME: blocked, …", the kit's report
// line, or a text that starts with "blocked"), or a background task that failed or was killed.
func NeedsOwner(e transcript.Entry) bool {
	if e.Kind != transcript.Note || e.Note == nil {
		return false
	}
	if e.Note.Kind == transcript.NoteTask {
		return TaskFailed(e.Note.Status)
	}
	if e.Note.Kind != transcript.NoteMessage {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(e.Text))
	if strings.HasPrefix(s, "ended its turn without a report") || firstWord(s) == "blocked" {
		return true
	}
	who, rest, ok := strings.Cut(s, ":")
	return ok && !strings.Contains(who, " ") && firstWord(rest) == "blocked"
}

// TaskFailed tells a background task's status that needs the owner: "failed" or "killed".
func TaskFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "killed":
		return true
	}
	return false
}

// TaskSummary is the summary of a background task as its line shows it: without the leading
// "Background command " of the harness, as the line starts with "Background task".
func TaskSummary(s string) string {
	return strings.TrimPrefix(s, "Background command ")
}

// Counts are the notes of the group that are messages, and those that are background tasks.
func (m *Model) Counts(g Row) (notes, tasks int) {
	for _, i := range g.Notes {
		if n := m.entries[i].Note; n != nil && n.Kind == transcript.NoteTask {
			tasks++
		} else {
			notes++
		}
	}
	return notes, tasks
}

// firstWord is the first word of s without the marks after it.
func firstWord(s string) string {
	w, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return strings.TrimRight(w, ",.;:")
}

// GroupText is the text of a folded work group: "3 worker notes · 1 background task · 6 tool
// calls". A part with no item is left out.
func GroupText(notes, tasks, calls int) string {
	var parts []string
	if notes > 0 {
		parts = append(parts, count(notes, "worker note"))
	}
	if tasks > 0 {
		parts = append(parts, count(tasks, "background task"))
	}
	if calls > 0 {
		parts = append(parts, count(calls, "tool call"))
	}
	return strings.Join(parts, " · ")
}

func count(n int, what string) string {
	s := strconv.Itoa(n) + " " + what
	if n != 1 {
		s += "s"
	}
	return s
}

// Duration is how long a call took, from the timestamps of the call and its result, and false
// when either is missing or does not parse.
func Duration(e transcript.Entry) (time.Duration, bool) {
	if e.Tool == nil || !e.Tool.Done || e.Time == "" || e.Tool.End == "" {
		return 0, false
	}
	start, err1 := time.Parse(time.RFC3339Nano, e.Time)
	end, err2 := time.Parse(time.RFC3339Nano, e.Tool.End)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0, false
	}
	return end.Sub(start), true
}

// DurationText is a duration as a tool line shows it: "0.4s", "12s", "2m 5s".
func DurationText(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	m := int(d / time.Minute)
	return strconv.Itoa(m) + "m " + strconv.Itoa(int((d-time.Duration(m)*time.Minute)/time.Second)) + "s"
}

// ToolName is the name a tool line shows: the server of an MCP tool ("contr" for
// mcp__plugin_contr_contr__send), else the tool's own name.
func ToolName(name string) string {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return name
	}
	server, _, _ := strings.Cut(rest, "__")
	if i := strings.LastIndexByte(server, '_'); i >= 0 && i < len(server)-1 {
		server = server[i+1:]
	}
	return server
}
