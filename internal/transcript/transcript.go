// Package transcript reads a Claude Code transcript (JSON lines) and gives
// the session as entries for the chat view (D242).
//
// Claude Code owns the format (D84), so the reader reads only the fields it
// needs: an unknown field or line type is no error, a line that is not JSON
// is skipped, and a line without its newline is left for the next read.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Kind is the kind of an entry.
type Kind int

// The entry kinds.
const (
	// Prompt is a text the owner typed. Text holds it; Images counts its images.
	Prompt Kind = iota
	// Said is a text of Claude. Text holds its Markdown source.
	Said
	// Thinking marks a thinking block. Its text is not kept.
	Thinking
	// Tool is a tool call. Tool holds it.
	Tool
	// Note is a text that hc or the contr kit typed into the session. Note
	// holds it.
	Note
	// Hidden is a text that is not part of the talk. Hidden gives why, and
	// Text holds the text when the line has one.
	Hidden
)

// Hide tells why an entry is Hidden.
type Hide int

// The reasons for a Hidden entry.
const (
	// HideMeta is a line that Claude Code marks isMeta.
	HideMeta Hide = iota + 1
	// HideReminder is a <system-reminder> text.
	HideReminder
	// HideHook is the output of a hook: additional context and the like.
	HideHook
	// HidePlaybook is the coordinator's playbook that the kit's SessionStart
	// hook gives.
	HidePlaybook
	// HideLocalCommand is the output of a local slash command.
	HideLocalCommand
	// HideCompactSummary is the summary that a compaction writes.
	HideCompactSummary
	// HideInterrupt is Claude Code's "[Request interrupted …]" mark.
	HideInterrupt
	// HideSystem is a system line of Claude Code (subtype in Text when there
	// is no content).
	HideSystem
	// HideHarness is another text that Claude Code adds to the session: a
	// stop hook's feedback, an image's source line, a nudge, or a text in
	// one tag of its own (D254).
	HideHarness
)

// NoteKind is the kind of a kit note.
type NoteKind int

// The note kinds.
const (
	// NoteMessage is a message that hc delivered: <hc from="ADDR NAME">…</hc>.
	NoteMessage NoteKind = iota
	// NoteAnswered is "Owner answered <ID>: <answer>".
	NoteAnswered
	// NoteDid is "Owner did <ID>: <text>".
	NoteDid
	// NoteTask is a background task's end: <task-notification>. Status
	// holds its status, ID its task id, and the entry's Text its summary
	// (D254).
	NoteTask
	// NoteAgent is a message of another Claude session, a teammate or a
	// subagent. Name holds the sender and the entry's Text the first line
	// (D254).
	NoteAgent
)

// Entry is one item of the session.
type Entry struct {
	Kind Kind
	// UUID and Time are the line's uuid and timestamp (RFC 3339 text as
	// Claude Code writes it), or "" when the line has none.
	UUID string
	Time string
	// Sidechain is set for a line of a subagent.
	Sidechain bool
	// Text is the prompt, the Markdown, the note body or the hidden text.
	Text string
	// Images is the number of images of a Prompt.
	Images int
	// Tool is set for a Tool entry.
	Tool *Call
	// Note is set for a Note entry.
	Note *KitNote
	// Hidden is set for a Hidden entry.
	Hidden Hide
}

// Call is a tool call and, once it came, what its result says.
type Call struct {
	ID   string
	Name string
	// Summary is one line about the input: the file path, the command, the
	// pattern or, for an MCP tool, the tool's own name.
	Summary string
	// Done is set when the result came.
	Done bool
	// Error is set when the result is an error.
	Error bool
	// Excerpt is the start of the result text, at most excerptMax runes
	// (200).
	Excerpt string
	// End is the timestamp of the result line ("" while not done or when
	// the line has none).
	End string
}

// KitNote is a text that hc or the contr kit typed into the session.
type KitNote struct {
	Kind NoteKind
	// Addr and Name are the sender of an hc message ("" when none).
	Addr string
	Name string
	// ID is the owner item of NoteAnswered and NoteDid, and the task id of
	// NoteTask.
	ID string
	// Status is the status of a NoteTask: "completed", "failed", "killed".
	Status string
}

