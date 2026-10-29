// Package board holds the board schema (v1) that the coordinator mod writes:
// the bridge token that names the session, the board file it points to, and
// the status and label of each stage and needs-you item.
//
// Pure: no file reads and no UI. Ported from the Rust contr board_model
// module. The file format is the mod's; keep every field name, default and
// lenient-list rule exactly.
package board

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"unicode/utf8"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/herdr"
)

// Version is the schema version this contr reads. A file or token of another
// version is not read.
const Version uint32 = 1

// BridgeToken is the pane token that marks the coordinator's pane.
const BridgeToken = "contr_board"

// StageHeads are the five stage columns, in order: plan, build, review, CI,
// you.
var StageHeads = [5]string{"plan", "build", "review", "CI", "you"}

// BridgeRef is the session and revision that a bridge token names.
type BridgeRef struct {
	Session string
	Rev     uint64
}

// ParseBridgeToken parses the bridge token "1 <session> <rev>" (single
// spaces). It returns false for any other shape, another version, a session
// outside [A-Za-z0-9_-]{1,64}, or a revision that is not digits.
func ParseBridgeToken(value string) (BridgeRef, bool) {
	parts := splitSpaces(value)
	if len(parts) != 3 {
		return BridgeRef{}, false
	}
	version, ok := digits(parts[0])
	if !ok || version != uint64(Version) || !isSession(parts[1]) {
		return BridgeRef{}, false
	}
	rev, ok := digits(parts[2])
	if !ok {
		return BridgeRef{}, false
	}
	return BridgeRef{Session: parts[1], Rev: rev}, true
}

// splitSpaces splits on every single space, keeping empty parts.
func splitSpaces(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// digits reads a value made of ASCII digits only. A sign is not accepted, and
// a value that does not fit in 64 bits is not accepted.
func digits(value string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return n, err == nil
}

func isSession(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		ok := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
		if !ok {
			return false
		}
	}
	return true
}

// Path is the board file for a session: <dir>/<session>.json.
func Path(dir, session string) string {
	return filepath.Join(dir, session+".json")
}

// Snapshot is one board file. Field names are the schema's; unknown fields
// are ignored.
type Snapshot struct {
	Version uint32
	Session string
	Rev     uint64
	// Summary is the mod's exact summary line.
	Summary string
	// WrapUp true shows the header label "keep going"; false shows "wrap up".
	WrapUp bool
	// NeedCount is the open needs-you items: the "Needs you N" count.
	NeedCount uint32
	// ClearRows: the work section has finished or joined rows, so "clear
	// done" shows.
	ClearRows bool
	// ClearItems: the needs-you section has resolved items, so "clear done"
	// shows.
	ClearItems bool
	Context    *ContextUse
	// Rows are the work rows in the mod's slot order. contr never reorders
	// them.
	Rows []WorkRow
	// Items are the needs-you items in the mod's slot order. contr never
	// reorders them.
	Items []NeedItem
	// Agents is what the coordinator mod knows about each worker pane.
	// Missing, null or not a list is empty. A bad entry is dropped; the rest
	// of the board still reads.
	Agents []BoardAgent
	// Actions 1 means the mod takes actions-file v1. Absent or 0: contr's
	// action buttons stay inert.
	Actions uint32
	// Acted is the highest action seq the mod has taken, whether it worked
	// or failed.
	Acted uint64
	// Failed lists the last failed actions, ascending by seq. contr shows
	// each new one once. A bad entry is dropped; the rest of the board still
	// reads.
	Failed []ActionFailure
}

