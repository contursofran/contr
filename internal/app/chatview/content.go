package chatview

// Content: how Claude's Markdown shows (docs/spec/ChatView.md, Content; D261). MyGo's theme
// options style most blocks (mdTheme). The Block hook builds what an option cannot: remarks,
// tables without a box and with numbers at the right, numbered lists with mono numbers, and task
// lists with a check and a ring. A diff block is a Code builder. A part that holds Markdown of its
// own (a remark's text, a cell, an item) shows it in a markdown.View of its own, which shares the
// selection of the view around it.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the content blocks.
const (
	// LabelRemark names a remark, before its word: "remark Done".
	LabelRemark = "remark"
	// LabelRemarkBar names the bar of a remark, before its word.
	LabelRemarkBar = "remark bar"
	// LabelDiffLine names a line of a diff block, before its kind: "diff line added".
	LabelDiffLine = "diff line"
)

// Sizes of the content (docs/spec/ChatView.md, Content).
const (
	remarkBar      = 2
	cellPadX       = theme.Space16 // at the right of a cell only
	cellPadY       = theme.Space8
	cellMax        = 340 // a column wraps its cells beyond this width
	headSize       = 12
	headLine       = 18
	numSize        = 13
	markerSize     = 14 // the check and the ring of a task item
	headingSize    = 14
	headingWeight  = 600
	headingMore    = 10 // the room a heading has above it, more than a block
	inlineCodeSize = 12.5
)

// RemarkKind is the kind of a remark: a quote whose first line is a tag, as [!DONE].
type RemarkKind int

// The kinds of remarks.
const (
	RemarkNote RemarkKind = iota
	RemarkDone
	RemarkDecision
	RemarkWarning
	RemarkUnverified
)

// remarkTags are the tags of each kind, in lower case. GitHub's TIP and IMPORTANT are notes and
// CAUTION is a warning.
var remarkTags = map[string]RemarkKind{
	"note": RemarkNote, "tip": RemarkNote, "important": RemarkNote,
	"done": RemarkDone, "decision": RemarkDecision,
	"warning": RemarkWarning, "caution": RemarkWarning,
	"unverified": RemarkUnverified,
}

var remarkWords = [...]string{"Note", "Done", "Decision needed", "Warning", "Not verified"}

// Word is what the remark says above its text.
func (k RemarkKind) Word() string { return remarkWords[k] }

// Color is the color of the remark's bar and word: content-info for a note and an unverified
// remark, content-done, content-failed for a decision and content-waiting for a warning.
func (k RemarkKind) Color(pal *theme.Palette) ui.Color {
	switch k {
	case RemarkDone:
		return pal.ContentDone
	case RemarkDecision:
		return pal.ContentFailed
	case RemarkWarning:
		return pal.ContentWaiting
	}
	return pal.ContentInfo
}

// Remark is a remark: its kind and its text, Markdown.
type Remark struct {
	Kind RemarkKind
	Body string
}

var remarkRe = regexp.MustCompile(`^[ \t]*\[!([A-Za-z]+)\][ \t]*\n?`)

// RemarkOf reads the text of a quote, without its markers: a remark when its first line starts
// with a known tag, in any case, and text follows. An unknown tag, or a tag alone, is a quote.
func RemarkOf(quote string) (Remark, bool) {
	m := remarkRe.FindStringSubmatchIndex(quote)
	if m == nil {
		return Remark{}, false
	}
	kind, ok := remarkTags[strings.ToLower(quote[m[2]:m[3]])]
	body := quote[m[1]:]
	if !ok || strings.TrimSpace(body) == "" {
		return Remark{}, false
	}
	return Remark{Kind: kind, Body: body}, true
}

// quoteText is the source of a quote without its markers: on each line, the indent, the ">" and
// one space after it. A lazy line keeps its text.
func quoteText(src string) string {
	lines := strings.SplitAfter(src, "\n")
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, ">") {
			t = t[1:]
			if strings.HasPrefix(t, " ") || strings.HasPrefix(t, "\t") {
				t = t[1:]
			}
			lines[i] = t
		}
	}
	return strings.Join(lines, "")
}

