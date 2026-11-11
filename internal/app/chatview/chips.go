package chatview

// Chips (docs/spec/ChatView.md, D262): names in Claude's text that contr knows show live. A PR
// or an issue number ("#667"), a worker's name ("b-649") and a path of a file in the session's
// folder become links of the scheme contr-chip: a pre-pass rewrites the Markdown source, outside
// code and links. The link's icon (markdown.Options.LinkIcon) is a 6px dot in the color of the
// state, or the file icon; a click (OnLink) opens the PR, selects the worker or opens the file.
// The state is part of the link's address, so a paragraph builds again only when a state changes.

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// TokenKind is what a name in the text may be.
type TokenKind uint8

const (
	// TokenNumber is "#667": a pull request or an issue.
	TokenNumber TokenKind = iota
	// TokenWord is a word that may be a worker's name: "b-649".
	TokenWord
	// TokenPath is a word that may be a file of the session's folder: "internal/app/x.go",
	// "x.go:42".
	TokenPath
)

// Token is a name found in a text: its kind, the name to look up ("667", "b-649",
// "internal/app/x.go" without its ":42"), and its bytes in the source.
type Token struct {
	Kind       TokenKind
	Name       string
	Start, End int
}

// ChipKind is what a chip shows; ChipNone is no chip.
type ChipKind uint8

const (
	ChipNone ChipKind = iota
	ChipPR
	ChipIssue
	ChipWorker
	ChipFile
)

// ChipState is the state a chip's dot shows; StateNone has no dot.
type ChipState uint8

const (
	StateNone ChipState = iota
	// The states of a pull request.
	StateOpen
	StateRunning
	StatePassed
	StateFailed
	StateMerged
	StateClosed
	// The states of a worker.
	StateNeedsYou
	StateWorking
	StateDone
	StateWaiting
)

// Chip is a known name: its kind and its state now.
type Chip struct {
	Kind  ChipKind
	State ChipState
	Draft bool // a draft PR
}

// ChipSource knows the names of the chips. Its methods run on the UI thread.
type ChipSource interface {
	// Look returns the chip of token t in the session at addr, false when contr does not know
	// the name (yet). It may start a read off the UI thread, whose answer redraws.
	Look(addr string, t Token) (Chip, bool)
	// Open is a click on the chip of name in the session at addr.
	Open(addr string, c Chip, name string)
	// Worker is the worker of name for a status board (blocks.go): its chip and the PR of its
	// branch; false when contr does not list it.
	Worker(addr, name string) (WorkerInfo, bool)
	// Ref is number n in the session at addr for a board or a queue: its chip, whether it is
	// done, and its checks; false when contr does not know it (yet). It reads as Look does.
	Ref(addr string, n int) (RefInfo, bool)
}

const chipScheme = "contr-chip:"

var (
	kindWords = [...]string{"", "pr", "issue", "worker", "file"}
	// draftWord ends the kind word of a draft PR: "pr.draft".
	draftWord  = ".draft"
	stateWords = [...]string{"", "open", "running", "passed", "failed", "merged", "closed", "needs-you", "working", "done", "waiting"}
	// stateTexts end the tip of a chip.
	stateTexts = [...]string{"", "open", "checks running", "checks passed", "checks failed", "merged", "closed", "needs you", "working", "done", "waiting"}
)

// chipDest is the address of the link of chip c of name: contr-chip:KIND/STATE/NAME.
func chipDest(c Chip, name string) string {
	kind := kindWords[c.Kind]
	if c.Draft && c.Kind == ChipPR {
		kind += draftWord
	}
	return chipScheme + kind + "/" + stateWords[c.State] + "/" + name
}

// parseChipDest reads an address that chipDest made.
func parseChipDest(dest string) (Chip, string, bool) {
	rest, ok := strings.CutPrefix(dest, chipScheme)
	if !ok {
		return Chip{}, "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[2] == "" {
		return Chip{}, "", false
	}
	var c Chip
	parts[0], c.Draft = strings.CutSuffix(parts[0], draftWord)
	for i, w := range kindWords {
		if i > 0 && w == parts[0] {
			c.Kind = ChipKind(i)
		}
	}
	for i, w := range stateWords {
		if w == parts[1] {
			c.State = ChipState(i)
		}
	}
	if c.Kind == ChipNone || (c.Draft && c.Kind != ChipPR) || (c.State == StateNone && parts[1] != "") {
		return Chip{}, "", false
	}
	return c, parts[2], true
}

