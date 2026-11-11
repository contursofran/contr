package chatview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/transcript"
)

// tinyPNG is a generated 8×6 PNG.
func tinyPNG(t testing.TB) []byte { return pngOf(t, 8, 6) }

// pngOf is a generated w×h PNG: a grey gradient with a lighter band, so a thumbnail shows its crop.
func pngOf(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			v := uint8(60 + 120*x/w)
			if y > h/3 && y < h/2 {
				v = 230
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// picPrompt is a prompt line with text and image blocks: data nil is a block with no bytes.
func picPrompt(uuid, text string, datas ...[]byte) string {
	s := `{"type":"user","uuid":"` + uuid + `","message":{"content":[{"type":"text","text":` + strconv.Quote(text) + `}`
	for _, d := range datas {
		if d == nil {
			s += `,{"type":"image","source":{"type":"url","url":"https://example.invalid/x.png"}}`
			continue
		}
		s += `,{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + base64.StdEncoding.EncodeToString(d) + `"}}`
	}
	return s + `]}}`
}

// TestPromptImagesShowAndOpen: a prompt's image shows as a thumbnail under it, without its
// "[Image #1]" mark; a click opens it larger, and Escape closes it (D269).
func TestPromptImagesShowAndOpen(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(picPrompt("u1", "[Image #1] look at this", tinyPNG(t)))
	r.tt.Frame()
	if !r.tt.HasText("look at this") || r.tt.HasText("[Image #1] look at this") {
		t.Errorf("prompt text: %v", r.tt.Texts())
	}
	if !r.has(LabelThumb + " 1") {
		t.Fatalf("no thumbnail in %v", r.tt.Texts())
	}
	if r.has(LabelImages + " 1 image") {
		t.Error("a chip shows for an image that loads")
	}
	if err := r.tt.Click(LabelThumb + " 1"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.has(LabelBigImage) {
		t.Fatal("a click does not open the image larger")
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	r.tt.Frame()
	if r.ch.big != nil || !apptest.Absent(r.tt, LabelBigImage) {
		t.Error("Escape does not close the larger image")
	}
}

// TestPromptImagesWithoutBytesOrBroken: images with no bytes, and an image whose bytes do not
// decode, show one quiet chip with their count, not an error.
func TestPromptImagesWithoutBytesOrBroken(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(picPrompt("u1", "two", nil, nil), picPrompt("u2", "broken", []byte("not a png")))
	r.tt.Frame()
	if !r.has(LabelImages + " 2 images") {
		t.Errorf("no chip for two images without bytes: %v", r.tt.Texts())
	}
	if !r.has(LabelImages + " 1 image") {
		t.Errorf("no chip for a broken image: %v", r.tt.Texts())
	}
	if r.has(LabelThumb + " 1") {
		t.Error("a thumbnail shows for a broken image")
	}
}

func TestImagesText(t *testing.T) {
	if ImagesText(1) != "1 image" || ImagesText(3) != "3 images" {
		t.Errorf("%q %q", ImagesText(1), ImagesText(3))
	}
}

// TestPicCacheKeepsTheNewest: the cache holds at most picCacheMax images; the least recently used
// goes, and one being read stays.
func TestPicCacheKeepsTheNewest(t *testing.T) {
	ch := &Chat{pics: map[picKey]*picState{}}
	reading := picKey{"p", transcript.Pic{N: 999}}
	ch.pics[reading] = &picState{loading: true}
	for i := range picCacheMax + 5 {
		ch.picUse++
		ch.pics[picKey{"p", transcript.Pic{N: i}}] = &picState{used: ch.picUse}
		ch.trimPics()
	}
	if len(ch.pics) != picCacheMax {
		t.Fatalf("cache holds %d, want %d", len(ch.pics), picCacheMax)
	}
	if ch.pics[reading] == nil {
		t.Error("an image being read was dropped")
	}
	if ch.pics[picKey{"p", transcript.Pic{N: 0}}] != nil || ch.pics[picKey{"p", transcript.Pic{N: picCacheMax + 4}}] == nil {
		t.Error("the cache dropped the wrong images")
	}
}

// pngClipboard puts an image, and no text, on the clipboard.
func pngClipboard(tt *ui.Tester) {
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, []byte("\x89PNG")))))
}