// mdHooks sets the hooks of the chat's Markdown options.
func (ch *Chat) mdHooks() {
	code := map[string]func(*ui.Context, *markdown.CodeBlockView){"diff": ch.diffBlock, "patch": ch.diffBlock}
	ch.md.Block, ch.md.Code = ch.block, code
	ch.mdNest.Block, ch.mdNest.Code = ch.block, code
	for _, o := range []*markdown.Options{&ch.mdNest, &ch.mdHead, &ch.mdNum} {
		o.ShareSelection = true
	}
}

// mdTheme styles Claude's Markdown with the tokens of the current theme: prose in ink-85, strong
// text and links in ink-95, code in mono with the code colors.
func (ch *Chat) mdTheme(pal *theme.Palette) {
	if ch.md.Theme != nil && ch.md.Theme.Text == pal.Ink85 {
		return
	}
	size := theme.StyleProse.Size
	em := func(px float32) float32 { return px / size }
	th := &markdown.Theme{
		FontSize: size, Font: theme.FontFamilySans, CodeFont: theme.FontFamily,
		LineHeight: theme.StyleProse.Line / size, CodeLineHeight: theme.StyleTerminal.Line / theme.StyleTerminal.Size,
		Text: pal.Ink85, Muted: pal.Ink45, Link: pal.Ink95, Strong: pal.Ink95, StrongWeight: 500,
		Code: pal.Ink90, CodeBackground: pal.Hover, CodeSize: em(inlineCodeSize),
		BlockSpacing: em(theme.Space12),
		QuoteBar:     pal.Ink30, QuoteText: pal.Ink85, QuoteBarWidth: remarkBar, Rule: pal.Hairline,
		Alerts:              [5]ui.Color{pal.ContentInfo, pal.ContentInfo, pal.ContentInfo, pal.ContentWaiting, pal.ContentWaiting},
		CodeBlockBackground: pal.Hover, CodeBlockBorder: pal.Hairline, CodeBlockRadius: theme.Radius8,
		CodeColors: codeColors(pal), Mark: pal.Selected, Checkbox: pal.Ink45,
	}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{Size: em(headingSize), Weight: headingWeight, Color: pal.Ink95,
			Top: em(theme.Space12 + headingMore), Bottom: em(theme.Space12)}
	}
	head, num := *th, *th
	head.FontSize, head.LineHeight, head.Text, head.Strong = headSize, float32(headLine)/headSize, pal.Ink45, pal.Ink45
	num.FontSize, num.Font = numSize, theme.FontFamily
	ch.md.Theme, ch.mdNest.Theme, ch.mdHead.Theme, ch.mdNum.Theme = th, th, &head, &num
}

// codeColors maps the tokens of MyGo's tokenizers to the five code colors.
func codeColors(pal *theme.Palette) *editor.Theme {
	t := &editor.Theme{Text: pal.Ink85}
	set := func(c ui.Color, kinds ...editor.TokenKind) {
		for _, k := range kinds {
			t.Tokens[k].Color = c
		}
	}
	set(pal.CodeKeyword, editor.TokenKeyword, editor.TokenMeta)
	set(pal.CodeFunction, editor.TokenFunction, editor.TokenType, editor.TokenTag, editor.TokenHeading)
	set(pal.CodeString, editor.TokenString, editor.TokenEscape, editor.TokenRegexp, editor.TokenLink, editor.TokenCode)
	set(pal.CodeNumber, editor.TokenNumber, editor.TokenConstant, editor.TokenAttribute)
	set(pal.CodeComment, editor.TokenComment)
	t.Tokens[editor.TokenComment].Italic = true
	set(pal.ContentDone, editor.TokenInserted)
	set(pal.ContentFailed, editor.TokenDeleted)
	return t
}

// block builds the blocks that MyGo's options cannot style; false leaves block i to MyGo.
func (ch *Chat) block(c *ui.Context, d *parser.Document, i int) bool {
	b := d.Block(i)
	src := d.Source()
	switch b.Kind {
	case parser.Quote:
		r, ok := RemarkOf(quoteText(src[b.Start:b.End]))
		if ok {
			ch.remark(c, r)
		}
		return ok
	case parser.Table:
		ch.table(c, d, i)
		return true
	case parser.List:
		// The lines of a list in a quote carry the quote's markers: MyGo builds it.
		if inQuote(d, i) || !b.Ordered && !hasTask(d, i) {
			return false
		}
		ch.mdList(c, d, i)
		return true
	}
	return false
}

