package viewers

// The Files tab of the PR pane (#153, D102): the changed files on a list,
// the diff of the chosen file in a code view, and the review threads under
// their lines. An open thread has `reply` and `resolve` (#154, D103).

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// Labels of the Files tab.
const (
	LabelPRFiles  = "pr files"
	LabelPRDiff   = "pr diff"
	LabelPRNoFile = "No files."
	LabelPRBinary = "Binary file."

	LabelPRReply     = "reply"
	LabelPRResolve   = "resolve"
	LabelPRReplySend = "Reply"
	LabelPRReplyBox  = "reply text"
)

// prFileListMax is the most height the file list takes, in DIPs.
const prFileListMax = 8 * kit.RowHeight

// prThreadCols is how many characters fit on a line of a thread box. It
// only sizes the room under a line.
const prThreadCols = 56

// prFiles is the state of the Files tab. Open resets it.
type prFiles struct {
	started   bool
	loaded    bool // the read ended
	files     []github.File
	threads   []github.Thread
	err       string // files could not be read
	threadErr string // threads could not be read; the files still show
	sel       int
	shown     int // the file that diff shows, -1 for none
	list      ui.ListState
	diff      *editor.DiffState
	note      string // binary or bad patch text, instead of the diff

	replying map[string]bool    // threads whose reply field is open
	drafts   map[string]*string // typed reply text per thread; kept when a write fails
	busy     bool               // a write is in flight
	writeErr string             // the last failed write
}

// loadFiles reads the files and the threads off the UI thread. Only the newest
// read lands (D59).
func (p *PRPane) loadFiles() {
	f := &p.filesState
	f.started, f.shown = true, -1
	mk := p.Client
	if mk == nil {
		mk = github.New
	}
	client := mk(p.dir)
	n := p.number
	p.reads.Do(p.Env, func() func() {
		files, err := client.Files(context.Background(), n)
		var threads []github.Thread
		var terr error
		if err == nil {
			threads, terr = client.Threads(context.Background(), n)
		}
		return func() {
			f.loaded = true
			if err != nil {
				f.err = err.Error()
			} else {
				f.files, f.threads = files, threads
			}
			if terr != nil {
				f.threadErr = terr.Error()
			}
			p.Env.Redraw()
		}
	})
}

// filesTab builds the Files tab.
func (p *PRPane) filesTab(c *ui.Context) {
	f := &p.filesState
	if !f.started {
		p.loadFiles()
	}
	dim := func(s string) {
		ui.Text(c, s).TextColor(theme.Color(theme.TextDim)).Padding(theme.PRPanePad)
	}
	switch {
	case f.err != "":
		dim(f.err)
		return
	case !f.loaded:
		dim(LabelPRLoad)
		return
	case len(f.files) == 0:
		dim(LabelPRNoFile)
		return
	}
	if f.sel < 0 || f.sel >= len(f.files) {
		f.sel = 0
	}
	if f.shown != f.sel {
		p.showFile(f.sel)
	}
	ui.Column(c).Label(LabelPRFiles).Grow(1).MinHeight(0).FillWidth().Children(func() {
		if f.writeErr != "" {
			ui.Text(c, f.writeErr).TextColor(theme.Color(theme.Text)).Padding(6, theme.PRPanePad)
		}
		if f.threadErr != "" {
			ui.Text(c, f.threadErr).TextColor(theme.Color(theme.TextDim)).Padding(6, theme.PRPanePad)
		}
		f.list.Key = func(i int) any { return f.files[i].Path }
		f.list.Label = func(i int) string { return f.files[i].Path }
		f.list.Selected = &f.sel
		h := float32(len(f.files)) * kit.RowHeight
		ui.List(c.Key("prfilelist"), &f.list, len(f.files), func(i int) {
			fl := f.files[i]
			kit.Row(c, kit.RowOpts{Label: "file " + fl.Path, Text: fl.Path, Counts: true,
				Added: fl.Additions, Removed: fl.Deletions, Selected: i == f.sel,
				OnClick: func() { f.sel = i; p.Env.Redraw() }})
		}).FillWidth().Shrink(0).Height(min(h, prFileListMax)).Padding(4, 0)
		ui.Column(c).Label(LabelPRDiff).Grow(1).MinHeight(0).FillWidth().
			BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
			switch {
			case f.note != "":
				ui.Text(c, f.note).TextColor(theme.Color(theme.TextDim)).Padding(theme.PRPanePad)
			case f.diff != nil:
				kit.CodeView(c, f.diff).Fill()
			}
		})
	})
}