// chipTip is the tip of a chip: "PR #667 · checks passed", "Worker b-649 · needs you",
// "Issue #649", "Open internal/app/x.go".
func chipTip(c Chip, name string) string {
	var s string
	switch c.Kind {
	case ChipPR:
		s = "PR #" + name
		if c.Draft {
			s = "Draft PR #" + name
		}
	case ChipIssue:
		s = "Issue #" + name
	case ChipWorker:
		s = "Worker " + name
	case ChipFile:
		return "Open " + name
	}
	if t := stateTexts[c.State]; t != "" {
		s += " · " + t
	}
	return s
}

// ChipColor is the color of the dot of state s, false for no dot: the content palette (D262).
func ChipColor(p *theme.Palette, s ChipState) (ui.Color, bool) {
	switch s {
	case StateRunning, StateWorking:
		return p.ContentRunning, true
	case StatePassed, StateMerged, StateDone:
		return p.ContentDone, true
	case StateFailed, StateNeedsYou:
		return p.ContentFailed, true
	case StateWaiting:
		return p.ContentWaiting, true
	case StateOpen, StateClosed:
		return p.ContentInfo, true
	}
	return ui.Color{}, false
}

const (
	chipDot    = 6 // the dot's width
	chipDotGap = 4 // between the dot and the text
)

// chipIcon is the LinkIcon of the views: the dot of a chip with a state, the file icon of a file
// chip, nothing for other links. Its color comes from the theme as it draws.
func chipIcon(dest string, size float32) (markdown.InlineObject, bool) {
	c, _, ok := parseChipDest(dest)
	if !ok {
		return markdown.InlineObject{}, false
	}
	if c.Kind == ChipFile {
		side := size * 0.9
		return markdown.InlineObject{Width: side + chipDotGap, Height: side * 0.82, Depth: side * 0.18,
			Draw: func(p *ui.Painter, r ui.Rect) {
				if s := icons.Line("file"); s != nil {
					p.Icon(s, ui.Rect{X: r.X, Y: r.Y, W: side, H: side}, theme.Current().Ink48)
				}
			}}, true
	}
	if g := ChipGitHub(c); g != icons.GitHubNone {
		return githubIcon(c, g, size), true
	}
	if _, ok := ChipColor(theme.Current(), c.State); !ok {
		return markdown.InlineObject{}, false
	}
	// The dot's center is near the middle of the lowercase letters.
	return markdown.InlineObject{Width: chipDot + chipDotGap, Height: size*0.3 + chipDot/2,
		Draw: func(p *ui.Painter, r ui.Rect) {
			if col, ok := ChipColor(theme.Current(), c.State); ok {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: chipDot, H: chipDot}, col, chipDot/2)
			}
		}}, true
}

// ChipGitHub is the Octicon of a PR or an issue chip (D286): a PR by its state (merged, closed,
// draft or open), an issue open or closed. Other chips have none.
func ChipGitHub(c Chip) icons.GitHub {
	switch c.Kind {
	case ChipPR:
		switch c.State {
		case StateMerged:
			return icons.PRMerged
		case StateClosed:
			return icons.PRClosed
		}
		if c.Draft {
			return icons.PRDraft
		}
		return icons.PROpen
	case ChipIssue:
		if c.State == StateClosed {
			return icons.IssueClosed
		}
		return icons.IssueOpen
	}
	return icons.GitHubNone
}

// checksDot reports whether the chip's state is one of checks, which a dot shows after the
// Octicon: running, passed or failed.
func checksDot(s ChipState) bool { return s == StateRunning || s == StatePassed || s == StateFailed }

