package app

// The Report tab's view: the RESULT.md of the selected work row, drawn one
// block per line. The file is read on a goroutine. A read starts only when
// the row's path or the board revision changes. Nothing here polls, sets a
// timer or watches the file. Only the finished read reaches the view, through
// the onChange function. Ported from the Rust contr module board_report.

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

// Fixed lines of the body.
const (
	// ReportNoRow is the line when no row is picked for the report.
	ReportNoRow = "Pick a work row to see its report."
	// ReportRowGone is the line when the picked row is no longer on the board.
	ReportRowGone = "That row is no longer on the board."
	// ReportNone is the line when the picked row has no report yet.
	ReportNone = "No report yet."
	// ReportLoading is the line while a read is in flight and no text is shown.
	ReportLoading = "loading report…"
	// ReportEmpty is the line when the report has no lines.
	ReportEmpty = "The report is empty."
	// ReportCutLine is the line after a report that was cut.
	ReportCutLine = "report cut · Open in editor for the rest"
)

// Labels of the Report tab parts. Tests find them. A drawn line has the
// label "report-line-<index>" and its block "report-<kind>-<index>".
const (
	LabelReport        = "report"
	LabelReportHead    = "report-head"
	LabelReportTitle   = "report-title"
	LabelReportOpen    = "report-open"
	LabelReportBack    = "report-back"
	LabelReportMeta    = "report-meta"
	LabelReportProblem = "report-problem"
	LabelReportBody    = "report-body"
	LabelReportEmpty   = "report-empty"
	LabelReportCut     = "report-cut"
)

// Texts of the two head actions.
const (
	openInEditor = "Open in editor"
	backAction   = "back"
)

// OpenPathFn opens a file with the system's default app. The app uses
// [OpenInSystem]; tests record paths.
type OpenPathFn func(c *ui.Context, path string)

// OpenInSystem opens path with the system's default app for its type.
func OpenInSystem(c *ui.Context, path string) {
	c.OpenURL((&url.URL{Scheme: "file", Path: path}).String())
}

// TargetKind says which target the Report tab shows.
type TargetKind uint8

const (
	// TargetNone is no row chosen for the report.
	TargetNone TargetKind = iota
	// TargetGone is a chosen row key that is no longer on the board.
	TargetGone
	// TargetRow is a work row; see [ReportTarget.Source].
	TargetRow
)

// ReportSource holds the fields of a work row that the Report tab shows.
type ReportSource struct {
	Key      string
	Label    string
	Title    string
	Report   *string
	ReportBy *string
}

// SourceFromRow copies the fields of row that the view shows.
func SourceFromRow(row board.WorkRow) ReportSource {
	return ReportSource{Key: row.Key, Label: row.Label, Title: row.Title, Report: row.Report, ReportBy: row.ReportBy}
}

// ReportTarget is what the Report tab shows; the board panel builds it from
// the board. Source is set only for TargetRow.
type ReportTarget struct {
	Kind   TargetKind
	Source ReportSource
}

// BodyKind says what the body shows.
type BodyKind uint8

const (
	// BodyEmpty is a fixed line, in Line.
	BodyEmpty BodyKind = iota
	// BodyLoading is the loading line.
	BodyLoading
	// BodyFailed is the first read failing: the error line, in Line.
	BodyFailed
	// BodyText is the text of the file. Problem is set when a re-read
	// failed; the old text is kept.
	BodyText
)

// ReportBody is what the body shows.
type ReportBody struct {
	Kind    BodyKind
	Line    string
	Text    *board.ReportText // shared: do not change it
	Problem string
}

type readKey struct {
	path string
	rev  uint64
}

// ReportView is the Report tab's view. The parent sets what it shows with
// [ReportView.Show]. It is safe to use from several goroutines.
type ReportView struct {
	// Open opens a file; nil means [OpenInSystem].
	Open OpenPathFn
	// OnBack runs when the owner chooses `back`. It may be nil.
	OnBack func()

	onChange func()
	// read reads a report; tests replace it to hold a read in flight.
	read func(path string) (board.ReportText, error)

	mu     sync.Mutex
	target ReportTarget
	body   ReportBody
	// key is the path and revision of the last read started. The same key
	// starts no new read. hasKey is false when there is none.
	key    readKey
	hasKey bool
	// reads counts reads started. Tests read it.
	reads int
	wg    sync.WaitGroup
}