// showFile builds the diff of file i, with the threads of that file.
func (p *PRPane) showFile(i int) {
	f := &p.filesState
	f.shown, f.diff, f.note = i, nil, ""
	file := f.files[i]
	fps, err := editor.ParsePatch(file.Patch)
	if err != nil {
		f.note = "Cannot read the diff of " + file.Path + ": " + err.Error()
		return
	}
	if len(fps) == 0 || fps[0].Binary {
		f.note = LabelPRBinary
		return
	}
	d := kit.NewCode(file.Path, "", "")
	d.SetPatch(fps[0])
	byKey := map[string]github.Thread{}
	for _, t := range f.threads {
		if t.Path != file.Path {
			continue
		}
		side := editor.DiffNew
		if t.Side == "LEFT" {
			side = editor.DiffOld
		}
		line, ok := patchTextLine(fps[0], side, t.Line)
		if !ok {
			continue // the line is not in the diff
		}
		byKey[t.ID] = t
		d.Annotations = append(d.Annotations, editor.DiffAnnotation{Side: side, Line: line, Height: p.threadBoxHeight(t), Key: t.ID})
	}
	d.RenderAnnotation = func(c *ui.Context, a editor.DiffAnnotation) {
		id, _ := a.Key.(string)
		p.renderThread(c, byKey[id])
	}
	f.diff = d
}

// patchTextLine maps line n of the old or new file to the line of the text
// that SetPatch builds for that side, counted from 0. It is false when the
// diff does not show the line.
func patchTextLine(fp editor.FilePatch, side editor.DiffSide, n int) (int, bool) {
	oldIdx, newIdx := 0, 0
	for _, h := range fp.Hunks {
		oldNo, newNo := h.OldStart, h.NewStart
		for _, l := range h.Lines {
			if l == "" || l[0] == '\\' {
				continue
			}
			if l[0] != '+' {
				if side == editor.DiffOld && oldNo == n {
					return oldIdx, true
				}
				oldIdx++
				oldNo++
			}
			if l[0] != '-' {
				if side == editor.DiffNew && newNo == n {
					return newIdx, true
				}
				newIdx++
				newNo++
			}
		}
	}
	return 0, false
}

// threadCollapsed is the one place that decides how a resolved thread
// looks: one line, "resolved · who" (owner question D25; change it here).
// It returns false for a thread that shows in full.
func threadCollapsed(t github.Thread) (string, bool) {
	if !t.Resolved {
		return "", false
	}
	who := ""
	if len(t.Comments) > 0 {
		who = t.Comments[0].Who
	}
	return "resolved · " + who, true
}

const (
	threadMarginTop, threadMarginBottom = 6, 8
	threadPadY                          = 8
)

// threadHeight is the room under the line for thread t, margins included.
func threadHeight(t github.Thread) float32 {
	lines := 1
	if _, ok := threadCollapsed(t); !ok {
		lines = 0
		for _, cm := range t.Comments {
			lines++ // who
			for _, l := range strings.Split(strings.TrimRight(cm.Body, "\n"), "\n") {
				lines += max(1, (len([]rune(l))+prThreadCols-1)/prThreadCols)
			}
		}
	}
	return float32(threadMarginTop+threadMarginBottom+2*threadPadY) + float32(lines)*theme.LineHeight
}

// threadBoxHeight is threadHeight plus the actions row of an open thread and
// its reply field, when that is open.
func (p *PRPane) threadBoxHeight(t github.Thread) float32 {
	h := threadHeight(t)
	if t.Resolved {
		return h
	}
	h += theme.AnswerButtonHeight + 8 // actions row and its gap
	if p.filesState.replying[t.ID] {
		h += theme.AnswerButtonHeight + 8
	}
	return h
}

// syncThreadHeights resizes the thread boxes after a reply field opens or
// closes. It keeps the diff and its scroll place.
func (p *PRPane) syncThreadHeights() {
	f := &p.filesState
	if f.diff == nil {
		return
	}
	byID := map[string]github.Thread{}
	for _, t := range f.threads {
		byID[t.ID] = t
	}
	for i := range f.diff.Annotations {
		id, _ := f.diff.Annotations[i].Key.(string)
		f.diff.Annotations[i].Height = p.threadBoxHeight(byID[id])
	}
}