// remark is a 2px bar in the kind's color, the kind's word in its color and the text in the
// prose ink. The bar of an unverified remark is dashed.
func (ch *Chat) remark(c *ui.Context, r Remark) {
	pal := theme.Current()
	color := r.Kind.Color(pal)
	ui.Row(c).Label(LabelRemark + " " + r.Kind.Word()).AlignItems(ui.Stretch).Children(func() {
		bar := ui.Box(c).Label(LabelRemarkBar + " " + r.Kind.Word()).Width(remarkBar).Shrink(0)
		if r.Kind == RemarkUnverified {
			bar.Draw(func(p *ui.Painter, rc ui.Rect) {
				for y := rc.Y; y < rc.Y+rc.H; y += 7 {
					p.Fill(ui.Rect{X: rc.X, Y: y, W: rc.W, H: min(4, rc.Y+rc.H-y)}, color, 0)
				}
			})
		} else {
			bar.Background(color)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Padding(theme.Space6, theme.Space12, theme.Space7, theme.Space14).Children(func() {
			kit.Styled(ui.Text(c, r.Kind.Word()), theme.StyleLabel).FontWeight(600).TextColor(color)
			markdown.View(c, r.Body, &ch.mdNest)
		})
	})
}

// table is a table without a box or a header fill: a hairline under each row but the last, the
// header in 12px ink-45, numbers at the right in mono. It scrolls sideways when wider than its box.
func (ch *Chat) table(c *ui.Context, d *parser.Document, i int) {
	pal := theme.Current()
	b := d.Block(i)
	src := d.Source()
	cols := len(b.Align)
	var rows [][]string
	var header []bool
	for r := i + 1; r < b.Next; r = d.Block(r).Next {
		cells := make([]string, cols)
		k := 0
		for cell := r + 1; cell < d.Block(r).Next && k < cols; cell = d.Block(cell).Next {
			cells[k] = src[d.Block(cell).Start:d.Block(cell).End]
			k++
		}
		rows, header = append(rows, cells), append(header, d.Block(r).Header)
	}
	align := ColumnAligns(b.Align, rows, header)
	tracks := make([]ui.Track, cols)
	for k := range tracks {
		tracks[k] = ui.FitContent()
	}
	ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).Children(func() {
		ui.Grid(c).ColumnTracks(tracks...).Shrink(0).Role(ui.RoleTable).Children(func() {
			for r, cells := range rows {
				for k, text := range cells {
					c := c.Key(r*cols + k)
					box := ui.Column(c).MaxWidth(cellMax).Padding(cellPadY, cellPadX, cellPadY, 0).AlignItems(align[k])
					if r < len(rows)-1 {
						box.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
					}
					o := &ch.mdNest
					switch {
					case header[r]:
						o = &ch.mdHead
						box.Role(ui.RoleColumnHeader).FontWeight(500)
					case align[k] == ui.End && numericCell(text):
						o = &ch.mdNum
						box.Role(ui.RoleCell)
					default:
						box.Role(ui.RoleCell)
					}
					box.Children(func() { markdown.View(c, text, o) })
				}
			}
		})
	})
}

var numberRe = regexp.MustCompile(`^[+\-−±~≈]?[$€£]?\d[\d,_]*(\.\d+)?\s*(%|[kKMGTx×smh]|ms|KB|MB|GB|px)?$|^\d+\s*/\s*\d+$`)

// cellValue is the text of a cell without the marks of code and emphasis.
func cellValue(s string) string { return strings.Trim(strings.TrimSpace(s), "`*_") }

// numericCell tells a cell that is a number: "212", "+64", "−12", "3.5%", "2 / 3".
func numericCell(s string) bool { return numberRe.MatchString(cellValue(s)) }

// ColumnAligns is where the cells of each column sit: as the delimiter row says, and at the
// right in a column without one whose body cells are all numbers or dashes, one at least a number.
func ColumnAligns(given []parser.Align, rows [][]string, header []bool) []ui.Align {
	out := make([]ui.Align, len(given))
	for k, a := range given {
		switch a {
		case parser.AlignLeft:
			out[k] = ui.Start
		case parser.AlignCenter:
			out[k] = ui.Center
		case parser.AlignRight:
			out[k] = ui.End
		default:
			out[k] = ui.Start
			if numericColumn(rows, header, k) {
				out[k] = ui.End
			}
		}
	}
	return out
}

