package app

// The view of the questions card (docs/spec/QuestionsCard.md). It builds a
// PopoverBase from the notch: chips, the item, Details and the keys line.
// Colors and type come from the theme tokens.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// questionsVia is the text at the right of the chips: questions and to-dos reach the
// owner through the orchestrator.
const questionsVia = "via orchestrator"

// styled sets the font of a text from a type style.
func styled(t ui.Element, st theme.TextStyle) ui.Element {
	return t.Font(st.Stack()).FontSize(st.Size).FontWeight(st.Weight)
}

// View builds the card for items, which must be the open items of the
// project, anchored to the notch. It builds nothing while the card is closed.
func (q *QuestionsCard) View(c *ui.Context, anchor ui.Element, items []NeedTab) {
	if !q.open || len(items) == 0 || q.Source == nil {
		return
	}
	snap := q.Source.Snapshot()
	idx := q.sync(items)
	t := items[idx]
	if t.Kind == NeedBlocked {
		q.watchBlocked(snap, t)
	}
	p := theme.Current()
	width := float32(theme.SizeNotchOpenWidth)
	if w, _ := c.Size(); float32(w)-2*theme.Space12 < width {
		width = float32(w) - 2*theme.Space12
	}
	shown := true
	pop := ui.PopoverBase(c, anchor, &shown, func(panel ui.Element) {
		panel = panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorTop).Role(ui.RoleDialog).Label(q.name(t, snap)).
			Column().Width(width).Radius(0, 0, theme.Radius12, theme.Radius12).
			Material(glass.Blur{Radius: theme.GlassBlur}).Focusable().FocusRing(false).Bind(&q.handle)
		for _, l := range p.ShadowNotch {
			if l.Inset {
				panel = panel.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			} else {
				panel = panel.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			}
		}
		// The keys act only with the focus in the card. The field takes its
		// own keys first, so a number typed in it stays text.
		panel = panel.OnShortcut(0, ui.KeyEnter, func() { q.enter(t, snap) })
		panel = panel.OnShortcut(0, ui.KeyTab, func() { q.next(items) })
		for pos, key := range keyOptions {
			panel = panel.OnShortcut(0, key, func() { q.digit(t, snap, pos) })
		}
		if q.focus {
			q.focus = false
			q.handle.Focus(c)
		}
		// float-solid, so the rows behind do not show through the text (D212), with
		// float-line on the sides and the bottom.
		panel.Children(func() {
			// Selectable: the owner can select and copy the texts of the
			// card (D204). Buttons and the field keep their own input.
			ui.Column(c).Width(width).Padding(theme.Space12).Gap(theme.Space4).
				Radius(0, 0, theme.Radius12, theme.Radius12).Background(questionsCardFill(p)).
				BorderWidth(0, 1, 1, 1).BorderColor(p.FloatLine).Selectable().Children(func() {
				q.body(c, p, items, t, snap)
			})
		})
	})
	// Escape and a press outside close the card; MyGo returns the focus.
	if pop.Dismissed() {
		q.Close()
	}
}

// questionsCardFill is the fill of the open questions card: float-solid (#524, D212). The
// closed notch keeps glass-float.
func questionsCardFill(p *theme.Palette) ui.Color { return p.FloatSolid }

// body builds what the card holds: the chips, the item, the failure and the keys.
func (q *QuestionsCard) body(c *ui.Context, p *theme.Palette, items []NeedTab, t NeedTab, snap agentstate.Snapshot) {
	q.chips(c, p, items, t)
	switch t.Kind {
	case NeedQuestion:
		q.questionBody(c, p, t, snap)
	case NeedTodo:
		q.todoBody(c, p, t, snap)
	case NeedBlocked:
		q.blockedBody(c, p, t, snap)
	}
	if msg := q.foot(snap); msg != "" {
		styled(ui.Text(c, msg), theme.StyleSmall).Padding(0, theme.Space12).TextColor(p.Ink62)
	}
	q.keys(c, p, t, snap, len(items))
}

// chipLabel is the text of a chip: the title of the item without the
// backticks of its code spans. A blocked worker that gave no message shows
// its name.
func chipLabel(t NeedTab) string {
	if t.Title == "" {
		return t.Key
	}
	return plainText(t.Title)
}