// UnmarshalJSON reads a board body. version, session and rev are required.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version    *uint32         `json:"version"`
		Session    *string         `json:"session"`
		Rev        *uint64         `json:"rev"`
		Summary    string          `json:"summary"`
		WrapUp     bool            `json:"wrap_up"`
		NeedCount  uint32          `json:"need_count"`
		ClearRows  bool            `json:"clear_rows"`
		ClearItems bool            `json:"clear_items"`
		Context    *ContextUse     `json:"context"`
		Rows       []WorkRow       `json:"rows"`
		Items      []NeedItem      `json:"items"`
		Agents     json.RawMessage `json:"agents"`
		Actions    uint32          `json:"actions"`
		Acted      uint64          `json:"acted"`
		Failed     json.RawMessage `json:"failed"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.Version == nil:
		return errors.New("missing field `version`")
	case raw.Session == nil:
		return errors.New("missing field `session`")
	case raw.Rev == nil:
		return errors.New("missing field `rev`")
	}
	*s = Snapshot{
		Version:    *raw.Version,
		Session:    *raw.Session,
		Rev:        *raw.Rev,
		Summary:    raw.Summary,
		WrapUp:     raw.WrapUp,
		NeedCount:  raw.NeedCount,
		ClearRows:  raw.ClearRows,
		ClearItems: raw.ClearItems,
		Context:    raw.Context,
		Rows:       raw.Rows,
		Items:      raw.Items,
		Agents:     lenientList[BoardAgent](raw.Agents),
		Actions:    raw.Actions,
		Acted:      raw.Acted,
		Failed:     lenientList[ActionFailure](raw.Failed),
	}
	return nil
}

// lenientList reads a list leniently, for agents and failed. A list keeps
// each entry that reads as a T and drops the others. Any other value (a
// string, a number, null) reads as no entries.
func lenientList[T any](raw json.RawMessage) []T {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil
	}
	var out []T
	for _, entry := range entries {
		var v T
		if err := json.Unmarshal(entry, &v); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// ActionFailure is one action that the mod could not take. Key is the item it
// named, if any.
type ActionFailure struct {
	Seq  uint64
	Key  *string
	Text string
}

// UnmarshalJSON reads a failure. seq is required.
func (f *ActionFailure) UnmarshalJSON(data []byte) error {
	var raw struct {
		Seq  *uint64 `json:"seq"`
		Key  *string `json:"key"`
		Text string  `json:"text"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Seq == nil {
		return errors.New("missing field `seq`")
	}
	*f = ActionFailure{Seq: *raw.Seq, Key: raw.Key, Text: raw.Text}
	return nil
}

// BoardAgent is one worker as the coordinator mod knows it. Blank or missing
// Name, Model and Role mean not known; the agents list then shows the herdr
// tab label and the agent kind.
type BoardAgent struct {
	// PaneID is the herdr pane id of the worker.
	PaneID string
	Name   *string
	Model  *string
	Role   *string
}