func numericColumn(rows [][]string, header []bool, k int) bool {
	numbers := 0
	for r, cells := range rows {
		if header[r] || k >= len(cells) {
			continue
		}
		switch v := cellValue(cells[k]); {
		case numericCell(v):
			numbers++
		case v != "" && v != "—" && v != "–" && v != "-":
			return false
		}
	}
	return numbers > 0
}

// inQuote tells that block i is inside a quote.
func inQuote(d *parser.Document, i int) bool {
	for p := d.Block(i).Parent; p > 0; p = d.Block(p).Parent {
		if d.Block(p).Kind == parser.Quote {
			return true
		}
	}
	return false
}

// hasTask tells that an item of list i has a check box.
func hasTask(d *parser.Document, i int) bool {
	for j := i + 1; j < d.Block(i).Next; j = d.Block(j).Next {
		if d.Block(j).Task != parser.NoTask {
			return true
		}
	}
	return false
}

// mdList is a numbered list with its numbers in mono, or a list with task items: a check for a
// done item, a ring for an open one.
func (ch *Chat) mdList(c *ui.Context, d *parser.Document, i int) {
	pal := theme.Current()
	b := d.Block(i)
	src := d.Source()
	spacing := float32(theme.Space12)
	if b.Tight {
		spacing = theme.Space4
	}
	width := float32(markerSize)
	if b.Ordered {
		last := b.Number + max(0, countItems(d, i)-1)
		w, _ := c.MeasureText(0, ui.Span{Text: strconv.Itoa(last) + string(b.Marker), Font: theme.FontFamily, Size: theme.StyleMeta.Size})
		width = max(width, w)
	}
	ui.Column(c).Role(ui.RoleList).Children(func() {
		n := b.Number
		for j := i + 1; j < b.Next; j = d.Block(j).Next {
			item := d.Block(j)
			c := c.Key(j)
			row := ui.Row(c).AlignItems(ui.Start).Gap(theme.Space8).Role(ui.RoleListItem)
			if j > i+1 {
				row.Margin(spacing, 0, 0, 0)
			}
			num := n
			row.Children(func() {
				marker := ui.Box(c).Width(width).Shrink(0).Unselectable()
				marker.Children(func() { itemMarker(c, pal, b, item, num) })
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					markdown.View(c, ItemText(src, item), &ch.mdNest)
				})
			})
			n++
		}
	})
}

// itemMarker is the mark of an item: its check or ring, its number or a bullet.
func itemMarker(c *ui.Context, pal *theme.Palette, list, item *parser.Block, n int) {
	line := theme.StyleProse.Line
	switch {
	case item.Task != parser.NoTask:
		checked := item.Task == parser.Checked
		ink := pal.Ink45
		if checked {
			ink = pal.Ink85
		}
		ui.Box(c).Size(markerSize, markerSize).Margin((line-markerSize)/2, 0, 0, 0).Role(ui.RoleCheckBox).Checked(checked).
			Draw(func(p *ui.Painter, r ui.Rect) {
				p.Stroke(ui.Rect{X: r.X + 1.5, Y: r.Y + 1.5, W: r.W - 3, H: r.H - 3}, ink, (r.W-3)/2, 1.3)
				if checked {
					path := &ui.Path{}
					path.MoveTo(r.X+r.W*0.34, r.Y+r.H*0.51).LineTo(r.X+r.W*0.46, r.Y+r.H*0.63).LineTo(r.X+r.W*0.66, r.Y+r.H*0.41)
					p.StrokePath(path, 1.3, ink)
				}
			})
	case list.Ordered:
		kit.Styled(ui.Text(c, strconv.Itoa(n)+string(list.Marker)), theme.StyleMeta).NoWrap().
			Margin((line-theme.StyleMeta.Line)/2, 0, 0, 0).TextColor(pal.Ink45)
	default:
		kit.Styled(ui.Text(c, "•"), theme.StyleProse).TextColor(pal.Ink45)
	}
}

func countItems(d *parser.Document, i int) int {
	n := 0
	for j := i + 1; j < d.Block(i).Next; j = d.Block(j).Next {
		n++
	}
	return n
}

