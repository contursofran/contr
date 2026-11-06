// The context of one agent, from Claude Code's status line. The status line
// runs in the agent's own terminal, so `contr statusline` can key the context
// by HC_TERMINAL_ID (D144). contr never reads Claude Code's transcripts.

package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ContextDir is the folder of the context files, under the state dir.
const ContextDir = "usage/context"

// EnvTerminal names the variable that holds the hc terminal of the agent.
const EnvTerminal = "HC_TERMINAL_ID"

// Context is the context window of one agent.
type Context struct {
	Schema uint32 `json:"schema"`
	// UpdatedAt is the Unix second when the file was written.
	UpdatedAt uint64 `json:"updated_at"`
	// Tokens is the size of the context now. Size is the window, in tokens.
	Tokens uint64 `json:"tokens"`
	Size   uint64 `json:"size"`
	// Model is the short model name from the status line ("sonnet", "opus"),
	// for line 2 of an agent row that contr did not start (D174). Empty when
	// the input had none.
	Model string `json:"model,omitempty"`
	// Effort is the effort level from the status line ("high"), empty when
	// the input had none (D189).
	Effort string `json:"effort,omitempty"`
}

// Percent is the whole percent of the window used, from 0 to 100.
func (c Context) Percent() float64 {
	if c.Size == 0 {
		return 0
	}
	return float64(min(c.Tokens*100/c.Size, 100))
}

// Label is the value text of the Context line: "90k of 200k  45%".
func (c Context) Label() string {
	return fmt.Sprintf("%s of %s  %d%%", shortTokens(c.Tokens), shortTokens(c.Size), int(c.Percent()))
}

// shortTokens writes n in thousands ("90k"), or in millions ("1M") when n is a
// whole number of millions.
func shortTokens(n uint64) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// contextLine holds the part of the status line input that gives the context.
type contextLine struct {
	ContextWindow *struct {
		Size         uint64 `json:"context_window_size"`
		CurrentUsage *struct {
			Input         uint64 `json:"input_tokens"`
			CacheCreation uint64 `json:"cache_creation_input_tokens"`
			CacheRead     uint64 `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	Model *struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Effort *struct {
		Level string `json:"level"`
	} `json:"effort"`
}

// ShortModel is the family word of a model: "sonnet" for "claude-sonnet-5-5"
// or "Sonnet 5.5". An unknown model gives its display name in lower case,
// else its id.
func ShortModel(id, display string) string {
	for _, fam := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(strings.ToLower(id+" "+display), fam) {
			return fam
		}
	}
	if display != "" {
		return strings.ToLower(display)
	}
	return id
}

// ParseContext reads the context from Claude Code's status line JSON. It
// returns nil when the input has no window size or no usage yet (a new
// session has current_usage null).
func ParseContext(stdinJSON string, now uint64) *Context {
	var in contextLine
	if err := json.Unmarshal([]byte(stdinJSON), &in); err != nil {
		return nil
	}
	w := in.ContextWindow
	if w == nil || w.Size == 0 || w.CurrentUsage == nil {
		return nil
	}
	u := w.CurrentUsage
	c := &Context{Schema: schema, UpdatedAt: now, Size: w.Size, Tokens: u.Input + u.CacheCreation + u.CacheRead}
	if in.Model != nil {
		c.Model = ShortModel(in.Model.ID, in.Model.DisplayName)
	}
	if in.Effort != nil {
		c.Effort = in.Effort.Level
	}
	return c
}

// contextPath is the file of terminal id. The id is cleaned to one safe file
// name; "" when nothing is left.
func contextPath(dir, id string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, id)
	if strings.Trim(name, "_") == "" {
		return ""
	}
	return filepath.Join(dir, ContextDir, name+".json")
}

// WriteContext saves c for terminal id, unless the file already holds the
// same tokens and size. It reports whether it wrote. Each terminal has its
// own file, so two agents never write the same one.
func WriteContext(dir, id string, c *Context) (bool, error) {
	path := contextPath(dir, id)
	if path == "" {
		return false, errors.New("no terminal id")
	}
	if old := readContext(path); old != nil && old.Tokens == c.Tokens && old.Size == c.Size && old.Model == c.Model && old.Effort == c.Effort {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// ReadContext returns the context saved for terminal id, or nil when there is
// none or the file is bad. It blocks: run it on a goroutine.
func ReadContext(dir, id string) *Context {
	path := contextPath(dir, id)
	if path == "" {
		return nil
	}
	return readContext(path)
}

func readContext(path string) *Context {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("unable to read a context file", "err", err)
		}
		return nil
	}
	var c Context
	if err := json.Unmarshal(data, &c); err != nil {
		slog.Warn("context file is corrupt", "err", err)
		return nil
	}
	if c.Schema != schema || c.Size == 0 {
		return nil
	}
	return &c
}

// ReadContexts returns the context of every terminal that has a file, keyed
// by the cleaned terminal id. It blocks: run it on a goroutine.
func ReadContexts(dir string) map[string]Context {
	out := map[string]Context{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Join(dir, ContextDir))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("unable to read the context folder", "err", err)
		}
		return out
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		if c := readContext(filepath.Join(dir, ContextDir, e.Name())); c != nil {
			out[name] = *c
		}
	}
	return out
}

// ContextKey is the key of terminal id in [ReadContexts].
func ContextKey(id string) string {
	p := contextPath("", id)
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(p), ".json")
}