// NewReportView returns a view that shows no row. onChange runs, on the
// goroutine that changed the view, when what is shown changed; it should only
// ask the window to redraw.
func NewReportView(onChange func()) *ReportView {
	return &ReportView{onChange: onChange, read: board.ReadReport, body: ReportBody{Kind: BodyEmpty, Line: ReportNoRow}}
}

// Show shows target for board revision rev. A read starts on a goroutine only
// when the valid path or rev differs from the last read. Call it from an
// event handler or a change callback, not while the view is built.
func (v *ReportView) Show(target ReportTarget, rev uint64) {
	v.mu.Lock()
	beforeT, beforeB := v.target, v.body
	if path, ok := readablePath(target); ok {
		v.target = target
		v.followLocked(path, rev)
	} else {
		v.body = idleBody(target)
		v.hasKey = false
		v.target = target
	}
	changed := !sameShown(beforeT, beforeB, v.target, v.body)
	v.mu.Unlock()
	if changed {
		v.notify()
	}
}

// followLocked shows the file at path for revision rev. The text of the same
// path stays on screen while the new read runs.
func (v *ReportView) followLocked(path string, rev uint64) {
	key := readKey{path, rev}
	if v.hasKey && v.key == key {
		return
	}
	keepText := v.hasKey && v.key.path == path && v.body.Kind == BodyText
	if !keepText {
		v.body = ReportBody{Kind: BodyLoading}
	}
	v.reads++
	v.key, v.hasKey = key, true
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()
		text, err := v.read(path)
		v.loaded(key, text, err)
	}()
}

// loaded applies a finished read. The result of a read that is no longer the
// current one is dropped.
func (v *ReportView) loaded(key readKey, text board.ReportText, err error) {
	v.mu.Lock()
	if !v.hasKey || v.key != key {
		v.mu.Unlock()
		return
	}
	before := v.body
	switch {
	case err == nil && len(text.Lines) == 0:
		v.body = ReportBody{Kind: BodyEmpty, Line: ReportEmpty}
	case err == nil:
		v.body = ReportBody{Kind: BodyText, Text: &text}
	case v.body.Kind == BodyText:
		// A text body is always for the path of the current read, so it stays.
		v.body.Problem = err.Error()
	default:
		v.body = ReportBody{Kind: BodyFailed, Line: err.Error()}
	}
	changed := !reflect.DeepEqual(before, v.body)
	v.mu.Unlock()
	if changed {
		v.notify()
	}
}

func (v *ReportView) notify() {
	if v.onChange != nil {
		v.onChange()
	}
}

func sameShown(t1 ReportTarget, b1 ReportBody, t2 ReportTarget, b2 ReportBody) bool {
	return reflect.DeepEqual(t1, t2) && reflect.DeepEqual(b1, b2)
}

// state takes the target and the body in one lock.
func (v *ReportView) state() (ReportTarget, ReportBody) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.target, v.body
}

// openInEditor opens the shown file with the app for its type. It does
// nothing without a valid path.
func (v *ReportView) openInEditor(c *ui.Context) {
	target, _ := v.state()
	path, ok := readablePath(target)
	if !ok {
		return
	}
	open := v.Open
	if open == nil {
		open = OpenInSystem
	}
	open(c, path)
}

// back tells the parent to show the Board tab.
func (v *ReportView) back() {
	if v.OnBack != nil {
		v.OnBack()
	}
}

// readablePath is the path to read for target: a row's report when it is a
// valid path.
func readablePath(t ReportTarget) (string, bool) {
	if t.Kind != TargetRow || t.Source.Report == nil {
		return "", false
	}
	return board.ReportPath(*t.Source.Report)
}

// idleBody is what the body shows for a target with no file to read.
func idleBody(t ReportTarget) ReportBody {
	switch {
	case t.Kind == TargetNone:
		return ReportBody{Kind: BodyEmpty, Line: ReportNoRow}
	case t.Kind == TargetGone:
		return ReportBody{Kind: BodyEmpty, Line: ReportRowGone}
	case t.Source.Report == nil:
		return ReportBody{Kind: BodyEmpty, Line: ReportNone}
	default:
		// Only a report path that ReportPath rejects gets here.
		return ReportBody{Kind: BodyFailed, Line: board.BadReportPath}
	}
}