// Result is a tool result for a call that an earlier read returned. Index
// is the call's place among all entries the Reader returned, from 0.
type Result struct {
	Index   int
	ID      string
	Error   bool
	Excerpt string
	// Time is the timestamp of the result line.
	Time string
}

// Batch is what one read gives.
type Batch struct {
	// Entries are the new entries. A result in the same read is already in
	// its call.
	Entries []Entry
	// Results are results for calls of earlier reads.
	Results []Result
	// Next is the offset to read from the next time.
	Next int64
}

// Reader reads a transcript in parts. It keeps the offset and the calls
// that wait for a result, so a result joins its call across reads. The
// zero value reads from the start.
type Reader struct {
	next    int64
	count   int            // entries returned so far
	pending map[string]int // call id -> entry index, for calls without a result
	// queued is the text of the last queued message read since the last text
	// of Claude or prompt, so that a second shape of it is no second prompt.
	queued string
}

// NewReader returns a Reader that starts at offset. Calls before the offset
// are not known, so their results are dropped.
func NewReader(offset int64) *Reader {
	return &Reader{next: offset}
}

// Offset is where the next read starts.
func (r *Reader) Offset() int64 { return r.next }

// ReadFile reads what was added to the file at path since the last read.
func (r *Reader) ReadFile(path string) (Batch, error) {
	f, err := os.Open(path)
	if err != nil {
		return Batch{Next: r.next}, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(r.next, io.SeekStart); err != nil {
		return Batch{Next: r.next}, err
	}
	return r.Read(f)
}

// Read reads src, which holds the transcript from Offset on. It consumes
// only whole lines: a last line without a newline stays for the next read.
// Entries read before an I/O error are returned with the error.
func (r *Reader) Read(src io.Reader) (Batch, error) {
	var b Batch
	idx := map[string]int{} // call id -> place in b.Entries
	br := bufio.NewReader(src)
	var err error
	for {
		var line []byte
		line, err = br.ReadBytes('\n')
		if err != nil {
			break // a part without '\n' is not consumed
		}
		r.next += int64(len(line))
		r.line(line, &b, idx)
	}
	b.Next = r.next
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return b, err
}

// line adds what one line gives to b.
func (r *Reader) line(raw []byte, b *Batch, idx map[string]int) {
	var l struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		UUID             string          `json:"uuid"`
		Timestamp        string          `json:"timestamp"`
		IsSidechain      bool            `json:"isSidechain"`
		IsMeta           bool            `json:"isMeta"`
		IsCompactSummary bool            `json:"isCompactSummary"`
		Content          json.RawMessage `json:"content"`
		Attachment       json.RawMessage `json:"attachment"`
		Message          struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &l) != nil {
		return
	}
	base := Entry{UUID: l.UUID, Time: l.Timestamp, Sidechain: l.IsSidechain}
	add := func(e Entry) {
		if e.Kind == Tool {
			idx[e.Tool.ID] = len(b.Entries)
			if r.pending == nil {
				r.pending = map[string]int{}
			}
			r.pending[e.Tool.ID] = r.count
		}
		b.Entries = append(b.Entries, e)
		r.count++
	}
	switch l.Type {
	case "user":
		r.user(base, l.IsMeta, l.IsCompactSummary, l.Message.Content, b, idx, add)
	case "assistant":
		for _, e := range assistant(base, l.Message.Content) {
			if e.Kind == Said {
				r.queued = ""
			}
			add(e)
		}
	case "system":
		e := base
		e.Kind, e.Hidden = Hidden, HideSystem
		if l.Subtype == "local_command" {
			e.Hidden = HideLocalCommand
		}
		e.Text = text(l.Content)
		if e.Text == "" {
			e.Text = l.Subtype
		}
		add(e)
	case "queue-operation":
		// Claude Code's queue (enqueue, dequeue, remove) is no delivery: a
		// queued message shows where the session got it (D265).
	case "attachment":
		if e, queued, ok := attachment(base, l.Attachment); ok {
			if queued {
				r.once(&e)
			}
			add(e)
		}
	}
}

// block is the part of a content block that the reader reads.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// blocks decodes content that is a list of blocks. A string gives one text
// block.
func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