// keysSession is a session whose terminal records the keys the composer presses.
func keysSession(t *testing.T) (*sess, *[][]byte, *error) {
	s := newSession(t, "idle")
	var keys [][]byte
	var keyErr error
	s.ch.Keys = func(addr string, k []byte) error {
		if addr != term {
			t.Errorf("keys to %q, want %q", addr, term)
		}
		keys = append(keys, k)
		return keyErr
	}
	return s, &keys, &keyErr
}

// TestPasteImagePressesCtrlV: ⌘V with an image on the clipboard presses Ctrl+V in the terminal and
// shows a chip until the send; the send types the text, and the pending prompt shows the image
// chip (D269).
func TestPasteImagePressesCtrlV(t *testing.T) {
	s, keys, _ := keysSession(t)
	pngClipboard(s.tt)
	s.key(ui.Cmd, ui.KeyV)
	s.key(ui.Cmd, ui.KeyV)
	if len(*keys) != 2 || !bytes.Equal((*keys)[0], []byte{0x16}) {
		t.Fatalf("keys = %q, want Ctrl+V twice", *keys)
	}
	if !s.has(LabelAttached+" Image 1") || !s.has(LabelAttached+" Image 2") || s.draft() != "" {
		t.Fatalf("chips: %v, draft %q", s.tt.Texts(), s.draft())
	}
	s.typeIn("see these")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 || s.sent[0] != "see these" {
		t.Errorf("sent %q", s.sent)
	}
	if s.has(LabelAttached + " Image 1") {
		t.Error("the chip stays after the send")
	}
	if !s.has(LabelPending) || !s.has(LabelImages+" 2 images") {
		t.Errorf("pending prompt: %v", s.tt.Texts())
	}
}

// TestPasteTextIsUnchanged: a paste of text goes into the field and presses no key.
func TestPasteTextIsUnchanged(t *testing.T) {
	s, keys, _ := keysSession(t)
	s.tt.SetClipboard("plain words")
	s.key(ui.Cmd, ui.KeyV)
	if s.draft() != "plain words" || len(*keys) != 0 {
		t.Errorf("draft %q, keys %q", s.draft(), *keys)
	}
	// Text and an image on the clipboard: the text goes into the field.
	s.tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.Text, []byte(" more")), transfer.Bytes(transfer.PNG, []byte("\x89PNG")))))
	s.key(ui.Cmd, ui.KeyV)
	if s.draft() != "plain words more" || len(*keys) != 0 {
		t.Errorf("text and image: draft %q, keys %q", s.draft(), *keys)
	}
	// No Keys: an image paste takes nothing.
	s.ch.Keys = nil
	pngClipboard(s.tt)
	s.key(ui.Cmd, ui.KeyV)
	if s.has(LabelAttached + " Image 1") {
		t.Error("a chip without Keys")
	}
}

// TestSendImagesAlonePressesEnter: images with no text press Enter in the terminal; a failure keeps
// the chips and shows the error.
func TestSendImagesAlonePressesEnter(t *testing.T) {
	s, keys, keyErr := keysSession(t)
	s.key(0, ui.KeyEnter)
	if len(*keys) != 0 || s.has(LabelPending) {
		t.Fatalf("a blank field with no images sent: keys %q", *keys)
	}
	pngClipboard(s.tt)
	s.key(ui.Cmd, ui.KeyV)
	*keyErr = errors.New("terminal gone")
	s.key(0, ui.KeyEnter)
	if len(*keys) != 2 || !bytes.Equal((*keys)[1], []byte{'\r'}) || len(s.sent) != 0 {
		t.Fatalf("keys %q, sent %q", *keys, s.sent)
	}
	if !s.has(LabelAttached+" Image 1") || !s.tt.HasText("terminal gone") || s.has(LabelPending) {
		t.Errorf("after a failed send: %v", s.tt.Texts())
	}
	*keyErr = nil
	s.key(0, ui.KeyEnter)
	if len(*keys) != 3 || s.has(LabelAttached+" Image 1") || !s.has(LabelImages+" 1 image") {
		t.Errorf("after a send: keys %q, %v", *keys, s.tt.Texts())
	}
}

