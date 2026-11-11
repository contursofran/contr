package chatview

// The images of a prompt (docs/spec/ChatView.md, D269): thumbnails under the bubble that open
// larger, and a quiet chip for the images whose bytes are not in the transcript or do not decode.
// The bytes stay in the file: a thumbnail reads its line again off the UI thread, and a small
// cache keeps the bitmaps.

import (
	"log"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/transcript"
)

// Labels of the images.
const (
	// LabelThumb names a thumbnail: "image 1", "image 2".
	LabelThumb = "image"
	// LabelImages names the chip of the images that do not show.
	LabelImages = "images"
	// LabelBigImage names the image opened larger.
	LabelBigImage = "image, larger"
)

// picCacheMax is the most bitmaps the chat keeps.
const picCacheMax = 32

// picKey is an image of a transcript.
type picKey struct {
	path string
	pic  transcript.Pic
}

// picState is a cached image: its bitmap, or failed. used orders the cache: the oldest goes.
type picState struct {
	bmp     *ui.Bitmap
	failed  bool
	loading bool
	used    uint64
}

// ImagesText is the chip of n images: "1 image", "2 images".
func ImagesText(n int) string {
	if n == 1 {
		return "1 image"
	}
	return strconv.Itoa(n) + " images"
}

// pic gives the state of image k, and starts its load when it is not cached.
func (ch *Chat) pic(k picKey) *picState {
	ch.picUse++
	if s := ch.pics[k]; s != nil {
		s.used = ch.picUse
		return s
	}
	s := &picState{loading: true, used: ch.picUse}
	ch.pics[k] = s
	ch.trimPics()
	ch.Env.Do(func() func() {
		bmp, err := loadPic(k)
		return func() {
			if err != nil {
				log.Printf("chatview: image %d of line %d: %v", k.pic.N, k.pic.Line, err)
			}
			s.bmp, s.failed, s.loading = bmp, err != nil, false
			ch.Env.Redraw()
		}
	})
	return s
}

// loadPic reads and decodes image k. It blocks.
func loadPic(k picKey) (*ui.Bitmap, error) {
	data, err := transcript.LoadPic(k.path, k.pic)
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

// trimPics drops the least recently used images that are not loading while the cache holds more
// than picCacheMax.
func (ch *Chat) trimPics() {
	for len(ch.pics) > picCacheMax {
		var old picKey
		var oldest uint64
		found := false
		for k, s := range ch.pics {
			if !s.loading && (!found || s.used < oldest) {
				old, oldest, found = k, s.used, true
			}
		}
		if !found {
			return
		}
		delete(ch.pics, old)
	}
}

// images shows the images of a prompt under its bubble, at the right: a thumbnail for each one
// that loads, and a chip with the count of the others. A thumbnail being read shows as a box of
// its size.
func (ch *Chat) images(c *ui.Context, pics []transcript.Pic, count int) {
	if count == 0 {
		return
	}
	path := ch.src.Path()
	pal := theme.Current()
	quiet := count - len(pics)
	ui.Row(c).FillWidth().Justify(ui.End).Wrap().Gap(theme.Space6).Margin(theme.Space6, 0, 0, 0).Children(func() {
		for i, p := range pics {
			k := picKey{path, p}
			s := ch.pic(k)
			label := LabelThumb + " " + strconv.Itoa(i+1)
			switch {
			case s.failed || path == "":
				quiet++
			case s.bmp == nil:
				ui.Box(c.Key(label)).Label(label).Size(theme.SizeThumb, theme.SizeThumb).Radius(theme.Radius12).Background(pal.Hover)
			default:
				ch.thumb(c.Key(label), k, s.bmp, label)
			}
		}
		if quiet > 0 {
			imageChip(c, ImagesText(quiet), LabelImages)
		}
	})
}

// thumb is one thumbnail: a click opens the image larger.
func (ch *Chat) thumb(c *ui.Context, k picKey, bmp *ui.Bitmap, label string) {
	b := ui.Box(c).Label(label).Size(theme.SizeThumb, theme.SizeThumb).Radius(theme.Radius12).
		Border(1, theme.Current().Hairline).Clip().Cursor(ui.CursorPointer).Children(func() {
		ui.Image(c, bmp).Fill().Fit(ui.Cover).Radius(theme.Radius12)
	})
	if b.Clicked() {
		ch.big = &k
	}
}

// imageChip is a quiet chip: "1 image" in a prompt, "Image 1" in the composer.
func imageChip(c *ui.Context, text, label string) {
	pal := theme.Current()
	ui.Box(c).Label(label+" "+text).Shrink(0).Padding(theme.Space2, theme.Space8).Radius(theme.Radius6).
		Border(1, pal.Hairline).Children(func() {
		kit.Styled(ui.Text(c, text), theme.StyleMeta).NoWrap().TextColor(pal.Ink62)
	})
}

// bigImage is the image the owner clicked, larger, over the dimmed window: as large as it is, at
// most the chat less its insets. Escape or a click outside closes it.
func (ch *Chat) bigImage(c *ui.Context) {
	if ch.big == nil {
		return
	}
	s := ch.pics[*ch.big]
	if s == nil || s.bmp == nil {
		ch.big = nil
		return
	}
	open := true
	pal := theme.Current()
	w, h := ui.ImageSize(s.bmp)
	if r := ch.panel.Bounds(c); r.W > 0 && r.H > 0 && w > 0 && h > 0 {
		mw, mh := r.W-4*theme.Space20, r.H-4*theme.Space20
		if f := min(1, mw/w, mh/h); f > 0 {
			w, h = w*f, h*f
		}
	}
	d := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.Scrim).Center()
		panel.Label(LabelBigImage).Radius(theme.Radius12).Clip()
		kit.PopDialog(c, backdrop, panel)
		panel.Children(func() {
			ui.Image(c, s.bmp).Size(w, h).Fit(ui.Contain).Radius(theme.Radius12)
		})
	})
	if d.Dismissed() || !open {
		ch.big = nil
		ch.Env.Redraw()
	}
}
