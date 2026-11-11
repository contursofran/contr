package transcript

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNoPic tells that the line of a Pic holds no image there: the file
// changed.
var ErrNoPic = errors.New("transcript: no image at that place")

// LoadPic reads the bytes of image p from the transcript at path: it reads
// the one line at p.Line again and decodes the image's base64 data (D269).
// It blocks: call it off the UI thread.
func LoadPic(path string, p Pic) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(p.Line, io.SeekStart); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var l struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Attachment struct {
			Prompt json.RawMessage `json:"prompt"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(line, &l); err != nil {
		return nil, fmt.Errorf("transcript: image line: %w", err)
	}
	content := l.Message.Content
	if len(content) == 0 {
		content = l.Attachment.Prompt
	}
	var bs []struct {
		Type   string `json:"type"`
		Source struct {
			Type string `json:"type"`
			Data string `json:"data"`
		} `json:"source"`
	}
	if json.Unmarshal(content, &bs) != nil {
		return nil, ErrNoPic
	}
	n := 0
	for _, b := range bs {
		if b.Type != "image" {
			continue
		}
		if n == p.N {
			if b.Source.Type != "base64" || b.Source.Data == "" {
				return nil, ErrNoPic
			}
			return base64.StdEncoding.DecodeString(b.Source.Data)
		}
		n++
	}
	return nil, ErrNoPic
}
