package diagram

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

const review = "owner -> lead: fix #696\nlead -> b-696: build step 2\nb-696 -> b-696: run check.sh\n" +
	"b-696 --> lead: PR #740 open\nlead --> owner: ready to merge\n"

func TestParseSeq(t *testing.T) {
	s, err := ParseSeq(review)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Actors, ",") != "owner,lead,b-696" || len(s.Msgs) != 5 {
		t.Fatalf("actors %v, messages %d", s.Actors, len(s.Msgs))
	}
	if m := s.Msgs[0]; m.From != 0 || m.To != 1 || m.Label != "fix #696" || m.Reply {
		t.Errorf("first message %+v", m)
	}
	if m := s.Msgs[2]; m.From != 2 || m.To != 2 {
		t.Errorf("a message to itself %+v", m)
	}
	if m := s.Msgs[3]; !m.Reply || m.From != 2 || m.To != 1 {
		t.Errorf("a reply %+v", m)
	}
	if s, err := ParseSeq("\n  a -> b: x: y  \n\n"); err != nil || s.Msgs[0].Label != "x: y" {
		t.Errorf("a label with a colon, blank lines: %+v %v", s, err)
	}
}

func TestParseSeqErrors(t *testing.T) {
	for _, src := range []string{"", "\n \n"} {
		if _, err := ParseSeq(src); !errors.Is(err, ErrNoMsg) {
			t.Errorf("%q: %v, want %v", src, err, ErrNoMsg)
		}
	}
	for _, src := range []string{"a -> b", "just words", "a -> b: x\na -> b -> c: y", "a -> : x"} {
		if _, err := ParseSeq(src); err == nil || !strings.Contains(err.Error(), "line") {
			t.Errorf("%q: %v, want a line error", src, err)
		}
	}
}

func TestSeqLimits(t *testing.T) {
	var b strings.Builder
	for i := range MaxActors - 1 {
		b.WriteString(string(rune('a'+i)) + " -> " + string(rune('a'+i+1)) + ": x\n")
	}
	six, _ := ParseSeq(b.String())
	if ok, _ := six.Fits(); !ok {
		t.Error("6 actors do not fit")
	}
	seven, _ := ParseSeq(b.String() + "f -> g: x\n")
	if ok, _ := seven.Fits(); ok {
		t.Error("7 actors fit")
	}
	many, _ := ParseSeq(strings.Repeat("a -> b: x\n", MaxMsgs))
	if ok, _ := many.Fits(); !ok {
		t.Error("16 messages do not fit")
	}
	many, _ = ParseSeq(strings.Repeat("a -> b: x\n", MaxMsgs+1))
	if ok, _ := many.Fits(); ok {
		t.Error("17 messages fit")
	}
	long, _ := ParseSeq("a -> b: " + strings.Repeat("x", MaxMsgLabel+1))
	if ok, _ := long.Fits(); ok {
		t.Error("a long message fits")
	}
	name, _ := ParseSeq(strings.Repeat("x", MaxLabel+1) + " -> b: y")
	if ok, _ := name.Fits(); ok {
		t.Error("a long actor fits")
	}
}

// SeqView draws a sequence that fits, and builds nothing for one too large or wider than maxW.
func TestSeqView(t *testing.T) {
	s, _ := ParseSeq(review)
	big, _ := ParseSeq(strings.Repeat("a -> b: x\n", MaxMsgs+1))
	for _, tc := range []struct {
		s    Seq
		maxW float32
		want bool
	}{{s, 2000, true}, {s, 200, false}, {big, 2000, false}} {
		var got bool
		tt := ui.NewTester(func(c *ui.Context) { got = SeqView(c, tc.s, tc.maxW, DefaultStyle) }, 1200, 600)
		tt.Frame()
		_, drawn := tt.Access(LabelSeq)
		if got != tc.want || drawn != tc.want {
			t.Errorf("maxW %v: SeqView %v, element %v; want %v", tc.maxW, got, drawn, tc.want)
		}
	}
}