// name is the name of the card for assistive technology: the question,
// without the backticks of its code spans.
func (q *QuestionsCard) name(t NeedTab, snap agentstate.Snapshot) string {
	return plainText(q.rawName(t, snap))
}

// rawName is the question of the card as the agent wrote it.
func (q *QuestionsCard) rawName(t NeedTab, snap agentstate.Snapshot) string {
	switch t.Kind {
	case NeedQuestion:
		if o, ok := q.question(snap); ok {
			return o.Question
		}
	case NeedTodo:
		if o, ok := q.todo(snap); ok {
			return o.Text
		}
	case NeedBlocked:
		if q.menu != nil && q.menu.Question != "" {
			return q.menu.Question
		}
	}
	return t.Title
}

// foot is the failure of the last action, else the problem of the state.
func (q *QuestionsCard) foot(snap agentstate.Snapshot) string {
	if q.err != "" {
		return q.err
	}
	return snap.Problem
}

// chips builds the head: the glyph, a chip for each item and "via
// orchestrator" at the right.
func (q *QuestionsCard) chips(c *ui.Context, p *theme.Palette, items []NeedTab, t NeedTab) {
	sel := q.idx
	// The glyph and "via orchestrator" stay on the first line of the chips.
	ui.Row(c).AlignItems(ui.Start).Gap(theme.Space8).Children(func() {
		ui.Box(c).Shrink(0).Margin(theme.Space4, 0, 0, 0).Children(func() {
			kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphNeedsYou, Small: true})
		})
		seg := ui.SegmentedBase(c.Key("questions-chips"), &sel, len(items))
		seg.Track.Label(LabelQuestionsChips).Grow(1).MinWidth(0).Wrap().Gap(theme.Space4).Children(func() {
			for i, it := range items {
				s := seg.Segment(i).Label(chipLabel(it)).Padding(3, 9).Radius(theme.Radius6).Cursor(ui.CursorPointer).MaxWidth(220)
				ink := p.Ink45
				if i == sel {
					ink = p.Ink100
					s = s.Background(p.Selected)
					for _, l := range p.ShadowSelected {
						s = s.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
					}
				}
				s.Children(func() { styled(ui.Text(c, chipLabel(it)), theme.StyleChip).SingleLine().TextColor(ink) })
			}
		})
		if t.Kind != NeedBlocked {
			styled(ui.Text(c, questionsVia), theme.StyleSmall).NoWrap().Shrink(0).Margin(theme.Space4, 0, 0, 0).TextColor(p.Ink35)
		}
	})
	if sel != q.idx {
		q.show(items, sel)
	}
}

// questionBody builds a question: the text, the context line, the choices and
// the field, then Details.
func (q *QuestionsCard) questionBody(c *ui.Context, p *theme.Palette, t NeedTab, snap agentstate.Snapshot) {
	o, ok := q.question(snap)
	if !ok {
		styled(ui.Text(c, plainText(t.Title)), theme.StyleQuestion).Padding(2, theme.Space12).TextColor(p.Ink100)
		return
	}
	q.heading(c, p, o.Question, contextLine(o.From))
	for i, opt := range CardOptions(o) {
		if i >= len(keyOptions) {
			break // the keys reach nine choices; more would show with no key
		}
		q.choice(c, p, i, opt.Text, opt.Recommended, opt.Impact, i == q.picked, func() { q.answer(t.Key, opt.Text) })
	}
	q.field(c, p, t)
	if len(o.Refs) > 0 || o.Source != "" {
		q.detailsSection(c, p, o.Refs, o.Source)
	}
}

// contextLine says who asked. The title is already the chip.
func contextLine(from string) string {
	var parts []string
	if from != "" {
		parts = append(parts, "from "+from)
	}
	return strings.Join(parts, " · ")
}

// heading builds the question line and the context line under it.
func (q *QuestionsCard) heading(c *ui.Context, p *theme.Palette, question, context string) {
	q.codeText(c, p, question, theme.StyleQuestion).Padding(2, theme.Space12).TextColor(p.Ink100)
	if context != "" {
		styled(ui.Text(c, context), theme.StyleSmall).Wrap().Padding(0, theme.Space12).TextColor(p.Ink42)
	}
}