func (r *Reader) user(base Entry, meta, compact bool, content json.RawMessage, b *Batch, idx map[string]int, add func(Entry)) {
	var texts []string
	images := 0
	for _, bl := range blocks(content) {
		switch bl.Type {
		case "text":
			texts = append(texts, bl.Text)
		case "image":
			images++
		case "tool_result":
			r.result(bl, base.Time, b, idx)
		}
	}
	if len(texts) == 0 && images == 0 {
		return
	}
	e := base
	e.Text = strings.Join(texts, "\n")
	switch {
	case compact:
		e.Kind, e.Hidden = Hidden, HideCompactSummary
	case queuedOK(e.Text):
		// The reminder of a message sent while the turn ran: read also on
		// a line marked meta.
		var n int
		e.Text, n, _ = queuedReminder(e.Text)
		classify(&e, n)
		r.once(&e)
	case meta:
		e.Kind, e.Hidden = Hidden, HideMeta
	default:
		classify(&e, images)
		if e.Kind == Prompt {
			r.queued = ""
		}
	}
	add(e)
}

// once reads e, a message that the owner sent while a turn ran: the first
// shape of it stays, a second shape of the same text is hidden (D265).
func (r *Reader) once(e *Entry) {
	if e.Kind == Hidden {
		return
	}
	key := strings.TrimSpace(e.Text)
	if key != "" && key == r.queued {
		e.Kind, e.Hidden, e.Note, e.Images = Hidden, HideHarness, nil, 0
		return
	}
	r.queued = key
}

// QueuedLead starts the text that tells Claude of a message the owner sent
// while it worked.
const QueuedLead = "The user sent a new message while you were working:"

// queuedTails start the harness's sentence after the message, each at the
// start of a line. A new Claude Code version may write another: add it here
// (D265).
var queuedTails = []string{
	"This is how Claude Code surfaces messages",
	"IMPORTANT: After completing your current task",
}

func queuedOK(s string) bool { _, _, ok := queuedReminder(s); return ok }

// queuedReminder reads the message of a text that starts with QueuedLead,
// alone, in a reminder or after other reminders, and tells whether s has
// that shape (D265). The message ends at the reminder's end or at a line
// that starts a tail of queuedTails. Its "[Image #N]" lines are dropped and
// counted in images.
func queuedReminder(s string) (msg string, images int, ok bool) {
	i := strings.Index(s, QueuedLead)
	if i < 0 || !harnessLead(s[:i]) {
		return "", 0, false
	}
	msg = s[i+len(QueuedLead):]
	msg, _, _ = strings.Cut(msg, "</system-reminder>")
	msg = msg[:QueuedTailAt(msg)]
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		if ImageMarker(l) {
			images++
			continue
		}
		keep = append(keep, l)
	}
	msg = strings.TrimSpace(strings.Join(keep, "\n"))
	return msg, images, msg != "" || images > 0
}

// QueuedTailAt gives the place of the first line of s that starts a tail of
// queuedTails, or len(s) when none does.
func QueuedTailAt(s string) int {
	end := len(s)
	for _, t := range queuedTails {
		for off := 0; ; {
			j := strings.Index(s[off:], t)
			if j < 0 {
				break
			}
			j += off
			if j == 0 || s[j-1] == '\n' {
				end = min(end, j)
				break
			}
			off = j + 1
		}
	}
	return end
}

