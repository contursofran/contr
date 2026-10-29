package board

// The actions that contr sends to the coordinator mod, and the queue that
// holds them. An answer, done or dismiss waits UndoMS before it is written,
// so the owner can undo it. The other actions are written at once. The mod
// takes each action once, by seq, and reports the highest seq it took as
// acted in the board file. Pure: no file reads or writes and no UI. The file
// writes are in the outbox entity. Ported from the Rust contr module
// board_actions.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"unicode/utf8"
)

// ActionsVersion is the actions-file version this contr writes. The mod reads
// only this version.
const ActionsVersion uint32 = 1

// UndoMS is how long an answer, done or dismiss waits before it is written.
// Undo works in this window.
const UndoMS uint64 = 5_000

// ActionsPath is the actions file for a session: <dir>/<session>.actions.json.
func ActionsPath(dir, session string) string {
	return filepath.Join(dir, session+".actions.json")
}

// ActionKind names the kind of an action. It is the "do" field of the file.
type ActionKind string

// The action kinds, with their wire names.
const (
	// KindAnswer picks option Option of decision Key. Label is that option's
	// text, which the mod checks.
	KindAnswer  ActionKind = "answer"
	KindDone    ActionKind = "done"
	KindDismiss ActionKind = "dismiss"
	// KindChat puts "Key:" in the coordinator's prompt. Nothing is submitted.
	KindChat       ActionKind = "chat"
	KindClearRows  ActionKind = "clear_rows"
	KindClearItems ActionKind = "clear_items"
	KindWrapUp     ActionKind = "wrap_up"
)

// Action is one action. Kind names the kind. The other fields depend on the
// kind: Key for answer, done, dismiss and chat; Option and Label for answer;
// On for wrap_up. A field that the kind does not use stays zero.
type Action struct {
	Kind   ActionKind
	Key    string
	Option uint64
	Label  string
	On     bool
}

// Answer builds an answer action.
func Answer(key string, option uint64, label string) Action {
	return Action{Kind: KindAnswer, Key: key, Option: option, Label: label}
}

// Done builds a done action.
func Done(key string) Action { return Action{Kind: KindDone, Key: key} }

// Dismiss builds a dismiss action.
func Dismiss(key string) Action { return Action{Kind: KindDismiss, Key: key} }

// Chat builds a chat action.
func Chat(key string) Action { return Action{Kind: KindChat, Key: key} }

// ClearRows builds a clear_rows action.
func ClearRows() Action { return Action{Kind: KindClearRows} }

// ClearItems builds a clear_items action.
func ClearItems() Action { return Action{Kind: KindClearItems} }

// WrapUp builds a wrap_up action.
func WrapUp(on bool) Action { return Action{Kind: KindWrapUp, On: on} }

// ItemKey is the item that this action names. ok is false for the section
// actions.
func (a Action) ItemKey() (key string, ok bool) {
	switch a.Kind {
	case KindAnswer, KindDone, KindDismiss, KindChat:
		return a.Key, true
	}
	return "", false
}

// Holds is true for an answer, done or dismiss. These wait the undo window
// before they are written.
func (a Action) Holds() bool {
	switch a.Kind {
	case KindAnswer, KindDone, KindDismiss:
		return true
	}
	return false
}

// AnswerText is the answer that the item shows: the label, "done" or
// "dismissed". ok is false for other actions.
func (a Action) AnswerText() (text string, ok bool) {
	switch a.Kind {
	case KindAnswer:
		return a.Label, true
	case KindDone:
		return "done", true
	case KindDismiss:
		return "dismissed", true
	}
	return "", false
}

// Envelope is one action with its sequence number and its time. Seq grows by
// one for each action in a session.
type Envelope struct {
	Seq uint64
	// At is wall-clock milliseconds since the Unix epoch.
	At     uint64
	Action Action
}

// ActionsFile is the actions file. Field names are the schema's.
type ActionsFile struct {
	Version uint32
	Session string
	Actions []Envelope
}

// JSON writes the action as compact JSON, in the field order of the schema
// and with serde_json's string escaping. Go's encoder escapes more than
// serde_json does, so the file is written by hand to keep the same bytes.
func (a Action) JSON() []byte { return a.appendJSON(nil) }

func (a Action) appendJSON(b []byte) []byte {
	b = append(b, `{"do":`...)
	b = appendString(b, string(a.Kind))
	switch a.Kind {
	case KindAnswer:
		b = append(b, `,"key":`...)
		b = appendString(b, a.Key)
		b = append(b, `,"option":`...)
		b = strconv.AppendUint(b, a.Option, 10)
		b = append(b, `,"label":`...)
		b = appendString(b, a.Label)
	case KindDone, KindDismiss, KindChat:
		b = append(b, `,"key":`...)
		b = appendString(b, a.Key)
	case KindWrapUp:
		b = append(b, `,"on":`...)
		b = strconv.AppendBool(b, a.On)
	}
	return append(b, '}')
}