// choice builds a choice row: a number, the label with the recommended tag,
// and the impact under it (D125).
func (q *QuestionsCard) choice(c *ui.Context, p *theme.Palette, i int, label string, recommended bool, impact string, picked bool, click func()) {
	b := ui.ButtonBase(c.Key("questions-choice-"+strconv.Itoa(i))).Role(ui.RoleRadio).Label(label).Description(impact).
		Selected(picked).FillWidth().Shrink(0).Row().AlignItems(ui.Start).Gap(theme.Space12).Padding(9, theme.Space12).
		Radius(theme.Radius9).Cursor(ui.CursorPointer).FocusRing(true).OnClick(click)
	if picked || b.Hovered() {
		b = b.Background(p.Hover)
	}
	b.Children(func() {
		styled(ui.Text(c, strconv.Itoa(i+1)), theme.StyleMeta).NoWrap().Shrink(0).Width(theme.Space12).TextColor(p.Ink32)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
				styled(ui.Text(c, label), theme.StyleBody).Wrap().TextColor(p.Ink90)
				if recommended {
					styled(ui.Text(c, "recommended"), theme.StyleMeta).NoWrap().Shrink(0).TextColor(p.Ink32)
				}
			})
			if impact != "" {
				styled(ui.Text(c, impact), theme.StyleSmall).Wrap().TextColor(p.Ink42)
			}
		})
	})
}

// field builds the field for a free answer.
func (q *QuestionsCard) field(c *ui.Context, p *theme.Palette, t NeedTab) {
	in := ui.TextInputBase(c.Key("questions-field"), &q.text).Label(LabelQuestionsField).Placeholder(LabelQuestionsField).
		PlaceholderColor(p.Ink30).TextColor(p.Ink90).Height(30).FillWidth().Shrink(0).PaddingX(theme.Space12).
		Radius(theme.Radius8).Border(1, p.InputLine).Margin(theme.Space4, 0, 0, 0)
	if in.Submitted() {
		q.answer(t.Key, q.text)
	}
}

// detailsSection builds Details, closed at first, with the refs and the
// source excerpt (D114).
func (q *QuestionsCard) detailsSection(c *ui.Context, p *theme.Palette, refs []string, source string) {
	col := ui.CollapsibleBase(c.Key("questions-details"), &q.details)
	col.Trigger.Label(LabelQuestionsDetails).AlignItems(ui.Center).Gap(theme.Space6).Padding(theme.Space4, theme.Space12).
		Cursor(ui.CursorPointer).Children(func() {
		arrow := "▸"
		if q.details {
			arrow = "▾"
		}
		styled(ui.Text(c, arrow), theme.StyleSmall).NoWrap().TextColor(p.Ink42)
		styled(ui.Text(c, LabelQuestionsDetails), theme.StyleSmall).NoWrap().TextColor(p.Ink42)
	})
	col.Panel(func() {
		ui.Column(c).Padding(0, theme.Space12).Gap(theme.Space4).Children(func() {
			if len(refs) > 0 {
				styled(ui.Text(c, strings.Join(refs, "  ")), theme.StyleSmall).Wrap().TextColor(p.Ink42)
			}
			if source != "" {
				q.codeText(c, p, source, theme.StyleSmall).TextColor(p.Ink42)
			}
		})
	})
}

// todoBody builds a to-do: its text and the done action.
func (q *QuestionsCard) todoBody(c *ui.Context, p *theme.Palette, t NeedTab, snap agentstate.Snapshot) {
	text := t.Title
	from := ""
	if o, ok := q.todo(snap); ok {
		text, from = o.Text, o.From
	}
	if from != "" {
		from = "from " + from
	}
	q.heading(c, p, text, from)
	b := ui.ButtonBase(c).Label(QuestionsDone).Height(26).Shrink(0).PaddingX(theme.Space12).AlignItems(ui.Center).
		Radius(theme.Radius8).Border(1, p.InputLine).Cursor(ui.CursorPointer).Margin(theme.Space4, 0, 0, theme.Space12).
		OnClick(func() { q.done(t.Key) })
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b.Children(func() { styled(ui.Text(c, QuestionsDone), theme.StyleBody).NoWrap().TextColor(p.Ink100) })
}

