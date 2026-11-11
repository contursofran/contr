package chatview

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/transcript"
)

// Lines of a message sent while a turn ran (D265), in the shapes the reader knows. None comes
// from a real transcript.
const (
	lEnqueue  = `{"type":"queue-operation","operation":"enqueue","content":"add auto compact later"}`
	lQueued   = `{"type":"attachment","uuid":"q1","attachment":{"type":"queued_command","prompt":"add auto compact later"}}`
	lReminder = `{"type":"user","uuid":"q2","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"text","text":"<system-reminder>\nThe user sent a new message while you were working:\nadd auto compact later\n</system-reminder>"}]}}`
)

func promptLine(uuid, at, text string) string {
	ts := ""
	if at != "" {
		ts = `,"timestamp":"` + at + `"`
	}
	return `{"type":"user","uuid":"` + uuid + `"` + ts + `,"message":{"content":"` + text + `"}}`
}

// promptTexts are the texts of the prompt rows, a sent one (no entry) with "sent:" before it.
func promptTexts(m *Model) []string {
	var out []string
	for _, r := range m.Rows() {
		switch {
		case r.Sent():
			out = append(out, "sent:"+r.Text)
		case r.Kind == RowPrompt:
			out = append(out, m.Entry(r.Entry).Text)
		}
	}
	return out
}

func pendingTexts(m *Model) []string {
	var out []string
	for _, p := range m.Pendings() {
		out = append(out, p.Text)
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMatches(t *testing.T) {
	cases := []struct {
		text, sent string
		want       bool
	}{
		{"fix it", "fix it", true},
		{"  fix it\n", "fix it ", true},
		{"<system-reminder>be brief</system-reminder>\nfix it", "fix it", true},
		{"fix it\n<user-prompt-submit-hook>x</user-prompt-submit-hook>", "fix it", true},
		{"fix it now", "fix it", false},
		{"please fix it", "fix it", false},
		{"other", "fix it", false},
		{"fix it", "", false},
	}
	for _, c := range cases {
		if got := Matches(c.text, c.sent); got != c.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.text, c.sent, got, c.want)
		}
	}
}

func TestQueuedMessageEndsItsPending(t *testing.T) {
	for _, line := range []string{lQueued, lReminder} {
		var m Model
		r := transcript.NewReader(0)
		apply(t, &m, r, lPrompt, lCall1)
		m.AddPending("add auto compact later")
		apply(t, &m, r, line)
		if p := pendingTexts(&m); len(p) != 0 {
			t.Errorf("%s: pending %q stays", line, p)
		}
		if got := promptTexts(&m); !same(got, []string{"start builders for 609", "add auto compact later"}) {
			t.Errorf("%s: prompts %q", line, got)
		}
	}
}

func TestQueuedMessageShowsOnce(t *testing.T) {
	var m Model
	r := transcript.NewReader(0)
	apply(t, &m, r, lPrompt)
	m.AddPending("add auto compact later")
	apply(t, &m, r, lEnqueue, lCall1, lQueued, lReminder, lSaid)
	if got := promptTexts(&m); !same(got, []string{"start builders for 609", "add auto compact later"}) {
		t.Errorf("prompts %q; want the queued message once", got)
	}
	if len(m.Pendings()) != 0 {
		t.Errorf("pending %q stays", pendingTexts(&m))
	}
}

func TestLaterPromptSettlesOlderPending(t *testing.T) {
	// The prompt of a later pending prompt: the older one shows as sent before it.
	var m Model
	r := transcript.NewReader(0)
	m.AddPending("first")
	m.AddPending("second")
	m.AddPending("third")
	apply(t, &m, r, promptLine("p2", "", "second"))
	if got := promptTexts(&m); !same(got, []string{"sent:first", "second"}) {
		t.Errorf("prompts %q", got)
	}
	if p := pendingTexts(&m); !same(p, []string{"third"}) {
		t.Errorf("pending %q; want the newer one", p)
	}
}