// ImageMarker tells a line that is only Claude Code's "[Image #N]" mark of
// a pasted image.
func ImageMarker(l string) bool {
	l = strings.TrimSpace(l)
	n, ok := strings.CutPrefix(l, "[Image #")
	n, ok2 := strings.CutSuffix(n, "]")
	if !ok || !ok2 || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// result joins a tool result to its call: in this batch when the call is
// in it, else as a Result for an earlier read. A result of an unknown call
// is dropped.
func (r *Reader) result(bl block, at string, b *Batch, idx map[string]int) {
	n, ok := r.pending[bl.ToolUseID]
	if !ok {
		return
	}
	delete(r.pending, bl.ToolUseID)
	excerpt := clip(text(bl.Content), excerptMax)
	if i, ok := idx[bl.ToolUseID]; ok {
		c := *b.Entries[i].Tool
		c.Done, c.Error, c.Excerpt, c.End = true, bl.IsError, excerpt, at
		b.Entries[i].Tool = &c
		return
	}
	b.Results = append(b.Results, Result{Index: n, ID: bl.ToolUseID, Error: bl.IsError, Excerpt: excerpt, Time: at})
}

func assistant(base Entry, content json.RawMessage) []Entry {
	var out []Entry
	for _, bl := range blocks(content) {
		e := base
		switch bl.Type {
		case "text":
			if strings.TrimSpace(bl.Text) == "" {
				continue
			}
			e.Kind, e.Text = Said, bl.Text
		case "thinking", "redacted_thinking":
			e.Kind = Thinking
		case "tool_use", "server_tool_use":
			e.Kind = Tool
			e.Tool = &Call{ID: bl.ID, Name: bl.Name, Summary: summary(bl.Name, bl.Input)}
		default:
			continue
		}
		out = append(out, e)
	}
	return out
}

// attachment reads a line of type "attachment": Claude Code writes hook
// output this way. Its shape is not documented, so any text in it is taken.
// A "queued_command" is a message that the owner sent while a turn ran and
// that Claude Code gave to Claude in the turn: it is read as a user text at
// its place (queued is set, D265).
func attachment(base Entry, raw json.RawMessage) (e Entry, queued, ok bool) {
	var a struct {
		Type      string          `json:"type"`
		HookEvent string          `json:"hookEvent"`
		Content   json.RawMessage `json:"content"`
		Stdout    string          `json:"stdout"`
		Prompt    json.RawMessage `json:"prompt"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return Entry{}, false, false
	}
	e = base
	if a.Type == "queued_command" {
		var texts []string
		images := 0
		for _, bl := range blocks(a.Prompt) {
			switch bl.Type {
			case "text":
				texts = append(texts, bl.Text)
			case "image":
				images++
			}
		}
		if len(texts) > 0 || images > 0 {
			e.Text = strings.Join(texts, "\n")
			classify(&e, images)
			return e, true, true
		}
	}
	e.Kind, e.Hidden = Hidden, HideHook
	e.Text = text(a.Content)
	if e.Text == "" {
		e.Text = a.Stdout
	}
	if !strings.HasPrefix(a.Type, "hook") {
		e.Hidden = HideMeta
	}
	if isPlaybook(e.Text) {
		e.Hidden = HidePlaybook
	}
	return e, false, true
}

// classify sets the kind of a user text that is not marked meta. images
// is the count of image blocks of the line.
func classify(e *Entry, images int) {
	s := strings.TrimSpace(e.Text)
	e.Kind = Prompt
	switch {
	case strings.HasPrefix(s, `<hc from="`):
		hcNote(e, s)
		return
	case strings.HasPrefix(s, "Owner answered "), strings.HasPrefix(s, "Owner did "):
		e.Kind, e.Note, e.Text = Note, ownerNote(s), s
		return
	case taskNote(e, s), agentNote(e, s):
		return
	}
	// A prompt can carry a reminder or a hook's context: the prompt is what
	// is left without them.
	body, why := stripHarness(s)
	if body == "" {
		e.Kind, e.Hidden = Hidden, why
		return
	}
	why = 0
	switch {
	case strings.HasPrefix(body, "<local-command-"), strings.HasPrefix(body, "<bash-stdout>"), strings.HasPrefix(body, "<bash-stderr>"):
		why = HideLocalCommand
	case strings.HasPrefix(body, "[Request interrupted"):
		why = HideInterrupt
	case isPlaybook(body):
		why = HidePlaybook
	case strings.HasPrefix(body, "Stop hook feedback:"):
		why = HideHook
	case strings.HasPrefix(body, "The user hasn't heard from you"), strings.HasPrefix(body, "The user hasn’t heard from you"):
		why = HideHarness
	case strings.Contains(body, "<command-name>"):
		e.Text = command(body)
	case oneTag(body):
		why = HideHarness
	default:
		e.Text = dropImageSources(body)
		if e.Text == "" && images == 0 {
			why = HideHarness
		}
	}
	if why != 0 {
		e.Kind, e.Hidden, e.Text = Hidden, why, s
		return
	}
	e.Images = images
}

// stripHarness removes the <system-reminder> blocks and hook tags of s, and
// gives what is left, trimmed, and why the first removed part is hidden.
// An open tag without its close tag runs to the end.
func stripHarness(s string) (string, Hide) {
	var why Hide
	for {
		i, tag, w := -1, "", Hide(0)
		if j := strings.Index(s, "<system-reminder>"); j >= 0 {
			i, tag, w = j, "system-reminder", HideReminder
		}
		if j, t := hookTagAt(s); j >= 0 && (i < 0 || j < i) {
			i, tag, w = j, t, HideHook
		}
		if i < 0 {
			return strings.TrimSpace(s), why
		}
		if why == 0 {
			why = w
		}
		end := len(s)
		if k := strings.Index(s[i:], "</"+tag+">"); k >= 0 {
			end = i + k + len("</"+tag+">")
		}
		s = s[:i] + s[end:]
	}
}

// hookTagAt finds the first hook tag in s, as <user-prompt-submit-hook>,
// and gives its place and name, or -1.
func hookTagAt(s string) (int, string) {
	off := 0
	for {
		i := strings.IndexByte(s[off:], '<')
		if i < 0 {
			return -1, ""
		}
		i += off
		if isHookTag(s[i:]) {
			tag, _, _ := strings.Cut(s[i+1:], ">")
			return i, tag
		}
		off = i + 1
	}
}

// oneTag tells a text that is all inside one <tag …>…</tag> pair, where the
// tag name is lowercase letters and dashes: a harness text, not a prompt.
func oneTag(s string) bool {
	if !strings.HasPrefix(s, "<") {
		return false
	}
	n := 1
	for n < len(s) && (s[n] >= 'a' && s[n] <= 'z' || s[n] == '-') {
		n++
	}
	if n == 1 || n == len(s) || (s[n] != '>' && s[n] != ' ') {
		return false
	}
	tag := s[1:n]
	return strings.HasSuffix(s, "</"+tag+">") && strings.Count(s, "<"+tag) == 1
}

// dropImageSources removes Claude Code's "[Image: source: …]" lines of a
// pasted image: the image counts in Images.
func dropImageSources(s string) string {
	if !strings.Contains(s, "[Image: source:") {
		return s
	}
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "[Image: source:") {
			keep = append(keep, l)
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// harnessLead tells that the text before a tag is only the harness's lead:
// nothing, a <system-reminder>, a "[SYSTEM NOTIFICATION …]" mark or the
// line of a message of another session.
func harnessLead(lead string) bool {
	lead = strings.TrimSpace(lead)
	return lead == "" || strings.HasPrefix(lead, "<system-reminder>") ||
		strings.HasPrefix(lead, "[SYSTEM NOTIFICATION") ||
		strings.HasPrefix(lead, "Another Claude session sent a message:")
}

// taskNote reads <task-notification>: the end of a background task, with
// its task-id, status and summary. It tells whether s has that shape.
func taskNote(e *Entry, s string) bool {
	i := strings.Index(s, "<task-notification>")
	if i < 0 || !harnessLead(s[:i]) {
		return false
	}
	in := s[i:]
	n := &KitNote{Kind: NoteTask, ID: between(in, "<task-id>", "</task-id>"), Status: between(in, "<status>", "</status>")}
	e.Kind, e.Note, e.Text = Note, n, cut(between(in, "<summary>", "</summary>"), summaryMax)
	return true
}

// agentTags are the tags of a message of another session or a teammate.
var agentTags = []string{"agent-message", "cross-session-message", "teammate-message"}

// agentNote reads a message of another Claude session or a teammate
// (<agent-message from="…">, <cross-session-message from="…">,
// <teammate-message …>), and a subagent's "[Subagent hand-back] …". It
// tells whether s has one of these shapes.
func agentNote(e *Entry, s string) bool {
	if strings.HasPrefix(s, "[Subagent hand-back]") {
		e.Kind, e.Note, e.Text = Note, &KitNote{Kind: NoteAgent, Name: "subagent"}, "reported"
		return true
	}
	for _, tag := range agentTags {
		i := strings.Index(s, "<"+tag)
		if i < 0 || !harnessLead(s[:i]) {
			continue
		}
		open, rest, ok := strings.Cut(s[i+1+len(tag):], ">")
		if !ok || (open != "" && open[0] != ' ') {
			continue
		}
		body, _, _ := strings.Cut(rest, "</"+tag+">")
		name := attr(open, "from")
		if name == "" {
			name = attr(open, "teammate_id")
		}
		e.Kind, e.Note, e.Text = Note, &KitNote{Kind: NoteAgent, Name: name}, cut(body, summaryMax)
		return true
	}
	if rest, ok := strings.CutPrefix(s, "Another Claude session sent a message:"); ok {
		e.Kind, e.Note, e.Text = Note, &KitNote{Kind: NoteAgent}, cut(rest, summaryMax)
		return true
	}
	return false
}

// attr gives the value of the attribute name="…" in the open tag's text.
func attr(open, name string) string {
	_, rest, ok := strings.Cut(" "+open, " "+name+`="`)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, `"`)
	return v
}

// hcNote reads <hc from="ADDR NAME">TEXT</hc>. A body that is an owner
// answer or done mark gives that kind, with the hc sender kept.
func hcNote(e *Entry, s string) {
	rest := s[len(`<hc from="`):]
	q := strings.IndexByte(rest, '"')
	if q < 0 || !strings.HasPrefix(rest[q+1:], ">") {
		return // not the hc shape: keep it a prompt
	}
	from := rest[:q]
	body := strings.TrimSuffix(strings.TrimSpace(rest[q+2:]), "</hc>")
	body = strings.TrimSpace(body)
	n := &KitNote{Kind: NoteMessage}
	if strings.HasPrefix(body, "Owner answered ") || strings.HasPrefix(body, "Owner did ") {
		n = ownerNote(body)
	}
	n.Addr, n.Name, _ = strings.Cut(from, " ")
	e.Kind, e.Note, e.Text = Note, n, body
}

// ownerNote reads "Owner answered ID: …" or "Owner did ID: …".
func ownerNote(s string) *KitNote {
	n := &KitNote{Kind: NoteAnswered}
	rest, ok := strings.CutPrefix(s, "Owner answered ")
	if !ok {
		n.Kind = NoteDid
		rest = strings.TrimPrefix(s, "Owner did ")
	}
	if id, _, ok := strings.Cut(rest, ":"); ok && !strings.ContainsAny(id, " \n") {
		n.ID = id
	}
	return n
}

// isHookTag tells a text that Claude Code wraps in a hook's tag, as
// <user-prompt-submit-hook>…</user-prompt-submit-hook>: hook output that came
// as a user line, not as an attachment.
func isHookTag(s string) bool {
	if !strings.HasPrefix(s, "<") {
		return false
	}
	tag, _, ok := strings.Cut(s[1:], ">")
	return ok && strings.HasSuffix(tag, "-hook") && !strings.ContainsAny(tag, " /")
}

// isPlaybook tells a text of the kit's SessionStart hook (cmd/contr/hook.go:
// startupText and restoreText).
func isPlaybook(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "contr: you are the coordinator") ||
		strings.HasPrefix(s, "contr: the conversation was compacted")
}