// ItemText is the source of a list item without its marker and its check box, its lines moved
// back by the width of the marker, so that it reads as Markdown of its own.
func ItemText(src string, item *parser.Block) string {
	indent := item.Start - lineStart(src, item.Start)
	first, rest, _ := strings.Cut(src[item.Start:item.End], "\n")
	m := len(first) - len(strings.TrimLeft(first, " "))
	for m < len(first) && first[m] >= '0' && first[m] <= '9' {
		m++
	}
	if m < len(first) && strings.IndexByte("-*+.)", first[m]) >= 0 {
		m++
	}
	sp := len(first[m:]) - len(strings.TrimLeft(first[m:], " "))
	if sp == 0 || sp > 4 {
		// No text on the line, or indented code after the marker: the text starts one space after it.
		sp = min(sp, 1)
	}
	col := indent + m + max(sp, 1)
	body := first[m+sp:]
	if item.Task != parser.NoTask && len(body) >= 3 && body[0] == '[' && body[2] == ']' {
		body = strings.TrimLeft(body[3:], " ")
	}
	if rest == "" {
		return body
	}
	lines := strings.Split(rest, "\n")
	for k, l := range lines {
		cut := min(col, len(l)-len(strings.TrimLeft(l, " ")))
		lines[k] = l[cut:]
	}
	return body + "\n" + strings.Join(lines, "\n")
}

// DiffLineKind is the kind of a line of a diff block.
type DiffLineKind int

// The kinds of diff lines.
const (
	DiffContext DiffLineKind = iota
	DiffAdded
	DiffRemoved
	DiffHunk
	DiffHeader
)

var diffKindNames = [...]string{"context", "added", "removed", "hunk", "header"}

// DiffKind reads the kind of a line of a diff.
func DiffKind(line string) DiffLineKind {
	switch {
	case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
		return DiffHeader
	case strings.HasPrefix(line, "@@"):
		return DiffHunk
	case strings.HasPrefix(line, "+"):
		return DiffAdded
	case strings.HasPrefix(line, "-"):
		return DiffRemoved
	}
	return DiffContext
}

// diffBlock is a diff or patch code block: the language and Copy above its lines, an added line
// in content-done and a removed one in content-failed, each on a tint of its color. It scrolls
// sideways as other code blocks do.
func (ch *Chat) diffBlock(c *ui.Context, b *markdown.CodeBlockView) {
	pal := theme.Current()
	code := strings.TrimSuffix(b.Code(), "\n")
	pad := float32(theme.Space12)
	box := ui.Column(c).Background(pal.Hover).Border(1, pal.Hairline).Radius(theme.Radius8).AlignSelf(ui.Stretch).Clip().
		Role(ui.RoleGroup).Label("Code, " + b.Language)
	box.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(theme.Space4, theme.Space6, 0, pad).Children(func() {
			kit.Styled(ui.Text(c, b.Language), theme.StyleMeta).TextColor(pal.Ink45).Unselectable()
			ui.Spacer(c)
			b.CopyButton(c)
		})
		lines := strings.Split(code, "\n")
		lineH := theme.StyleTerminal.Line
		// The tints lie under the scroll, as wide as the block: the lines in the scroll are only as
		// wide as the longest one.
		ui.Box(c).AlignSelf(ui.Stretch).Children(func() {
			ui.Column(c).Absolute().Left(0).Right(0).Top(theme.Space2).Children(func() {
				for k, line := range lines {
					kind := DiffKind(line)
					tint := ui.Box(c.Key(k)).Label(LabelDiffLine + " " + diffKindNames[kind]).Height(lineH).AlignSelf(ui.Stretch)
					if ink, ok := diffInk(pal, kind); ok {
						tint.Background(theme.DiffTint(ink))
					}
				}
			})
			ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).Children(func() {
				ui.Column(c).Shrink(0).Padding(theme.Space2, pad, pad, pad).Children(func() {
					for k, line := range lines {
						ink, _ := diffInk(pal, DiffKind(line))
						if line == "" {
							line = " "
						}
						kit.Styled(ui.Text(c.Key(k), line), theme.StyleTerminal).NoWrap().Height(lineH).
							LineHeight(lineH / theme.StyleTerminal.Size).TextColor(ink)
					}
				})
			})
		})
	})
}

// diffInk is the ink of a diff line of kind k, and whether its line has a tint.
func diffInk(pal *theme.Palette, k DiffLineKind) (ui.Color, bool) {
	switch k {
	case DiffAdded:
		return pal.ContentDone, true
	case DiffRemoved:
		return pal.ContentFailed, true
	case DiffHunk:
		return pal.ContentInfo, false
	}
	return pal.Ink62, false
}