// JSON writes the envelope: seq and at, then the action fields.
func (e Envelope) JSON() []byte { return e.appendJSON(nil) }

func (e Envelope) appendJSON(b []byte) []byte {
	b = append(b, `{"seq":`...)
	b = strconv.AppendUint(b, e.Seq, 10)
	b = append(b, `,"at":`...)
	b = strconv.AppendUint(b, e.At, 10)
	b = append(b, ',')
	inner := e.Action.appendJSON(nil)
	return append(b, inner[1:]...)
}

// JSON writes the actions file. The bytes are what the mod reads.
func (f ActionsFile) JSON() []byte {
	b := append([]byte(nil), `{"version":`...)
	b = strconv.AppendUint(b, uint64(f.Version), 10)
	b = append(b, `,"session":`...)
	b = appendString(b, f.Session)
	b = append(b, `,"actions":[`...)
	for i, e := range f.Actions {
		if i > 0 {
			b = append(b, ',')
		}
		b = e.appendJSON(b)
	}
	return append(b, `]}`...)
}

// MarshalJSON writes the action like JSON. json.Marshal escapes <, > and &
// in the result, which is the same value but other bytes; use JSON for the
// file.
func (a Action) MarshalJSON() ([]byte, error) { return a.JSON(), nil }

// MarshalJSON writes the envelope like JSON.
func (e Envelope) MarshalJSON() ([]byte, error) { return e.JSON(), nil }

// MarshalJSON writes the file like JSON.
func (f ActionsFile) MarshalJSON() ([]byte, error) { return f.JSON(), nil }

// appendString writes s as a JSON string the way serde_json does: escape
// quote, backslash and the control characters; write everything else as it
// is. Bytes that are not valid UTF-8 become U+FFFD.
func appendString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	for _, r := range s {
		switch {
		case r == '"':
			b = append(b, `\"`...)
		case r == '\\':
			b = append(b, `\\`...)
		case r == '\b':
			b = append(b, `\b`...)
		case r == '\f':
			b = append(b, `\f`...)
		case r == '\n':
			b = append(b, `\n`...)
		case r == '\r':
			b = append(b, `\r`...)
		case r == '\t':
			b = append(b, `\t`...)
		case r < 0x20:
			b = append(b, `\u00`...)
			b = append(b, hex[r>>4], hex[r&0xf])
		default:
			b = utf8.AppendRune(b, r)
		}
	}
	return append(b, '"')
}

// UnmarshalJSON reads an action. do is required and must be a known kind.
// The fields that the kind needs are required; others are ignored.
func (a *Action) UnmarshalJSON(data []byte) error {
	var raw struct {
		Do     *string `json:"do"`
		Key    *string `json:"key"`
		Option *uint64 `json:"option"`
		Label  *string `json:"label"`
		On     *bool   `json:"on"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Do == nil {
		return errors.New("missing field `do`")
	}
	kind := ActionKind(*raw.Do)
	out := Action{Kind: kind}
	needKey := func() error {
		if raw.Key == nil {
			return errors.New("missing field `key`")
		}
		out.Key = *raw.Key
		return nil
	}
	switch kind {
	case KindAnswer:
		if err := needKey(); err != nil {
			return err
		}
		if raw.Option == nil {
			return errors.New("missing field `option`")
		}
		if raw.Label == nil {
			return errors.New("missing field `label`")
		}
		out.Option, out.Label = *raw.Option, *raw.Label
	case KindDone, KindDismiss, KindChat:
		if err := needKey(); err != nil {
			return err
		}
	case KindClearRows, KindClearItems:
	case KindWrapUp:
		if raw.On == nil {
			return errors.New("missing field `on`")
		}
		out.On = *raw.On
	default:
		return fmt.Errorf("unknown variant `%s`", *raw.Do)
	}
	*a = out
	return nil
}

// UnmarshalJSON reads an envelope. seq and at are required.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	var raw struct {
		Seq *uint64 `json:"seq"`
		At  *uint64 `json:"at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Seq == nil {
		return errors.New("missing field `seq`")
	}
	if raw.At == nil {
		return errors.New("missing field `at`")
	}
	var act Action
	if err := act.UnmarshalJSON(data); err != nil {
		return err
	}
	*e = Envelope{Seq: *raw.Seq, At: *raw.At, Action: act}
	return nil
}

