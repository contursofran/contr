package app

// The Files tab of the PR pane (#153, D102): the changed files on a list,
// the diff of the chosen file in a code view, and the review threads under
// their lines. It is read only; reply and resolve come later.

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
}

// loadFiles reads the files and the threads off the UI thread. Only the newest
// read lands (D59).
func (p *PRPane) loadFiles() {
	f := &p.filesState
	f.started, f.shown = true, -1
	gen := p.gen
	mk := p.Client
	if mk == nil {
		mk = github.New
	}
	client := mk(p.dir)
	n := p.number
	p.Run.Do(func() func() {
		files, err := client.Files(context.Background(), n)
		var threads []github.Thread
		var terr error
		if err == nil {
			threads, terr = client.Threads(context.Background(), n)
		}
		return func() {
			if gen != p.gen {
				return
			}
			f.loaded = true
			if err != nil {
				f.err = err.Error()
			} else {
				f.files, f.threads = files, threads
			}
			if terr != nil {
				f.threadErr = terr.Error()
			}
			p.redraw()
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
				OnClick: func() { f.sel = i; p.redraw() }})
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
		d.Annotations = append(d.Annotations, editor.DiffAnnotation{Side: side, Line: line, Height: threadHeight(t), Key: t.ID})
	}
	d.RenderAnnotation = func(c *ui.Context, a editor.DiffAnnotation) {
		id, _ := a.Key.(string)
		renderThread(c, byKey[id])
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

// renderThread draws the box of a thread: bg #1C1C1C, border #2A2A2A,
// radius 8, padding 8×10; per comment the author (bold) with `on line N`
// on the first, then the text.
func renderThread(c *ui.Context, t github.Thread) {
	ui.Column(c).Label("thread "+t.ID).FillWidth().Margin(threadMarginTop, 10, threadMarginBottom, 12).
		Padding(threadPadY, 10).Radius(8).Background(theme.Color(theme.BgCard)).
		Border(1, theme.Color(theme.SegmentSelected)).Children(func() {
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
	})
}
