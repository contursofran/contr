package agents

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Turn is what a Claude Code transcript says about the last turn: the part
// after the last prompt.
type Turn struct {
	// Reported is set when the turn sent a message to another terminal:
	// a Bash call with `hc send`, or the kit's message tool.
	Reported bool
	// Said is the last text of the assistant in the turn.
	Said string
}

// entry is the part of one transcript line that LastTurn reads.
type entry struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		Command string `json:"command"`
	} `json:"input"`
}

// LastTurn reads a Claude Code transcript (JSON lines) and returns its last
// turn. A line that is not JSON is skipped: the format belongs to Claude
// Code, so LastTurn reads only the fields it needs.
func LastTurn(r io.Reader) (Turn, error) {
	var t Turn
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		blocks := contentBlocks(e.Message.Content)
		switch e.Type {
		case "user":
			if !e.IsMeta && isPrompt(e.Message.Content, blocks) {
				t = Turn{}
			}
		case "assistant":
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if s := strings.TrimSpace(b.Text); s != "" {
						t.Said = s
					}
				case "tool_use":
					if reports(b) {
						t.Reported = true
					}
				}
			}
		}
	}
	return t, sc.Err()
}

// contentBlocks decodes content that is a list of blocks; a plain string
// gives none.
func contentBlocks(raw json.RawMessage) []block {
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// isPrompt tells if user content is a prompt (typed or sent text), not a
// tool result.
func isPrompt(raw json.RawMessage, blocks []block) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return true
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
	}
	return len(blocks) > 0
}

// reports tells if a tool call sends a message to another terminal.
func reports(b block) bool {
	if b.Name == "Bash" {
		return strings.Contains(b.Input.Command, "hc send")
	}
	return strings.HasSuffix(b.Name, "contr__message")
}

// tailBytes is how much of the end of a transcript ContextTokens reads.
const tailBytes = 1 << 20

// ContextTokens returns the size of the context at the last assistant reply
// of the transcript at path: its input tokens, cache reads and cache writes.
// It reads only the end of the file. It returns 0 when no reply has usage.
func ContextTokens(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if off := fi.Size() - tailBytes; off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return 0, err
		}
	}
	tokens := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Usage *struct {
					Input       int `json:"input_tokens"`
					CacheRead   int `json:"cache_read_input_tokens"`
					CacheCreate int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		// The first line after a seek is cut; it does not parse and is skipped.
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "assistant" || e.Message.Usage == nil {
			continue
		}
		u := e.Message.Usage
		tokens = u.Input + u.CacheRead + u.CacheCreate
	}
	return tokens, sc.Err()
}

// RanCommand tells whether the transcript at path holds a run of the slash
// command name (such as "contr:orchestrate"): Claude Code records it as
// <command-name>/name</command-name>.
func RanCommand(path, name string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	mark := []byte("<command-name>/" + name + "</command-name>")
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if bytes.Contains(sc.Bytes(), mark) {
			return true, nil
		}
	}
	return false, sc.Err()
}