// UnmarshalJSON reads an actions file. version and session are required;
// actions defaults to empty.
func (f *ActionsFile) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version *uint32    `json:"version"`
		Session *string    `json:"session"`
		Actions []Envelope `json:"actions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Version == nil {
		return errors.New("missing field `version`")
	}
	if raw.Session == nil {
		return errors.New("missing field `session`")
	}
	*f = ActionsFile{Version: *raw.Version, Session: *raw.Session, Actions: raw.Actions}
	return nil
}

// ParseActions parses an actions file. ok is false for bad JSON, a missing
// version, or a version other than ActionsVersion.
func ParseActions(data []byte) (file ActionsFile, ok bool) {
	if err := json.Unmarshal(data, &file); err != nil {
		return ActionsFile{}, false
	}
	if file.Version != ActionsVersion {
		return ActionsFile{}, false
	}
	return file, true
}

// held is an action in the undo window, with its click time (ms) and the
// time (ms) at which it is written.
type held struct {
	action    Action
	at        uint64
	releaseAt uint64
}

// Outbox is what contr has queued for one session. A held action can still
// be undone. A held action has no seq yet: it gets one when it is written, so
// the seq order is the write order. A written action stays in the file until
// the mod takes it, which the board file reports as acted. Not safe for
// concurrent use.
type Outbox struct {
	session string
	nextSeq uint64
	// held is in push order.
	held []held
	// written is ascending by seq. A new write gets a seq above all of them,
	// so it goes at the end.
	written []Envelope
	// seenFailed: failures with a seq up to this one are not shown. It starts
	// at acted.
	seenFailed uint64
}

// isAnswerFor is true for an answer, done or dismiss for key.
func isAnswerFor(a Action, key string) bool {
	k, ok := a.ItemKey()
	if !ok || k != key {
		return false
	}
	_, ok = a.AnswerText()
	return ok
}

// NewOutbox makes a queue for session, after the mod took actions up to
// acted. The entries of existing with a seq above acted are kept as written.
// An existing file of another session is ignored. The next seq follows the
// highest seq that is known. Failures up to acted are not shown.
func NewOutbox(session string, acted uint64, existing *ActionsFile) *Outbox {
	highest := acted
	var written []Envelope
	if existing != nil && existing.Session == session {
		for _, env := range existing.Actions {
			highest = max(highest, env.Seq)
			if env.Seq > acted {
				written = append(written, env)
			}
		}
	}
	slices.SortStableFunc(written, func(a, b Envelope) int {
		switch {
		case a.Seq < b.Seq:
			return -1
		case a.Seq > b.Seq:
			return 1
		}
		return 0
	})
	return &Outbox{
		session:    session,
		nextSeq:    satAdd(highest, 1),
		written:    written,
		seenFailed: acted,
	}
}

func satAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// Session is the session that the queue belongs to.
func (q *Outbox) Session() string { return q.session }

// Push queues action at nowMS. An answer, done or dismiss is held until
// nowMS + UndoMS and returns ok false: it gets its seq when it is written.
// Any other action is written at once and returns its seq.
func (q *Outbox) Push(action Action, nowMS uint64) (seq uint64, ok bool) {
	if action.Holds() {
		q.held = append(q.held, held{action, nowMS, satAdd(nowMS, UndoMS)})
		return 0, false
	}
	seq = q.takeSeq()
	q.written = append(q.written, Envelope{Seq: seq, At: nowMS, Action: action})
	return seq, true
}

// Undo removes the held answer, done or dismiss for key, if it is the latest
// one for key. It returns true if one was removed. An action that was
// already written can not be undone.
func (q *Outbox) Undo(key string) bool {
	pos := -1
	for i := len(q.held) - 1; i >= 0; i-- {
		if isAnswerFor(q.held[i].action, key) {
			pos = i
			break
		}
	}
	if pos < 0 {
		return false
	}
	q.held = slices.Delete(q.held, pos, pos+1)
	return true
}

// Release writes the held actions whose time has come (releaseAt <= nowMS),
// in push order. Each one gets the next seq now. It returns true if any was
// written.
func (q *Outbox) Release(nowMS uint64) bool {
	var due, kept []held
	for _, h := range q.held {
		if h.releaseAt <= nowMS {
			due = append(due, h)
		} else {
			kept = append(kept, h)
		}
	}
	q.held = kept
	return q.writeHeld(due)
}

// ReleaseAll writes every held action at once, in push order, for quit. It
// returns true if any was written.
func (q *Outbox) ReleaseAll() bool {
	all := q.held
	q.held = nil
	return q.writeHeld(all)
}

// Ack drops the written actions that the mod has taken, which are those with
// seq <= acted. It returns true if any was dropped.
func (q *Outbox) Ack(acted uint64) bool {
	before := len(q.written)
	q.written = slices.DeleteFunc(q.written, func(e Envelope) bool { return e.Seq <= acted })
	return len(q.written) != before
}

// NewFailures returns the failures with a seq above the last one seen, in the
// order given. Each failure is returned once. A later call returns only the
// failures that are newer.
func (q *Outbox) NewFailures(failed []ActionFailure) []ActionFailure {
	var fresh []ActionFailure
	top := q.seenFailed
	for _, f := range failed {
		if f.Seq > q.seenFailed {
			fresh = append(fresh, f)
			top = max(top, f.Seq)
		}
	}
	q.seenFailed = top
	return fresh
}

// File is the actions file to write: the written actions, in seq order.
func (q *Outbox) File() ActionsFile {
	return ActionsFile{
		Version: ActionsVersion,
		Session: q.session,
		Actions: append([]Envelope{}, q.written...),
	}
}

// PendingAnswer is the answer text for key and whether it can still be
// undone. It is the latest answer, done or dismiss for key, held or written.
// A chat does not hide it. ok is false when there is none.
func (q *Outbox) PendingAnswer(key string) (text string, undoable bool, ok bool) {
	action, isHeld, found := q.latestAnswer(key)
	if !found {
		return "", false, false
	}
	text, ok = action.AnswerText()
	return text, isHeld, ok
}

// NextRelease is the earliest time (ms) at which a held action is written.
// ok is false when nothing is held.
func (q *Outbox) NextRelease() (ms uint64, ok bool) {
	for _, h := range q.held {
		if !ok || h.releaseAt < ms {
			ms, ok = h.releaseAt, true
		}
	}
	return ms, ok
}

// latestAnswer is the latest answer, done or dismiss, for key, with true when
// it is still held. A held answer is newer than a written one: a written
// action was pushed before every held action, because a later push is due
// later.
func (q *Outbox) latestAnswer(key string) (Action, bool, bool) {
	for i := len(q.held) - 1; i >= 0; i-- {
		if isAnswerFor(q.held[i].action, key) {
			return q.held[i].action, true, true
		}
	}
	var best *Envelope
	for i := range q.written {
		e := &q.written[i]
		if isAnswerFor(e.Action, key) && (best == nil || e.Seq >= best.Seq) {
			best = e
		}
	}
	if best == nil {
		return Action{}, false, false
	}
	return best.Action, false, true
}

// takeSeq is the next seq. Each call gives a larger seq than the one before.
func (q *Outbox) takeSeq() uint64 {
	seq := q.nextSeq
	q.nextSeq = satAdd(seq, 1)
	return seq
}

// writeHeld writes the due held actions in the order given. Each one gets the
// next seq. It returns true if due was not empty.
func (q *Outbox) writeHeld(due []held) bool {
	for _, h := range due {
		q.written = append(q.written, Envelope{Seq: q.takeSeq(), At: h.at, Action: h.action})
	}
	return len(due) > 0
}

// CardOption is one option of a decision card.
type CardOption struct {
	// Index is the option's index in the item's Options.
	Index int
	Text  string
	// Recommended is true for the recommended option.
	Recommended bool
}

// CardOptions are the options of a decision, in card order: the recommended
// one first, then the others in order. A Recommended that is out of range
// marks none.
func CardOptions(item NeedItem) []CardOption {
	rec := -1
	if item.Recommended != nil && *item.Recommended < uint(len(item.Options)) {
		rec = int(*item.Recommended)
	}
	out := make([]CardOption, 0, len(item.Options))
	if rec >= 0 {
		out = append(out, CardOption{rec, item.Options[rec], true})
	}
	for i, text := range item.Options {
		if i != rec {
			out = append(out, CardOption{i, text, false})
		}
	}
	return out
}

// NextOpen is the item after key in slot order that isOpen accepts. If there
// is none, it is the first other accepted item. If there is none of those
// either, it is nil. This is the mod's "next" rule.
func NextOpen(items []NeedItem, key string, isOpen func(NeedItem) bool) *NeedItem {
	after := 0
	for i := range items {
		if items[i].Key == key {
			after = i + 1
			break
		}
	}
	accepts := func(it *NeedItem) bool { return it.Key != key && isOpen(*it) }
	for i := after; i < len(items); i++ {
		if accepts(&items[i]) {
			return &items[i]
		}
	}
	for i := range items {
		if accepts(&items[i]) {
			return &items[i]
		}
	}
	return nil
}

// Ago says how long ago atMS was, for a card: "now" under a minute, then
// "5m", "2h" and "3d". A time in the future reads as "now".
func Ago(nowMS, atMS uint64) string {
	var secs uint64
	if nowMS > atMS {
		secs = (nowMS - atMS) / 1_000
	}
	switch {
	case secs < 60:
		return "now"
	case secs < 3_600:
		return strconv.FormatUint(secs/60, 10) + "m"
	case secs < 86_400:
		return strconv.FormatUint(secs/3_600, 10) + "h"
	}
	return strconv.FormatUint(secs/86_400, 10) + "d"
}