// titleText is the label, two spaces, then the title. A part that is empty is
// left out.
func titleText(s ReportSource) string {
	var parts []string
	for _, p := range []string{s.Label, s.Title} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "  ")
}

// ariaLabel is the accessible name of the view: "report <label> <title>, by
// <report_by>", with the missing parts left out.
func ariaLabel(t ReportTarget) string {
	if t.Kind != TargetRow {
		return LabelReport
	}
	name := LabelReport
	for _, p := range []string{t.Source.Label, t.Source.Title} {
		if p != "" {
			name += " " + p
		}
	}
	if t.Source.ReportBy != nil {
		name += ", by " + *t.Source.ReportBy
	}
	return name
}

// lineText is the text of one drawn line of the body. An item keeps its
// marker, a rule reads as "---" and a blank line is empty.
func lineText(l board.ReportLine) string {
	switch l.Kind {
	case board.LineItem:
		return l.Marker + " " + l.Text
	case board.LineRule:
		return "---"
	case board.LineBlank:
		return ""
	default:
		return l.Text
	}
}

// View builds the tab.
func (v *ReportView) View(c *ui.Context) {
	target, body := v.state()
	_, open := readablePath(target)
	var by *string
	if target.Kind == TargetRow {
		by = target.Source.ReportBy
	}
	problem := body.Problem
	ui.Column(c).Label(ariaLabel(target)).Role(ui.RoleGroup).Fill().PaddingX(theme.PanelPadX).Children(func() {
		v.head(c, target, open)
		metaLine(c, by, problem)
		ui.Box(c.Key("divider")).FillWidth().Height(1).Shrink(0).Background(theme.Color(theme.Line))
		v.drawBody(c, body)
	})
}

// head: the title on the left; "Open in editor" (with a valid path) and "back"
// on the right. Without a row the title is an empty spacer, so "back" stays at
// the right edge.
func (v *ReportView) head(c *ui.Context, target ReportTarget, open bool) {
	ui.Row(c).Label(LabelReportHead).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.ReportGap).Children(func() {
		if target.Kind == TargetRow {
			ui.Row(c.Key("title")).Label(LabelReportTitle).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, titleText(target.Source)).Bold().SingleLine().Ellipsis("…").MinWidth(0).
					TextColor(theme.Color(theme.Text))
			})
		} else {
			ui.Box(c.Key("title")).Grow(1).MinWidth(0)
		}
		ui.Row(c.Key("actions")).Shrink(0).AlignItems(ui.Center).Gap(theme.ReportGap).Children(func() {
			if open {
				headAction(c, LabelReportOpen, openInEditor, func() { v.openInEditor(c) })
			}
			headAction(c, LabelReportBack, backAction, v.back)
		})
	})
}

// headAction is a text action in the head: dim, lit on hover, and a button
// for the reader.
func headAction(c *ui.Context, label, text string, onClick func()) {
	el := ui.ButtonBase(c.Key(label)).Label(label).Cursor(ui.CursorPointer).OnClick(onClick)
	color := uint32(theme.TextDim)
	if el.Hovered() {
		color = theme.Text
	}
	el.TextColor(theme.Color(color)).Children(func() { ui.Text(c, text) })
}

// metaLine is the line under the head: "by <report_by>", then " · <problem>" when
// a re-read failed. Absent when there is neither.
func metaLine(c *ui.Context, by *string, problem string) {
	if by == nil && problem == "" {
		return
	}
	ui.Row(c).Label(LabelReportMeta).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).TextColor(theme.Color(theme.TextDim)).Children(func() {
		if by != nil {
			ui.Text(c, "by "+*by).NoWrap()
		}
		if problem != "" {
			if by != nil {
				ui.Text(c, " · ").NoWrap()
			}
			ui.Row(c.Key("problem")).Label(LabelReportProblem).Children(func() { ui.Text(c, problem) })
		}
	})
}