// TestPasteImageFailureShowsTheError: a Ctrl+V that fails adds no chip and shows the error.
func TestPasteImageFailureShowsTheError(t *testing.T) {
	s, _, keyErr := keysSession(t)
	*keyErr = errors.New("no terminal")
	pngClipboard(s.tt)
	s.key(ui.Cmd, ui.KeyV)
	if s.has(LabelAttached+" Image 1") || !s.tt.HasText("no terminal") {
		t.Errorf("after a failed paste: %v", s.tt.Texts())
	}
}

// TestDropFilesTypesTheirPaths: files dropped on the composer add their escaped paths to the field.
func TestDropFilesTypesTheirPaths(t *testing.T) {
	s, keys, _ := keysSession(t)
	s.typeIn("see")
	box, ok := s.tt.Find(LabelComposer)
	if !ok {
		t.Fatal("no composer")
	}
	if !s.tt.DropFiles(box.X+box.W/2, box.Y+box.H/2, "/tmp/shot one.png", "/tmp/b(1).png") {
		t.Fatal("the composer takes no files")
	}
	s.tt.Frame()
	if want := `see /tmp/shot\ one.png /tmp/b\(1\).png`; s.draft() != want {
		t.Errorf("draft %q, want %q", s.draft(), want)
	}
	if len(*keys) != 0 {
		t.Errorf("a drop pressed keys %q", *keys)
	}
}

func TestShellEscape(t *testing.T) {
	for in, want := range map[string]string{
		"/a/b.png": "/a/b.png", "/a b/c'd.png": `/a\ b/c\'d.png`, "/x/é.png": "/x/é.png", "/x/$y&.png": `/x/\$y\&.png`,
	} {
		if got := ShellEscape(in); got != want {
			t.Errorf("ShellEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResetDropsTheImages: a reset of the transcript drops the cached images and closes the
// larger one: the offsets may hold other lines now.
func TestResetDropsTheImages(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(picPrompt("u1", "look", tinyPNG(t)))
	r.tt.Frame()
	if err := r.tt.Click(LabelThumb + " 1"); err != nil {
		t.Fatal(err)
	}
	if len(r.ch.pics) == 0 || r.ch.big == nil {
		t.Fatal("no image cached or open")
	}
	r.ch.changed(true)
	if len(r.ch.pics) != 0 || r.ch.big != nil {
		t.Errorf("after a reset: %d images, big %v", len(r.ch.pics), r.ch.big)
	}
}

// TestSendButtonSendsImages: with image chips and an empty field the round button is Send, also
// while the agent works, and a click presses Enter in the terminal (D269, D271).
func TestSendButtonSendsImages(t *testing.T) {
	s, keys, _ := keysSession(t)
	s.status = "working"
	s.tt.Frame()
	pngClipboard(s.tt)
	s.key(ui.Cmd, ui.KeyV)
	if !s.has(LabelSend) || s.has(LabelStop) {
		t.Fatalf("button with images: %v", s.tt.Texts())
	}
	if err := s.tt.Click(LabelSend); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(*keys) != 2 || !bytes.Equal((*keys)[1], []byte{'\r'}) || s.has(LabelAttached+" Image 1") {
		t.Errorf("after Send: keys %q, %v", *keys, s.tt.Texts())
	}
}