// blockedBody builds the menu of a blocked worker: the detail, the question
// and the choices. Without a menu it shows the worker's message and Focus
// worker.
func (q *QuestionsCard) blockedBody(c *ui.Context, p *theme.Palette, t NeedTab, snap agentstate.Snapshot) {
	if q.menu == nil {
		q.heading(c, p, t.Title, "")
	} else {
		// The detail first: for a permission, the tool and its command.
		if len(q.menu.Detail) > 0 {
			styled(ui.Text(c, strings.Join(q.menu.Detail, "\n")), theme.StyleMeta).Wrap().Padding(2, theme.Space12).TextColor(p.Ink62)
		}
		q.heading(c, p, q.menu.Question, "")
		for i, ch := range q.menu.Choices {
			if i >= len(keyOptions) {
				break
			}
			q.choice(c, p, i, ch.Label, false, "", false, func() { q.choose(t, i) })
		}
	}
	ui.Row(c).Shrink(0).Gap(theme.Space12).Padding(theme.Space4, theme.Space12, 0).Children(func() {
		link := func(label string, click func()) {
			b := ui.ButtonBase(c).Label(label).Shrink(0).Cursor(ui.CursorPointer).OnClick(click)
			b.Children(func() {
				tx := styled(ui.Text(c, label), theme.StyleSmall).NoWrap().TextColor(p.Ink62)
				if b.Hovered() {
					tx.Underline()
				}
			})
		}
		link(QuestionsFocusWorker, func() { q.focusWorker(t) })
		if w, ok := findBlocked(snap, t.Key); ok {
			if label, open := q.Entry.For(w); open != nil {
				link(label, func() { open(); q.Close() })
			}
		}
	})
}

// keys builds the keys line, with the keys the item takes.
func (q *QuestionsCard) keys(c *ui.Context, p *theme.Palette, t NeedTab, snap agentstate.Snapshot, n int) {
	var parts []string
	switch t.Kind {
	case NeedQuestion:
		if o, ok := q.question(snap); ok && len(o.Options) > 0 {
			parts = append(parts, "1-"+strconv.Itoa(min(len(o.Options), len(keyOptions)))+" pick")
		}
		parts = append(parts, "⏎ send")
	case NeedTodo:
		parts = append(parts, "⏎ done")
	case NeedBlocked:
		if q.menu != nil && len(q.menu.Choices) > 0 {
			parts = append(parts, "1-"+strconv.Itoa(min(len(q.menu.Choices), len(keyOptions)))+" send")
		}
	}
	if n > 1 {
		parts = append(parts, "⇥ next")
	}
	parts = append(parts, "esc close")
	styled(ui.Text(c, strings.Join(parts, "  ")), theme.StyleMeta).NoWrap().Padding(theme.Space6, theme.Space12, 0).TextColor(p.Ink32)
}

// codeText builds a wrapped paragraph of s in style st. Its backtick spans
// show as mono code chips without the backticks; a click on a chip copies
// its text and shows "Copied" (D204).
func (q *QuestionsCard) codeText(c *ui.Context, p *theme.Palette, s string, st theme.TextStyle) ui.Element {
	parts := splitCode(s)
	if !slices.ContainsFunc(parts, func(t textPart) bool { return t.Code }) {
		return styled(ui.Text(c, s), st).Wrap()
	}
	return styled(ui.RichText(c), st).Wrap().Children(func() {
		for _, t := range parts {
			if !t.Code {
				ui.Text(c, t.Text)
				continue
			}
			chip := ui.RichText(c, ui.Span{Text: t.Text, Font: theme.FontMono, Size: theme.StyleMeta.Size,
				Background: p.HoverStrong, Radius: theme.Radius4, PaddingX: theme.Space4, PaddingY: theme.Space2}).Label(LabelCopyCode + t.Text).Cursor(ui.CursorPointer)
			if chip.Clicked() {
				q.copyCode(c, t.Text)
			}
		}
	})
}

// copyCode puts code on the clipboard and shows "Copied".
func (q *QuestionsCard) copyCode(c *ui.Context, code string) {
	c.WriteClipboard(code)
	q.Env.Show(QuestionsCopied)
}