// githubIcon is the LinkIcon of a PR or an issue chip: its Octicon in ink-48, 0.9 of the text
// size as the file icon, then the dot of its checks when it has one.
func githubIcon(c Chip, g icons.GitHub, size float32) markdown.InlineObject {
	side := size * 0.9
	w := side + chipDotGap
	if checksDot(c.State) {
		w += chipDot + chipDotGap
	}
	return markdown.InlineObject{Width: w, Height: side * 0.82, Depth: side * 0.18,
		Draw: func(p *ui.Painter, r ui.Rect) {
			pal := theme.Current()
			if s := icons.GitHubSVG(g); s != nil {
				p.Icon(s, ui.Rect{X: r.X, Y: r.Y, W: side, H: side}, pal.Ink48)
			}
			if col, ok := ChipColor(pal, c.State); ok && checksDot(c.State) {
				y := r.Y + side/2 - chipDot/2
				p.Fill(ui.Rect{X: r.X + side + chipDotGap, Y: y, W: chipDot, H: chipDot}, col, chipDot/2)
			}
		}}
}

// Tokens finds the names in Markdown src that may be chips: in text only, not in code, links,
// images or HTML. A run of text is the text between two of those, as its source has it; text
// written with an escape or an entity ends a run.
func Tokens(src string) []Token {
	d := parser.Parse(src)
	var out []Token
	for i := 1; i < d.Len(); i++ {
		in := d.Inlines(i)
		if len(in) == 0 {
			continue
		}
		runFrom, runTo := -1, -1
		flush := func() {
			if runFrom >= 0 {
				out = appendTokens(out, src, runFrom, runTo)
			}
			runFrom, runTo = -1, -1
		}
		var walk func(from, to int)
		walk = func(from, to int) {
			for k := from; k < to; k = in[k].Next {
				n := &in[k]
				switch n.Kind {
				case parser.Text, parser.Reference, parser.Mention:
					if n.End <= len(src) && src[n.Start:n.End] == n.Text {
						if runTo != n.Start {
							flush()
							runFrom = n.Start
						}
						runTo = n.End
						continue
					}
				case parser.Emphasis, parser.Strong, parser.Strikethrough:
					flush()
					walk(k+1, n.Next)
				}
				flush()
			}
		}
		walk(0, len(in))
		flush()
	}
	return out
}

// tokenByte tells the bytes of a word that may be a name or a path.
func tokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '_' || b == '-' || b == '.' || b == '/' || b == ':'
}

func digit(b byte) bool { return b >= '0' && b <= '9' }

