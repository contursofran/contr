package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/owner"
)

const copyCmd = "go run ./tmp/usagecheck"

// copyRig is the card on a to-do whose text holds a code span, with the
// toasts of the app.
func copyRig(t *testing.T) (*qRig, *state.Toasts) {
	t.Helper()
	src := askSource()
	src.snap.Todos = []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Run `" + copyCmd + "` and report"}}
	r := newQRig(t, src)
	toasts := state.NewToasts(nil)
	r.card.Status = toasts.Show
	r.open("T1")
	return r, toasts
}

func TestTodoCodeSpanShowsAsChipWithoutBackticks(t *testing.T) {
	r, _ := copyRig(t)
	if _, ok := r.tt.Find(LabelCopyCode + copyCmd); !ok {
		t.Fatalf("no code chip %q in %q", LabelCopyCode+copyCmd, r.tt.Texts())
	}
	// The paragraph reads without the backticks.
	if !r.tt.HasText("Run " + copyCmd + " and report") {
		t.Errorf("no paragraph without backticks in %q", r.tt.Texts())
	}
}

func TestTodoChipTabShowsNoBackticks(t *testing.T) {
	r, _ := copyRig(t)
	want := "Run " + copyCmd + " and report"
	if _, ok := r.tt.Find(want); !ok {
		t.Fatalf("no item tab %q in %q", want, r.tt.Texts())
	}
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, "`") {
			t.Errorf("text %q shows a backtick", s)
		}
	}
}

func TestCardNameShowsNoBackticks(t *testing.T) {
	r, _ := copyRig(t)
	items := r.notch.Items()
	i := slices.IndexFunc(items, func(it NeedTab) bool { return it.Key == "T1" })
	if i < 0 {
		t.Fatalf("no item T1")
	}
	want := "Run " + copyCmd + " and report"
	if got := r.card.name(items[i], r.src.snap); got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestCodeChipClickCopiesAndShowsCopied(t *testing.T) {
	r, toasts := copyRig(t)
	if err := r.tt.Click(LabelCopyCode + copyCmd); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Clipboard(); got != copyCmd {
		t.Errorf("clipboard %q, want %q", got, copyCmd)
	}
	if got := toasts.Pending(); !slices.Equal(got, []string{QuestionsCopied}) {
		t.Errorf("toasts %q, want %q", got, QuestionsCopied)
	}
	if !r.card.IsOpen() {
		t.Error("the click closed the card")
	}
}

func TestKeysWorkAfterACodeChipClick(t *testing.T) {
	r, _ := copyRig(t)
	if err := r.tt.Click(LabelCopyCode + copyCmd); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	if !slices.Equal(r.src.dones, []string{"T1"}) {
		t.Errorf("Enter after the chip click: dones %v", r.src.dones)
	}
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.card.IsOpen() {
		t.Error("Escape left the card open")
	}
}

func TestCardTextIsSelectableAndCopies(t *testing.T) {
	r, _ := copyRig(t)
	from, ok := r.tt.Find(LabelCopyCode + copyCmd)
	if !ok {
		t.Fatalf("no code chip in %q", r.tt.Texts())
	}
	// A drag from the left of the to-do text over the chip selects it.
	y := from.Y + from.H/2
	r.tt.Press(from.X-30, y)
	r.tt.Move(from.X+from.W+5, y)
	r.tt.Release(from.X+from.W+5, y)
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyC)
	if got := r.tt.Clipboard(); got == "" || !strings.Contains(got, copyCmd) {
		t.Errorf("Cmd+C after a drag copied %q", got)
	}
}

func TestUnclosedBacktickShowsAsPlainText(t *testing.T) {
	src := askSource()
	src.snap.Todos = []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Run `go test"}}
	r := newQRig(t, src)
	r.open("T1")
	if !r.tt.HasText("Run `go test") {
		t.Errorf("unclosed backtick text missing: %q", r.tt.Texts())
	}
	if _, ok := r.tt.Find(LabelCopyCode + "go test"); ok {
		t.Error("an unclosed backtick made a chip")
	}
}

func TestDetailsSourceCodeSpanShowsAsChip(t *testing.T) {
	src := askSource()
	src.snap.Questions[0].Source = "see `make check`"
	r := newQRig(t, src)
	r.open("D1")
	if err := r.tt.Click(LabelQuestionsDetails); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if _, ok := r.tt.Find(LabelCopyCode + "make check"); !ok {
		t.Errorf("no code chip in Details: %q", r.tt.Texts())
	}
}
