package chatview

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/transcript"
)

// The important rows of the chat (D270).

const (
	lMarkSaid = `{"type":"assistant","uuid":"m1","timestamp":"2026-11-10T10:01:00Z","message":{"content":[{"type":"text","text":"Two things.\n\n> [!DECISION]\n> Ship the notch now\n> or after the release?\n\n> [!NOTE]\n> Only a note.\n\n` + "```md" + `\n> [!WARNING]\n> In a code block.\n` + "```" + `\n\n> [!warning] CI is slow\n\n>[!DONE]\n>PR 633 is merged.\n\n> [!UNVERIFIED]\n> The e2e run."}]}}`
	lPin      = `{"type":"assistant","uuid":"m2","timestamp":"2026-11-10T10:02:00Z","message":{"content":[{"type":"tool_use","id":"p1","name":"mcp__plugin_contr_contr__pin_for_owner","input":{"text":"Test the build"}}]}}`
	lPinRes   = `{"type":"user","uuid":"m3","timestamp":"2026-11-10T10:02:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"p1","content":"T4"}]}}`
	lAsk      = `{"type":"assistant","uuid":"m4","timestamp":"2026-11-10T10:03:00Z","message":{"content":[{"type":"tool_use","id":"q1","name":"mcp__plugin_contr_contr__ask_owner","input":{"question":"Which name?"}}]}}`
	lAskErr   = `{"type":"assistant","uuid":"m5","timestamp":"2026-11-10T10:03:30Z","message":{"content":[{"type":"tool_use","id":"q2","name":"mcp__plugin_contr_contr__ask_owner","input":{}}]}}`
	lAskErrR  = `{"type":"user","uuid":"m6","timestamp":"2026-11-10T10:03:31Z","message":{"content":[{"type":"tool_result","tool_use_id":"q2","is_error":true,"content":"no store"}]}}`
	lPrompt2  = `{"type":"user","uuid":"m7","timestamp":"2026-11-10T10:05:00Z","message":{"content":"ship it now"}}`
	lDecide2  = `{"type":"assistant","uuid":"m8","timestamp":"2026-11-10T10:06:00Z","message":{"content":[{"type":"text","text":"> [!DECISION]\n> Merge #634 too?"}]}}`
)

func markKinds(ms []Mark) []MarkKind {
	var out []MarkKind
	for _, m := range ms {
		out = append(out, m.Kind)
	}
	return out
}

func TestMarksAreTheImportantRowsInThreadOrder(t *testing.T) {
	var m Model
	apply(t, &m, transcript.NewReader(0), lPrompt, lCall1, lRes1, lMarkSaid, lTaskBad, lPin, lPinRes, lAsk, lAskErr, lAskErrR, lNote1, lPrompt2, lDecide2)
	got := Marks(&m)
	want := []MarkKind{MarkYou, MarkDecision, MarkWarning, MarkDone, MarkUnverified, MarkNeedsYou, MarkPinned, MarkAsked, MarkYou, MarkDecision}
	if k := markKinds(got); len(k) != len(want) {
		t.Fatalf("kinds %v, want %v", k, want)
	}
	for i, k := range markKinds(got) {
		if k != want[i] {
			t.Errorf("mark %d: kind %v, want %v", i, k, want[i])
		}
	}
	rows := m.Rows()
	for i, mk := range got {
		if mk.Row < 0 || mk.Row >= len(rows) {
			t.Fatalf("mark %d: row %d out of %d", i, mk.Row, len(rows))
		}
	}
	if got[0].Text != "start builders for 609" || !got[0].At.Equal(time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("prompt mark %+v", got[0])
	}
	if got[1].Text != "Ship the notch now or after the release?" || got[3].Text != "PR 633 is merged." || got[2].Text != "CI is slow" {
		t.Errorf("remark texts %q, %q, %q", got[1].Text, got[2].Text, got[3].Text)
	}
	if got[1].Row != got[4].Row || got[1].ID == got[2].ID {
		t.Errorf("remarks of one text share its row, each with its own id: %+v", got[1:5])
	}
	if got[5].Text != "Background task failed: \"go vet\" failed with exit code 1" || rows[got[5].Row].Kind != RowOwnerNote {
		t.Errorf("needs-you mark %+v", got[5])
	}
	if got[6].Item != "T4" || rows[got[6].Row].Kind != RowGroup || got[6].Text != "" {
		t.Errorf("pin mark %+v, want item T4 in a work group", got[6])
	}
	if got[7].Item != "" {
		t.Errorf("a question with no result has no item: %+v", got[7])
	}
	// The first decision has a prompt after it; the last does not.
	if got[1].Waits || !got[9].Waits {
		t.Errorf("waits: %v %v, want false true", got[1].Waits, got[9].Waits)
	}
}

func TestMarkWords(t *testing.T) {
	for k, w := range map[MarkKind]string{MarkYou: "You", MarkDecision: "Decision", MarkWarning: "Warning", MarkDone: "Done",
		MarkUnverified: "Not verified", MarkNeedsYou: "Needs you", MarkPinned: "Pinned", MarkAsked: "Question"} {
		if k.Word() != w {
			t.Errorf("%d: %q, want %q", k, k.Word(), w)
		}
	}
}

func TestMarksOfASentPromptHaveNoTime(t *testing.T) {
	var m Model
	m.AddPending("go on")
	m.TurnEnded()
	got := Marks(&m)
	if len(got) != 1 || got[0].Kind != MarkYou || got[0].Text != "go on" || !got[0].At.IsZero() {
		t.Errorf("marks %+v, want one You with no time", got)
	}
}

func TestFirstWordsCutsALongText(t *testing.T) {
	long := ""
	for range 100 {
		long += "word\n"
	}
	got := []rune(firstWords(long))
	if len(got) != markTextMax+1 || got[markTextMax] != '…' {
		t.Errorf("%d runes, want %d and an ellipsis", len(got), markTextMax+1)
	}
}

// The chat tells the room of its list and scrolls to a row.
func TestChatAreaAndScrollToRow(t *testing.T) {
	r := scrolledRig(t)
	top, bottom, ok := r.ch.Area()
	if !ok || top != topSpace || bottom <= 0 || bottom > 300 {
		t.Errorf("Area = %v, %v, %v; want the switch row above and the dock below", top, bottom, ok)
	}
	if !r.ch.list.AtEnd() {
		t.Fatal("the chat does not start at its end")
	}
	r.ch.ScrollToRow(0)
	r.settle()
	r.settle()
	if first, _ := r.ch.list.Visible(); first != 0 {
		t.Errorf("first row in view %d after ScrollToRow(0)", first)
	}
}
