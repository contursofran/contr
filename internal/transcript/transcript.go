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
	// ID is the owner item of NoteAnswered and NoteDid.
	ID string
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
	case "attachment":
		if e, ok := attachment(base, l.Attachment); ok {
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
	case meta:
		e.Kind, e.Hidden = Hidden, HideMeta
	default:
		classify(&e)
		if e.Kind == Prompt {
			e.Images = images
		}
	}
	add(e)
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
func attachment(base Entry, raw json.RawMessage) (Entry, bool) {
	var a struct {
		Type      string          `json:"type"`
		HookEvent string          `json:"hookEvent"`
		Content   json.RawMessage `json:"content"`
		Stdout    string          `json:"stdout"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return Entry{}, false
	}
	e := base
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
	return e, true
}

// classify sets the kind of a user text that is not marked meta.
func classify(e *Entry) {
	s := strings.TrimSpace(e.Text)
	e.Kind = Prompt
	switch {
	case strings.HasPrefix(s, `<hc from="`):
		hcNote(e, s)
	case strings.HasPrefix(s, "Owner answered "), strings.HasPrefix(s, "Owner did "):
		e.Kind, e.Note, e.Text = Note, ownerNote(s), s
	case strings.HasPrefix(s, "<system-reminder>"):
		e.Kind, e.Hidden = Hidden, HideReminder
	case strings.HasPrefix(s, "<local-command-"), strings.HasPrefix(s, "<bash-stdout>"), strings.HasPrefix(s, "<bash-stderr>"):
		e.Kind, e.Hidden = Hidden, HideLocalCommand
	case isHookTag(s):
		e.Kind, e.Hidden = Hidden, HideHook
	case strings.HasPrefix(s, "[Request interrupted"):
		e.Kind, e.Hidden = Hidden, HideInterrupt
	case isPlaybook(s):
		e.Kind, e.Hidden = Hidden, HidePlaybook
	case strings.Contains(s, "<command-name>"):
		e.Text = command(s)
	}
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
