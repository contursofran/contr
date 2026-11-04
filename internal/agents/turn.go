package agents

import (
	"bufio"
	"encoding/json"
	"io"
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