func TestPromptAfterPendingSettlesItByTime(t *testing.T) {
	var m Model
	r := transcript.NewReader(0)
	m.AddPending("lost one")
	// A prompt from before the send, and one with no time, leave it pending.
	apply(t, &m, r, promptLine("p1", "2000-01-01T00:00:00Z", "typed in the terminal"), promptLine("p2", "", "no time"))
	if p := pendingTexts(&m); !same(p, []string{"lost one"}) {
		t.Fatalf("pending %q; want it kept", p)
	}
	// A prompt from after the send settles it before itself.
	apply(t, &m, r, promptLine("p3", "2100-01-01T00:00:00Z", "later one"))
	if got := promptTexts(&m); !same(got, []string{"typed in the terminal", "no time", "sent:lost one", "later one"}) {
		t.Errorf("prompts %q", got)
	}
	if len(m.Pendings()) != 0 {
		t.Errorf("pending %q stays", pendingTexts(&m))
	}
}

func TestTurnEndSettlesPending(t *testing.T) {
	var m Model
	r := transcript.NewReader(0)
	apply(t, &m, r, lPrompt, lCall1)
	m.AddPending("lost one")
	id := m.Pendings()[0].ID
	m.TurnEnded()
	rows := m.Rows()
	if len(m.Pendings()) != 0 || !rows[len(rows)-1].Sent() || rows[len(rows)-1].ID != id {
		t.Fatalf("rows %+v, pending %q; want the pending prompt sent at the end, with its id", rows, pendingTexts(&m))
	}
	// Its prompt entry comes after all: it takes the sent prompt's place, once.
	apply(t, &m, r, promptLine("p2", "", "lost one"))
	if got := promptTexts(&m); !same(got, []string{"start builders for 609", "lost one"}) {
		t.Errorf("prompts %q; want the entry once", got)
	}
	// A sent prompt before the last prompt entry stays.
	m.AddPending("again")
	m.TurnEnded()
	apply(t, &m, r, promptLine("p3", "", "other"), promptLine("p4", "", "again"))
	if got := promptTexts(&m); !same(got, []string{"start builders for 609", "lost one", "sent:again", "other", "again"}) {
		t.Errorf("prompts %q", got)
	}
}

func TestTakePendingKeepsSentPrompts(t *testing.T) {
	old := &Model{}
	old.AddPending("waits")
	old.AddPending("brought")
	old.TurnEnded()
	old.AddPending("still")
	// The read brings "brought" as its last prompt: that sent prompt goes; the other stays.
	m := &Model{}
	apply(t, m, transcript.NewReader(0), promptLine("p1", "", "brought"))
	m.takePending(old)
	if got := promptTexts(m); !same(got, []string{"brought", "sent:waits"}) {
		t.Errorf("prompts %q", got)
	}
	if p := pendingTexts(m); !same(p, []string{"still"}) {
		t.Errorf("pending %q", p)
	}
}

func TestViewSettlesPendingWhenTheTurnEnds(t *testing.T) {
	s := newSession(t, "working")
	s.write(lPrompt)
	s.typeIn("add auto compact later")
	s.key(0, ui.KeyEnter)
	if !s.has(LabelPending) {
		t.Fatalf("no pending prompt: %v", s.tt.Texts())
	}
	// The session goes on working: the prompt stays pending.
	s.tt.Frame()
	if !s.has(LabelPending) {
		t.Fatal("the pending prompt went while the turn runs")
	}
	// The turn ends with no prompt entry of it: it shows as a sent prompt, not dimmed.
	s.status = "idle"
	s.tt.Frame()
	s.tt.Frame()
	if s.has(LabelPending) || !s.tt.HasText("add auto compact later") {
		t.Errorf("pending %v, texts %v; want a sent prompt", s.has(LabelPending), s.tt.Texts())
	}
}