// appendTokens adds the tokens of src[from:to], a run of text.
func appendTokens(out []Token, src string, from, to int) []Token {
	for i := from; i < to; {
		b := src[i]
		before := i == from || !tokenByte(src[i-1]) && src[i-1] != '#'
		switch {
		case b == '#' && before:
			j := i + 1
			for j < to && digit(src[j]) {
				j++
			}
			if j > i+1 && (j == to || !tokenByte(src[j]) || src[j] == '.' || src[j] == ':') {
				out = append(out, Token{Kind: TokenNumber, Name: src[i+1 : j], Start: i, End: j})
			}
			i = max(j, i+1)
		case tokenByte(b) && before:
			j := i
			for j < to && tokenByte(src[j]) {
				j++
			}
			if t, ok := word(src[i:j]); ok {
				t.Start, t.End = i, i+len(t.Name)+t.End
				out = append(out, t)
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// word classifies a word: a path when it has a slash or an extension, else a name with a letter.
// The token's End is the length of a ":42" line suffix after its name.
func word(w string) (Token, bool) {
	w = strings.TrimRight(w, ".,:;-")
	line := ""
	if i := strings.IndexByte(w, ':'); i > 0 {
		if _, err := strconv.Atoi(strings.ReplaceAll(w[i+1:], ":", "")); err != nil {
			return Token{}, false // "a:b" is no name; "x.go:42" and "x.go:42:7" are
		}
		w, line = w[:i], w[i:]
	}
	if w == "" {
		return Token{}, false
	}
	dot := strings.LastIndexByte(w, '.')
	ext := dot > 0 && dot < len(w)-1 && strings.ContainsFunc(w[dot+1:], func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' })
	switch {
	case strings.Contains(w, "/") || ext:
		return Token{Kind: TokenPath, Name: w, End: len(line)}, true
	case line == "" && strings.ContainsFunc(w, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }):
		return Token{Kind: TokenWord, Name: w}, true
	}
	return Token{}, false
}

// rewrite makes each token with a chip a link to its chip in src.
func rewrite(src string, toks []Token, chips []Chip) string {
	var b strings.Builder
	at := 0
	for i, t := range toks {
		c := chips[i]
		if c.Kind == ChipNone {
			continue
		}
		b.WriteString(src[at:t.Start])
		b.WriteByte('[')
		for _, r := range src[t.Start:t.End] {
			if strings.ContainsRune(`\_*~[]`, r) {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteString("](" + chipDest(c, t.Name) + ` "` + chipTip(c, t.Name) + `")`)
		at = t.End
	}
	b.WriteString(src[at:])
	return b.String()
}

// chipPart is a part of a text with its tokens, and the source with chips that the last chips
// gave.
type chipPart struct {
	src   string
	toks  []Token
	chips []Chip
	out   string
}

// chipText is the source of part i of the text row id with its known names as chips. The tokens
// are found once for each source; the chips are looked up at each build, so a chip follows the
// state of now, and the source is made again only when a chip changes.
func (ch *Chat) chipText(id string, i int, src string) string {
	if ch.Chips == nil {
		return src
	}
	key := id + "/" + strconv.Itoa(i)
	p := ch.chips[key]
	if p == nil || p.src != src {
		p = &chipPart{src: src, toks: Tokens(src), out: src}
		p.chips = make([]Chip, len(p.toks))
		ch.chips[key] = p
	}
	changed := false
	addr := ch.src.Terminal()
	for k, t := range p.toks {
		c, ok := ch.Chips.Look(addr, t)
		if !ok {
			c = Chip{}
		}
		if c != p.chips[k] {
			p.chips[k], changed = c, true
		}
	}
	if changed {
		p.out = rewrite(src, p.toks, p.chips)
	}
	return p.out
}

// openLink is a click on a link of Claude's text: a chip goes to its source; a #heading scrolls
// to it; an http, https or mailto link opens as markdown.View opens it without OnLink.
func (ch *Chat) openLink(url string) {
	if c, name, ok := parseChipDest(url); ok {
		if ch.Chips != nil {
			ch.Chips.Open(ch.src.Terminal(), c, name)
		}
		return
	}
	if strings.HasPrefix(url, "#") {
		ch.md.Anchor = url
		ch.md.AnchorVersion++
		ch.Env.Redraw()
		return
	}
	if ui.SafeToOpen(url) {
		ch.svc.OpenURL(url)
	}
}

// RefChip is the chip of a number: a merged or closed PR, an open one by its checks (a failed
// check first, then a pending one, then a passed one), a draft one marked, or an issue open or
// closed.
func RefChip(r github.Ref) Chip {
	if !r.PR {
		if r.State == "CLOSED" {
			return Chip{Kind: ChipIssue, State: StateClosed}
		}
		return Chip{Kind: ChipIssue}
	}
	c := Chip{Kind: ChipPR, State: StateOpen, Draft: r.Draft && r.State == "OPEN"}
	switch {
	case r.State == "MERGED":
		c.State = StateMerged
	case r.State == "CLOSED":
		c.State = StateClosed
	case r.Checks.Fail > 0:
		c.State = StateFailed
	case r.Checks.Pending > 0:
		c.State = StateRunning
	case r.Checks.Pass > 0:
		c.State = StatePassed
	}
	return c
}

// WorkerChip is the chip of a worker, as the team row counts it (D259): an ended one is done, a
// blocked one needs the owner, a working one works, any other waits.
func WorkerChip(w agents.Worker) Chip {
	c := Chip{Kind: ChipWorker, State: StateWaiting}
	switch {
	case !w.Live:
		c.State = StateDone
	case w.Status == "blocked":
		c.State = StateNeedsYou
	case w.Status == "working":
		c.State = StateWorking
	}
	return c
}