// UnmarshalJSON reads an agent. pane_id is required and must be a string.
func (a *BoardAgent) UnmarshalJSON(data []byte) error {
	var raw struct {
		PaneID *string `json:"pane_id"`
		Name   *string `json:"name"`
		Model  *string `json:"model"`
		Role   *string `json:"role"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.PaneID == nil {
		return errors.New("missing field `pane_id`")
	}
	*a = BoardAgent{PaneID: *raw.PaneID, Name: raw.Name, Model: raw.Model, Role: raw.Role}
	return nil
}

// AgentMeta gives the agents list's data per pane id. Name, Model and Role
// come from Agents (the first entry for a pane wins). Link is the Label of
// the first work row in slot order with that PaneID that is not Finished and
// has no Joined. A pane in neither gets no entry.
func AgentMeta(b *Snapshot) map[string]herdr.AgentMeta {
	m := map[string]herdr.AgentMeta{}
	for _, a := range b.Agents {
		if _, ok := m[a.PaneID]; !ok {
			m[a.PaneID] = herdr.AgentMeta{Name: a.Name, Model: a.Model, Role: a.Role}
		}
	}
	for i := range b.Rows {
		row := &b.Rows[i]
		if !row.live() || row.PaneID == nil {
			continue
		}
		meta := m[*row.PaneID]
		if meta.Link == nil {
			label := row.Label
			meta.Link = &label
		}
		m[*row.PaneID] = meta
	}
	return m
}

// PaneMark is what the board says about one worker pane, for its worktree
// row.
type PaneMark struct {
	// Label is the work row's label, such as "#640".
	Label string
	// Who is the work row's who.
	Who Who
	// OpenItem is the work row's item, when that item is in Items and open.
	OpenItem *string
	// Blocked: a stage of the work row is StageBlock.
	Blocked bool
}

// PaneMarks gives the mark of each worker pane that a work row holds. A row
// counts when it is not Finished and has no Joined; the first such row in
// slot order wins a pane, as in AgentMeta. A row without a PaneID gives no
// mark.
func PaneMarks(b *Snapshot) map[string]PaneMark {
	m := map[string]PaneMark{}
	for i := range b.Rows {
		row := &b.Rows[i]
		if !row.live() || row.PaneID == nil {
			continue
		}
		if _, ok := m[*row.PaneID]; ok {
			continue
		}
		mark := PaneMark{Label: row.Label, Who: row.Who}
		if row.Item != nil {
			for _, it := range b.Items {
				if it.Key == *row.Item && it.Open {
					key := *row.Item
					mark.OpenItem = &key
					break
				}
			}
		}
		for _, s := range row.Stages {
			if s == StageBlock {
				mark.Blocked = true
				break
			}
		}
		m[*row.PaneID] = mark
	}
	return m
}

// ContextUse is the context use of the coordinator. The panel does not draw
// it in #9.
type ContextUse struct {
	Tokens uint64
	Limit  uint64
}

// UnmarshalJSON reads the context use. tokens and limit are required.
func (c *ContextUse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Tokens *uint64 `json:"tokens"`
		Limit  *uint64 `json:"limit"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Tokens == nil || raw.Limit == nil {
		return errors.New("missing field `tokens` or `limit`")
	}
	*c = ContextUse{Tokens: *raw.Tokens, Limit: *raw.Limit}
	return nil
}

// WorkRow is one work row: an issue or a pull request, with five stages.
type WorkRow struct {
	// Key is the stable slot key.
	Key   string
	Label string
	Title string
	// Stages are plan, build, review, CI, you. A missing stage reads as
	// StageNone; see Stage.
	Stages []Stage
	Who    Who
	// Joined is the task this slot joined, drawn as "joined #641" and struck
	// through.
	Joined *string
	// Finished: nothing runs or waits on this row: drawn dim.
	Finished bool
	// Now is the sentence for the answer card.
	Now *string
	// Item is the open needs-you item this row waits on.
	Item *string
	// PaneID is the worker pane that "Focus tab" moves to.
	PaneID *string
	// URL is the pull request that "Open PR" opens.
	URL *string
	// Report is the absolute path of the worker's RESULT.md.
	Report *string
	// ReportBy is the worker that wrote the report.
	ReportBy *string
}

// workRowFields is the wire shape of a WorkRow, with key optional so that a
// missing key can be reported.
type workRowFields struct {
	Key      *string `json:"key"`
	Label    string  `json:"label"`
	Title    string  `json:"title"`
	Stages   []Stage `json:"stages"`
	Who      Who     `json:"who"`
	Joined   *string `json:"joined"`
	Finished bool    `json:"finished"`
	Now      *string `json:"now"`
	Item     *string `json:"item"`
	PaneID   *string `json:"pane_id"`
	URL      *string `json:"url"`
	Report   *string `json:"report"`
	ReportBy *string `json:"report_by"`
}

// UnmarshalJSON reads a work row. key is required.
func (r *WorkRow) UnmarshalJSON(data []byte) error {
	var raw workRowFields
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Key == nil {
		return errors.New("missing field `key`")
	}
	*r = WorkRow{
		Key: *raw.Key, Label: raw.Label, Title: raw.Title, Stages: raw.Stages,
		Who: raw.Who, Joined: raw.Joined, Finished: raw.Finished, Now: raw.Now,
		Item: raw.Item, PaneID: raw.PaneID, URL: raw.URL, Report: raw.Report,
		ReportBy: raw.ReportBy,
	}
	return nil
}

// live reports whether the row is not finished and has no joined task.
func (r *WorkRow) live() bool { return !r.Finished && r.Joined == nil }

// Stage gives the stage at index: 0 is plan and 4 is you. A missing stage
// reads as StageNone.
func (r *WorkRow) Stage(index int) Stage {
	if index < 0 || index >= len(r.Stages) {
		return StageNone
	}
	return r.Stages[index]
}

// Stage is one stage cell. Any other string is StageUnknown, which draws as
// not started.
type Stage int

// The stage values.
const (
	StageDone Stage = iota
	StageRun
	StageWait
	StageNone
	StageAsk
	StageTodo
	StageBlock
	StageUnknown
)

var stageNames = map[string]Stage{
	"done": StageDone, "run": StageRun, "wait": StageWait, "none": StageNone,
	"ask": StageAsk, "todo": StageTodo, "block": StageBlock,
}

// UnmarshalJSON reads a stage string. A string that is not a stage is
// StageUnknown; a value that is not a string is an error.
func (s *Stage) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	if v, ok := stageNames[name]; ok {
		*s = v
	} else {
		*s = StageUnknown
	}
	return nil
}

// Who is the who text of a work row and whose it is. A missing who is an
// empty text.
type Who struct {
	Text string  `json:"text"`
	Kind WhoKind `json:"kind"`
}

// WhoKind says whose a who text is about. WhoYou is the owner's turn. Any
// other string is WhoOther.
type WhoKind int

// The who kinds. The zero value is WhoText, the default.
const (
	WhoText WhoKind = iota
	WhoYou
	WhoDim
	WhoOther
)

// UnmarshalJSON reads a who kind string.
func (k *WhoKind) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	switch name {
	case "you":
		*k = WhoYou
	case "dim":
		*k = WhoDim
	case "text":
		*k = WhoText
	default:
		*k = WhoOther
	}
	return nil
}

// NeedItem is one needs-you item: a decision, a to-do or news.
type NeedItem struct {
	// Key is the stable slot key, such as "D7" or "T12".
	Key   string
	Kind  ItemKind
	Title string
	// Open false: resolved, struck through until "clear done". A missing open
	// is true.
	Open bool
	// Task is the task the item belongs to.
	Task *string
	// Text is the full question.
	Text *string
	// From is the pane that asked.
	From *string
	// At is when the item was raised, in Unix milliseconds.
	At *uint64
	// Options are the option lines, in order.
	Options []string
	// Recommended is the index into Options that is recommended, drawn as the
	// filled button.
	Recommended *uint
	// Link is a to-do's link.
	Link *string
	// Answer is how a resolved item ended, in one line.
	Answer *string
}

