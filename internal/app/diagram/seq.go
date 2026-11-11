package diagram

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// LangSeq is the language of the fenced block that holds a sequence diagram (D287).
const LangSeq = "contr-seq"

// LabelSeq is the label of a drawn sequence, for the screen reader and the tests.
const LabelSeq = "Sequence diagram"

// Limits past which a sequence shows as its code block.
const (
	MaxActors   = 6
	MaxMsgs     = 16
	MaxMsgLabel = 40
)

// Seq is a sequence diagram: actors left to right in order of first use, messages top to bottom.
type Seq struct {
	Actors []string
	Msgs   []Msg
}

// Msg is one arrow of a sequence. Reply is a dashed arrow. From == To is a message to itself.
type Msg struct {
	From, To int
	Label    string
	Reply    bool
}

// ErrNoMsg is the error of ParseSeq for a block without a message.
var ErrNoMsg = errors.New("no message")

// Was: or "b-649 --> lead: done" (a reply).
var msgRe = regexp.MustCompile(`^([^:]+?)\s*(-->|->)\s*([^:\s][^:]*?)\s*:\s*(.+?)\s*$`)

func (s *Seq) actor(name string) int {
	for i, a := range s.Actors {
		if a == name {
			return i
		}
	}
	s.Actors = append(s.Actors, name)
	return len(s.Actors) - 1
}

// ParseSeq reads the text of a contr-seq block. A line is "A -> B: text" or "A --> B: text" (a
// reply); the text is required; empty lines are skipped.
func ParseSeq(src string) (Seq, error) {
	var s Seq
	for k, l := range strings.Split(src, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		m := msgRe.FindStringSubmatch(l)
		if m == nil || strings.Contains(m[3], "->") {
			return Seq{}, fmt.Errorf("line %d: not \"A -> B: text\" or \"A --> B: text\"", k+1)
		}
		from, to := s.actor(m[1]), s.actor(m[3])
		s.Msgs = append(s.Msgs, Msg{From: from, To: to, Label: m[4], Reply: m[2] == "-->"})
	}
	if len(s.Msgs) == 0 {
		return Seq{}, ErrNoMsg
	}
	return s, nil
}

// Fits reports whether the sequence is small enough to draw, and the reason when it is not.
func (s Seq) Fits() (bool, string) {
	switch {
	case len(s.Actors) > MaxActors:
		return false, "more than 6 actors"
	case len(s.Msgs) > MaxMsgs:
		return false, "more than 16 messages"
	}
	for _, a := range s.Actors {
		if len([]rune(a)) > MaxLabel {
			return false, "an actor longer than 28 characters"
		}
	}
	for _, m := range s.Msgs {
		if len([]rune(m.Label)) > MaxMsgLabel {
			return false, "a message longer than 40 characters"
		}
	}
	return true, ""
}

// Sizes of a sequence, in points.
const (
	laneGap  = 40 // the least room between two lifelines, beyond the actor names
	laneMin  = 120
	edgeMin  = 40 // the least room left of the first and right of the last lifeline
	loopW    = 24 // the width of a message to itself
	labelGap = 24
)

// SeqView draws s, at most maxW wide, and returns true: actor names on top, a dashed lifeline
// under each, one arrow per message top to bottom. It builds nothing and returns false when s is
// too large or wider than maxW.
func SeqView(c *ui.Context, s Seq, maxW float32, st Style) bool {
	if ok, _ := s.Fits(); !ok {
		return false
	}
	pal := theme.Current()
	actor := func(a string) ui.Span {
		sp := st.node(a)
		sp.Weight = theme.StyleBodyStrong.Weight
		return sp
	}
	aw := make([]float32, len(s.Actors))
	for i, a := range s.Actors {
		aw[i], _ = c.MeasureText(0, actor(a))
	}
	_, lh := c.MeasureText(0, st.label("M"))
	_, ah := c.MeasureText(0, actor("M"))
	actorH := ah + 12
	pitch := max(32, lh+20)
	// The room between two neighbor lanes holds the names and the widest label between them.
	last := len(s.Actors) - 1
	room := make([]float32, last)
	for i := range room {
		room[i] = max(laneMin, (aw[i]+aw[i+1])/2+laneGap)
	}
	left, right := max(aw[0]/2, edgeMin), max(aw[last]/2, edgeMin)
	for _, m := range s.Msgs {
		w, _ := c.MeasureText(0, st.label(m.Label))
		lo, hi := min(m.From, m.To), max(m.From, m.To)
		if lo == hi {
			// A message to itself: the loop and its label at the right of the lane.
			need := loopW + 6 + w + labelGap/2
			if lo == last {
				right = max(right, need)
			} else {
				room[lo] = max(room[lo], need)
			}
			continue
		}
		per := (w + labelGap) / float32(hi-lo)
		for k := lo; k < hi; k++ {
			room[k] = max(room[k], per)
		}
	}
	laneX := make([]float32, len(s.Actors))
	x := left
	for i := range s.Actors {
		laneX[i] = x
		if i < last {
			x += room[i]
		}
	}
	totalW := x + right
	if totalW > maxW {
		return false
	}
	totalH := actorH + 8 + float32(len(s.Msgs))*pitch + 6
	ui.Box(c).Label(LabelSeq).Width(totalW).Height(totalH).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		for i, a := range s.Actors {
			sp := actor(a)
			sp.Color = pal.Ink88
			w, h := p.MeasureText(0, sp)
			lx := r.X + laneX[i]
			p.RichText(lx-w/2, r.Y+(actorH-h)/2, 0, sp)
			life := (&ui.Path{}).MoveTo(lx, r.Y+actorH+2).LineTo(lx, r.Y+totalH).Cap(ui.ButtCap).Dash(0, 3, 4)
			p.StrokePath(life, 1, pal.Ink30)
		}
		for k, m := range s.Msgs {
			// The arrow sits low in its row, the label above it.
			y := r.Y + actorH + 8 + float32(k)*pitch + pitch - 8
			x0, x1 := r.X+laneX[m.From], r.X+laneX[m.To]
			sp := st.label(m.Label)
			sp.Color = pal.Ink64
			w, h := p.MeasureText(0, sp)
			if m.From == m.To {
				loop := (&ui.Path{}).MoveTo(x0+3, y-12).LineTo(x0+loopW, y-12).LineTo(x0+loopW, y).LineTo(x0+4, y)
				if m.Reply {
					loop.Dash(0, 4, 3)
				}
				p.StrokePath(loop, 1.2, pal.Ink48)
				p.StrokePath(head(x0+4, y, -1, 0), 1.2, pal.Ink48)
				p.RichText(x0+loopW+6, y-6-h/2, 0, sp)
				continue
			}
			dir := float32(1)
			if x1 < x0 {
				dir = -1
			}
			line := (&ui.Path{}).MoveTo(x0+dir*3, y).LineTo(x1-dir*3, y).Cap(ui.ButtCap)
			if m.Reply {
				line.Dash(0, 4, 3)
			}
			p.StrokePath(line, 1.2, pal.Ink48)
			p.StrokePath(head(x1-dir*3, y, dir, 0), 1.2, pal.Ink48)
			p.RichText((x0+x1)/2-w/2, y-h-3, 0, sp)
		}
	})
	return true
}
