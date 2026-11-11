package transcript

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// picLine is a user line with text and the image blocks imgs, each a JSON object.
func picLine(uuid string, imgs ...string) string {
	s := `{"type":"user","uuid":"` + uuid + `","message":{"content":[{"type":"text","text":"see"}`
	for _, im := range imgs {
		s += "," + im
	}
	return s + "]}}\n"
}

func b64(media string, data []byte) string {
	return `{"type":"image","source":{"type":"base64","media_type":"` + media + `","data":"` + base64.StdEncoding.EncodeToString(data) + `"}}`
}

// TestPicsAndLoadPic reads the images of prompts: one with bytes gives a Pic that LoadPic reads
// again from its line; one without bytes, of an unknown type, or empty only counts (D269).
func TestPicsAndLoadPic(t *testing.T) {
	png, gif := []byte("\x89PNG one"), []byte("GIF89a two")
	first := picLine("u1", b64("image/png", png))
	second := picLine("u2",
		`{"type":"image","source":{"type":"url","url":"https://x"}}`,
		b64("image/gif", gif),
		b64("image/tiff", []byte("tiff")),
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":""}}`)
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(first+second), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := NewReader(0).ReadFile(path)
	if err != nil || len(b.Entries) != 2 {
		t.Fatalf("read: %v %+v", err, b.Entries)
	}
	e1, e2 := b.Entries[0], b.Entries[1]
	if e1.Images != 1 || len(e1.Pics) != 1 || e1.Pics[0] != (Pic{Line: 0, N: 0}) {
		t.Errorf("first = %+v", e1)
	}
	if e2.Images != 4 || len(e2.Pics) != 1 || e2.Pics[0] != (Pic{Line: int64(len(first)), N: 1}) {
		t.Errorf("second = %+v", e2)
	}
	for _, c := range []struct {
		p    Pic
		want []byte
	}{{e1.Pics[0], png}, {e2.Pics[0], gif}} {
		got, err := LoadPic(path, c.p)
		if err != nil || !bytes.Equal(got, c.want) {
			t.Errorf("LoadPic(%+v) = %q, %v; want %q", c.p, got, err, c.want)
		}
	}
	// A place with no bytes, a place past the line's images and a missing file fail.
	for _, p := range []Pic{{Line: int64(len(first)), N: 0}, {Line: 0, N: 3}} {
		if _, err := LoadPic(path, p); !errors.Is(err, ErrNoPic) {
			t.Errorf("LoadPic(%+v) err = %v, want ErrNoPic", p, err)
		}
	}
	if _, err := LoadPic(path, Pic{Line: 3}); err == nil {
		t.Error("LoadPic in the middle of a line: no error")
	}
	if _, err := LoadPic(filepath.Join(t.TempDir(), "gone"), Pic{}); err == nil {
		t.Error("LoadPic of a missing file: no error")
	}
}

// TestPicsOfQueuedAndHidden: a queued command's images are Pics of its line; a second shape of
// the same message is hidden with no Pics.
func TestPicsOfQueuedAndHidden(t *testing.T) {
	q := `{"type":"attachment","attachment":{"type":"queued_command","prompt":[{"type":"text","text":"also this"},` + b64("image/png", []byte("x")) + `]}}`
	again := `{"type":"attachment","attachment":{"type":"queued_command","prompt":[{"type":"text","text":"also this"},` + b64("image/png", []byte("x")) + `]}}`
	b := read(t, q, again)
	if len(b.Entries) != 2 {
		t.Fatalf("entries %+v", b.Entries)
	}
	if e := b.Entries[0]; e.Kind != Prompt || e.Images != 1 || len(e.Pics) != 1 || e.Pics[0].N != 0 {
		t.Errorf("queued = %+v", e)
	}
	if e := b.Entries[1]; e.Kind != Hidden || e.Pics != nil {
		t.Errorf("second shape = %+v", e)
	}
	// The offset of the second line is the length of the first with its newline.
	if p := read(t, lPrompt, q).Entries[1].Pics; len(p) != 1 || p[0].Line != int64(len(lPrompt)+1) {
		t.Errorf("pics after a prompt = %+v, want line %s", p, strconv.Itoa(len(lPrompt)+1))
	}
}

// TestImageMarksDrop: a prompt with images drops its "[Image #N]" marks; one without images keeps
// them as text.
func TestImageMarksDrop(t *testing.T) {
	for in, want := range map[string]string{
		"[Image #1] look":             "look",
		"look [Image #2]\n[Image #3]": "look",
		"[Image #x] stays":            "[Image #x] stays",
		"[Image #1]":                  "",
		"a [b] [Image #12]c":          "a [b] c",
	} {
		if got := DropImageMarks(in); got != want {
			t.Errorf("DropImageMarks(%q) = %q, want %q", in, got, want)
		}
	}
	if e := one(t, `{"type":"user","message":{"content":[{"type":"text","text":"[Image #1] look"},`+b64("image/png", []byte("x"))+`]}}`); e.Text != "look" || e.Images != 1 {
		t.Errorf("with an image = %+v", e)
	}
	if e := one(t, `{"type":"user","message":{"content":[{"type":"text","text":"[Image #1] look"}]}}`); e.Text != "[Image #1] look" {
		t.Errorf("without images = %+v", e)
	}
	if e := one(t, `{"type":"user","message":{"content":[`+b64("image/png", []byte("x"))+`]}}`); e.Kind != Prompt || e.Text != "" || len(e.Pics) != 1 {
		t.Errorf("image alone = %+v", e)
	}
}
