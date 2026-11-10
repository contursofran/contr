package app

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/shortcut"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelTerminalArt names the drawing of the empty terminal for tests and the e2e driver. Screen
// readers do not see it: it is decoration.
const LabelTerminalArt = "terminal art"

// TerminalNewLine is the hint under TerminalOffLine; the key of New tab follows it.
const TerminalNewLine = "New terminal"

// handsGapTop is where the fingertip gap sits from the panel's top, as a fraction of its
// height: a little above the middle (#644).
const handsGapTop = 0.45

// handsFade is the part of the panel, from the bottom, that fades to the stage color under the
// texts.
const handsFade = 30

var (
	handsOnce sync.Once
	handsBmp  atomic.Pointer[ui.Bitmap]
)

// handsBitmap is the decoded drawing, or nil until it is decoded. The first call starts the
// decode on a goroutine, once for the app; done runs when the bitmap is ready (on that
// goroutine). A drawing that does not decode is logged and never shows.
func handsBitmap(done func()) *ui.Bitmap {
	handsOnce.Do(func() { go loadHands(assets.Hands, done) })
	return handsBmp.Load()
}

// handsCover is where the drawing of size iw×ih goes to cover r: the gap between the fingertips
// at r's horizontal center and, if the drawing is tall enough, at handsGapTop of its height.
// The scale is large enough that the gap can be at the center with no empty band at a side.
func handsCover(r ui.Rect, iw, ih float32) ui.Rect {
	if iw <= 0 || ih <= 0 || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}
	}
	near := float32(min(assets.HandsGapX, 1-assets.HandsGapX))
	s := max(r.H/ih, r.W/(2*near*iw))
	w, h := iw*s, ih*s
	x := r.X + r.W/2 - float32(assets.HandsGapX)*w
	y := r.Y + r.H*handsGapTop - float32(assets.HandsGapY)*h
	y = min(max(y, r.Y+r.H-h), r.Y)
	return ui.Rect{X: x, Y: y, W: w, H: h}
}

// emptyTerminal is the terminal slot while no terminal is open (docs/spec/Terminal.md, Empty):
// the dot drawing of the two hands covers the panel in ink-50, and over a fade to the stage at
// the bottom 30% it says "No terminal open" and "New terminal ⌘T". It is still: it draws
// nothing while idle (D55).
func (f *Frame) emptyTerminal(c *ui.Context) {
	p := theme.Current()
	invalidate := f.invalidate
	post := f.post
	bmp := handsBitmap(func() {
		if post != nil && invalidate != nil {
			post(invalidate)
		}
	})
	ui.Box(c).Label(LabelTerminalArt).AccessHidden(true).IgnoreInput(true).Absolute().Fill().
		Draw(func(pt *ui.Painter, r ui.Rect) {
			if bmp == nil {
				return
			}
			w, h := ui.ImageSize(bmp)
			// Inside the panel's 1px border, so its radius less 1.
			pt.Clip(r, theme.Radius12-1, func() {
				pt.ImageRegion(bmp, ui.Rect{}, handsCover(r, w, h), ui.ImageOptions{Tint: p.Ink50})
			})
		})
	ui.Column(c).IgnoreInput(true).Absolute().Left(0).Right(0).Bottom(0).
		HeightPercent(handsFade).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space6).
		LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: p.Stage.Alpha(0), At: 0}, {Color: p.Stage, At: 0.65}}}).
		Radius(0, 0, theme.Radius12-1, theme.Radius12-1).
		Children(func() {
			kit.Styled(ui.Text(c, TerminalOffLine), theme.StyleLabel).NoWrap().TextColor(p.Ink62)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
				kit.Styled(ui.Text(c, TerminalNewLine), theme.StyleSmall).NoWrap().TextColor(p.Ink50)
				kit.Styled(ui.Text(c, shortcut.Of(shortcut.NewTab).Glyphs()), theme.StyleKbd).NoWrap().TextColor(p.Ink50)
			})
		})
}

// loadHands decodes data as the drawing and keeps it, then runs done. Data that does not decode
// is logged and done does not run. Any thread.
func loadHands(data []byte, done func()) {
	b, err := ui.DecodeBitmap(data)
	if err != nil {
		slog.Error("terminal art not decoded", "err", err)
		return
	}
	handsBmp.Store(b)
	if done != nil {
		done()
	}
}