// drawBody is the drawn lines in a scrolling column, or the one line that
// replaces them.
func (v *ReportView) drawBody(c *ui.Context, body ReportBody) {
	ui.Scroll(c).Label(LabelReportBody).Grow(1).MinHeight(0).FillWidth().Padding(theme.ReportBodyPadTop, 0, 0).Children(func() {
		switch body.Kind {
		case BodyText:
			for i, line := range body.Text.Lines {
				drawLine(c, i, line)
			}
			if body.Text.Cut {
				note(c, LabelReportCut, ReportCutLine, theme.TextDim2)
			}
		case BodyLoading:
			note(c, LabelReportEmpty, ReportLoading, theme.TextDim)
		default:
			note(c, LabelReportEmpty, body.Line, theme.TextDim)
		}
	})
}

// note is a fixed line in the body, in color.
func note(c *ui.Context, label, text string, color uint32) {
	ui.Row(c.Key(label)).Label(label).FillWidth().Shrink(0).Children(func() {
		ui.Text(c, text).TextColor(theme.Color(color))
	})
}

// drawLine draws one line of the body, one block per line. The wrapper is
// "report-line-<index>"; the block in it is "report-<kind>-<index>".
func drawLine(c *ui.Context, index int, line board.ReportLine) {
	idx := strconv.Itoa(index)
	ui.Column(c.Key("line-" + idx)).Label("report-line-" + idx).FillWidth().Shrink(0).Children(func() {
		switch line.Kind {
		case board.LineHeading:
			blockHeading(c, index, line)
		case board.LineItem:
			blockItem(c, index, line)
		case board.LineCode:
			blockCode(c, index, line.Text)
		case board.LineQuote:
			blockQuote(c, index, line.Text)
		case board.LineRule:
			ui.Box(c.Key("rule")).Label("report-rule-" + idx).FillWidth().Height(1).Shrink(0).
				MarginY(theme.ReportRuleMargin).Background(theme.Color(theme.Line))
		case board.LineBlank:
			ui.Box(c.Key("blank")).Label("report-blank-" + idx).FillWidth().Height(theme.ReportBlankHeight).Shrink(0)
		default:
			ui.Box(c.Key("text")).Label("report-text-" + idx).FillWidth().Children(func() {
				ui.Text(c, line.Text).TextColor(theme.Color(theme.Text2))
			})
		}
	})
}

// blockHeading is bold. Levels 1 and 2 get a gap above them, except on the first
// line.
func blockHeading(c *ui.Context, index int, line board.ReportLine) {
	el := ui.Box(c.Key("heading")).Label("report-heading-" + strconv.Itoa(index)).FillWidth()
	if index > 0 && line.Level <= 2 {
		el = el.Margin(theme.ReportBlockGap, 0, 0)
	}
	el.Children(func() { ui.Text(c, line.Text).Bold().TextColor(theme.Color(theme.Text)) })
}

// blockItem is the marker in its column, then the text, indented by depth.
func blockItem(c *ui.Context, index int, line board.ReportLine) {
	ui.Row(c.Key("item")).Label("report-item-" + strconv.Itoa(index)).FillWidth().MinHeight(theme.LineHeight).
		AlignItems(ui.Start).PaddingStart(float32(line.Depth) * theme.ReportIndent).Children(func() {
		ui.Text(c, line.Marker).Width(theme.ReportMarkerWidth).Shrink(0).NoWrap().TextColor(theme.Color(theme.TextDim))
		ui.Text(c, line.Text).Grow(1).MinWidth(0).TextColor(theme.Color(theme.Text2))
	})
}

// blockCode is one line high on the code background, clipped at the panel edge.
// The owner opens the editor for the rest.
func blockCode(c *ui.Context, index int, text string) {
	ui.Row(c.Key("code")).Label("report-code-" + strconv.Itoa(index)).FillWidth().Height(theme.LineHeight).Shrink(0).
		PaddingX(theme.ReportCodePadX).Background(theme.Color(theme.ReportCodeBg)).ClipX().Children(func() {
		ui.Text(c, text).NoWrap().TextColor(theme.Color(theme.Text2))
	})
}

// blockQuote is dim, with a rule on the left.
func blockQuote(c *ui.Context, index int, text string) {
	ui.Box(c.Key("quote")).Label("report-quote-" + strconv.Itoa(index)).FillWidth().MinHeight(theme.LineHeight).
		BorderStartWidth(1).BorderColor(theme.Color(theme.Line)).PaddingStart(theme.ReportQuotePad).Children(func() {
		ui.Text(c, text).TextColor(theme.Color(theme.TextDim))
	})
}