// command turns Claude Code's record of a slash command into what the owner
// typed: "/name args".
func command(s string) string {
	name := between(s, "<command-name>", "</command-name>")
	if name == "" {
		return s
	}
	if args := strings.TrimSpace(between(s, "<command-args>", "</command-args>")); args != "" {
		return name + " " + args
	}
	return name
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	in, _, _ := strings.Cut(rest, close)
	return strings.TrimSpace(in)
}

// summaryMax and excerptMax are the most runes of a summary and an excerpt.
const (
	summaryMax = 200
	excerptMax = 200
)

// summary gives one line about a tool's input.
func summary(name string, raw json.RawMessage) string {
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if _, tool, ok := strings.Cut(rest, "__"); ok {
			return tool
		}
		return rest
	}
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"file_path", "notebook_path", "command", "pattern", "path", "url", "query", "description", "prompt", "skill"} {
		if v, ok := in[k].(string); ok && strings.TrimSpace(v) != "" {
			return cut(v, summaryMax)
		}
	}
	return ""
}

// text gives the text of content that is a string, a list of strings or a
// list of text blocks.
func text(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil {
		return strings.Join(ss, "\n")
	}
	var parts []string
	for _, bl := range blocks(raw) {
		if bl.Type == "text" {
			parts = append(parts, bl.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// clip gives s, trimmed, at most max runes, with "…" when cut. Lines are
// kept.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// cut gives the first line of s, at most max runes, with "…" when cut.
func cut(s string, max int) string {
	s = strings.TrimSpace(s)
	line, _, more := strings.Cut(s, "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > max {
		return clip(line, max)
	}
	if more {
		return line + " …"
	}
	return line
}