// draft is the reply text of thread id.
func (f *prFiles) draft(id string) *string {
	if f.drafts == nil {
		f.drafts = map[string]*string{}
	}
	if f.drafts[id] == nil {
		f.drafts[id] = new(string)
	}
	return f.drafts[id]
}

// writeThread runs one write off the UI thread. On success it reads the
// threads again. On failure the error shows in the pane and the typed text
// stays. Only one write runs at a time.
func (p *PRPane) writeThread(do func(context.Context, github.Client) error, ok func()) {
	f := &p.filesState
	if f.busy || p.client.R == nil {
		return
	}
	f.busy, f.writeErr = true, ""
	client, n := p.client, p.number
	p.reads.Do(p.Env, func() func() {
		err := do(context.Background(), client)
		var threads []github.Thread
		var terr error
		if err == nil {
			threads, terr = client.Threads(context.Background(), n)
		}
		return func() {
			f.busy = false
			switch {
			case err != nil:
				f.writeErr = err.Error()
			default:
				ok()
				if terr != nil {
					f.threadErr = terr.Error()
				} else {
					f.threads, f.threadErr = threads, ""
					f.shown = -1 // rebuild the diff with the new threads
				}
			}
			p.Env.Redraw()
		}
	})
	p.Env.Redraw()
}

// replyTo sends the typed reply of thread id.
func (p *PRPane) replyTo(id string) {
	f := &p.filesState
	text := strings.TrimSpace(*f.draft(id))
	if text == "" {
		return
	}
	p.writeThread(func(ctx context.Context, c github.Client) error { return c.Reply(ctx, id, text) },
		func() { delete(f.drafts, id); delete(f.replying, id) })
}

// resolveThread marks thread id as resolved.
func (p *PRPane) resolveThread(id string) {
	p.writeThread(func(ctx context.Context, c github.Client) error { return c.Resolve(ctx, id) }, func() {})
}

// renderThread draws the box of a thread: bg #1C1C1C, border #2A2A2A,
// radius 8, padding 8×10; per comment the author (bold) with `on line N`
// on the first, then the text. An open thread ends with `reply` and
// `resolve`; `reply` opens a field (Enter or `Reply` sends).
func (p *PRPane) renderThread(c *ui.Context, t github.Thread) {
	f := &p.filesState
	ui.Column(c).Label("thread "+t.ID).FillWidth().Margin(threadMarginTop, 10, threadMarginBottom, 12).
		Padding(threadPadY, 10).Radius(8).Background(theme.Color(theme.BgCard)).
		Border(1, theme.Color(theme.SegmentSelected)).Gap(8).Children(func() {
		if s, ok := threadCollapsed(t); ok {
			ui.Text(c, s).TextColor(theme.Color(theme.TextDim)).SingleLine().Ellipsis("…")
			return
		}
		for i, cm := range t.Comments {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, cm.Who).Bold().NoWrap()
				if i == 0 {
					ui.Text(c, "on line "+strconv.Itoa(t.Line)).TextColor(theme.Color(theme.TextDim)).NoWrap()
				}
			})
			ui.Text(c, cm.Body).TextColor(theme.Color(theme.Text2)).FillWidth()
		}
		ui.Row(c).Height(theme.AnswerButtonHeight).Gap(theme.AnswerButtonGap).AlignItems(ui.Center).Children(func() {
			var reply, resolve kit.ClickFn
			if !f.busy {
				reply = func() {
					if f.replying == nil {
						f.replying = map[string]bool{}
					}
					f.replying[t.ID] = !f.replying[t.ID]
					p.syncThreadHeights()
					p.Env.Redraw()
				}
				resolve = func() { p.resolveThread(t.ID) }
			}
			kit.CardButton(c, LabelPRReply, kit.ButtonPlain, theme.Current().Ink48, reply)
			kit.CardButton(c, LabelPRResolve, kit.ButtonPlain, theme.Current().Ink48, resolve)
		})
		if f.replying[t.ID] {
			ui.Row(c).Height(theme.AnswerButtonHeight).Gap(theme.AnswerButtonGap).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c.Key("reply"+t.ID), f.draft(t.ID)).Label(LabelPRReplyBox).
					Placeholder("Reply").Grow(1).MinWidth(0)
				if in.Submitted() {
					p.replyTo(t.ID)
				}
				send := kit.ClickFn(func() { p.replyTo(t.ID) })
				if f.busy {
					send = nil
				}
				kit.CardButton(c, LabelPRReplySend, kit.ButtonOutlined, theme.Current().Ink88, send)
			})
		}
	})
}