// UnmarshalJSON reads an item. key and kind are required; open defaults to
// true.
func (n *NeedItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Key         *string   `json:"key"`
		Kind        *ItemKind `json:"kind"`
		Title       string    `json:"title"`
		Open        *bool     `json:"open"`
		Task        *string   `json:"task"`
		Text        *string   `json:"text"`
		From        *string   `json:"from"`
		At          *uint64   `json:"at"`
		Options     []string  `json:"options"`
		Recommended *uint     `json:"recommended"`
		Link        *string   `json:"link"`
		Answer      *string   `json:"answer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Key == nil {
		return errors.New("missing field `key`")
	}
	if raw.Kind == nil {
		return errors.New("missing field `kind`")
	}
	open := true
	if raw.Open != nil {
		open = *raw.Open
	}
	*n = NeedItem{
		Key: *raw.Key, Kind: *raw.Kind, Title: raw.Title, Open: open,
		Task: raw.Task, Text: raw.Text, From: raw.From, At: raw.At,
		Options: raw.Options, Recommended: raw.Recommended, Link: raw.Link,
		Answer: raw.Answer,
	}
	return nil
}

// ItemKind: ask is a decision, do a to-do, know news. Any other string is
// ItemOther, which reads as news.
type ItemKind int

// The item kinds.
const (
	ItemAsk ItemKind = iota
	ItemDo
	ItemKnow
	ItemOther
)

// UnmarshalJSON reads an item kind string.
func (k *ItemKind) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	switch name {
	case "ask":
		*k = ItemAsk
	case "do":
		*k = ItemDo
	case "know":
		*k = ItemKnow
	default:
		*k = ItemOther
	}
	return nil
}

// ErrorKind says why a board file was not read.
type ErrorKind int

const (
	// ErrVersion: the file's version is not Version.
	ErrVersion ErrorKind = iota
	// ErrJSON: not JSON, not UTF-8, or missing version or a required field.
	ErrJSON
)

// Error is why a board file was not read. It is comparable.
type Error struct {
	Kind ErrorKind
	// Version is the file's version, for ErrVersion.
	Version uint32
	// Msg is the decoder's message, for ErrJSON.
	Msg string
}

// Error returns Line.
func (e Error) Error() string { return e.Line() }

// Line is the one-line message for the panel.
func (e Error) Line() string {
	if e.Kind == ErrVersion {
		return fmt.Sprintf("board v%d · this contr reads v%d", e.Version, Version)
	}
	msg := []rune(e.Msg)
	if len(msg) > 60 {
		msg = msg[:60]
	}
	return "board unreadable: " + string(msg)
}

// Parse parses a board file. The version is checked before the rest of the
// body, so a board of another version is an ErrVersion error whatever its
// body. Errors are of type Error.
func Parse(data []byte) (*Snapshot, error) {
	if !utf8.Valid(data) {
		return nil, Error{Kind: ErrJSON, Msg: "invalid UTF-8"}
	}
	var probe struct {
		Version *uint32 `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, Error{Kind: ErrJSON, Msg: err.Error()}
	}
	if probe.Version == nil {
		return nil, Error{Kind: ErrJSON, Msg: "missing field `version`"}
	}
	if *probe.Version != Version {
		return nil, Error{Kind: ErrVersion, Version: *probe.Version}
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, Error{Kind: ErrJSON, Msg: err.Error()}
	}
	return &s, nil
}

// StageStatus is the icon for a stage cell.
func StageStatus(s Stage) icons.Status {
	switch s {
	case StageDone:
		return icons.Done
	case StageRun:
		return icons.Working
	case StageWait:
		return icons.Waiting
	case StageAsk:
		return icons.Decide
	case StageTodo:
		return icons.Do
	case StageBlock:
		return icons.Blocked
	default:
		return icons.NotStarted
	}
}

// ItemStatus is the icon for a needs-you item. A resolved item is Done
// whatever its kind.
func ItemStatus(it NeedItem) icons.Status {
	if !it.Open {
		return icons.Done
	}
	switch it.Kind {
	case ItemAsk:
		return icons.Decide
	case ItemDo:
		return icons.Do
	default:
		return icons.Fyi
	}
}

// ItemKindLabel is the kind word for a needs-you item. A resolved item reads
// "done" whatever its kind.
func ItemKindLabel(it NeedItem) string {
	if !it.Open {
		return "done"
	}
	switch it.Kind {
	case ItemAsk:
		return "decide"
	case ItemDo:
		return "do"
	default:
		return "fyi"
	}
}
