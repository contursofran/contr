package usage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ctxLine = `{"version":"2.1.0","context_window":{"context_window_size":200000,"used_percentage":45,
"current_usage":{"input_tokens":2000,"cache_creation_input_tokens":8000,"cache_read_input_tokens":80000}}}`

func TestParseContextSumsTheInputTokens(t *testing.T) {
	c := ParseContext(ctxLine, 7)
	if c == nil || c.Tokens != 90000 || c.Size != 200000 || c.UpdatedAt != 7 {
		t.Fatalf("context %+v", c)
	}
	if c.Percent() != 45 || c.Label() != "90k of 200k  45%" {
		t.Errorf("percent %v label %q", c.Percent(), c.Label())
	}
}

func TestParseContextSkipsInputWithoutUsage(t *testing.T) {
	for name, in := range map[string]string{
		"not json":       `{`,
		"no window":      `{"version":"2"}`,
		"no usage yet":   `{"context_window":{"context_window_size":200000,"current_usage":null}}`,
		"no size":        `{"context_window":{"current_usage":{"input_tokens":5}}}`,
		"rate limits":    `{"rate_limits":{"five_hour":{"used_percentage":5}}}`,
		"wrong type":     `{"context_window":{"context_window_size":"big"}}`,
		"empty document": ``,
	} {
		if c := ParseContext(in, 1); c != nil {
			t.Errorf("%s: context %+v, want none", name, c)
		}
	}
}

func TestContextLabelAndPercentEdges(t *testing.T) {
	for _, tc := range []struct {
		c     Context
		label string
		pct   float64
	}{
		{Context{Tokens: 250000, Size: 200000}, "250k of 200k  100%", 100},
		{Context{Tokens: 0, Size: 1000000}, "0 of 1M  0%", 0},
		{Context{Tokens: 999, Size: 200000}, "999 of 200k  0%", 0},
		{Context{Tokens: 5, Size: 0}, "5 of 0  0%", 0},
	} {
		if got := tc.c.Label(); got != tc.label || tc.c.Percent() != tc.pct {
			t.Errorf("%+v: label %q percent %v, want %q %v", tc.c, got, tc.c.Percent(), tc.label, tc.pct)
		}
	}
}

func TestContextIsKeptPerTerminal(t *testing.T) {
	dir := t.TempDir()
	a, b := ParseContext(ctxLine, 1), &Context{Schema: schema, Tokens: 10000, Size: 200000}
	for id, c := range map[string]*Context{"w1-2": a, "w1-3": b} {
		if wrote, err := WriteContext(dir, id, c); err != nil || !wrote {
			t.Fatalf("write %s: %v %v", id, wrote, err)
		}
	}
	if got := ReadContext(dir, "w1-2"); got == nil || got.Tokens != 90000 {
		t.Errorf("w1-2 = %+v", got)
	}
	if got := ReadContext(dir, "w1-3"); got == nil || got.Tokens != 10000 {
		t.Errorf("w1-3 = %+v", got)
	}
	if ReadContext(dir, "w1-4") != nil || ReadContext(dir, "") != nil {
		t.Error("a terminal with no file has a context")
	}
	all := ReadContexts(dir)
	if len(all) != 2 || all[ContextKey("w1-3")].Tokens != 10000 {
		t.Errorf("contexts %+v", all)
	}
	// The same tokens are not written again.
	if wrote, _ := WriteContext(dir, "w1-2", a); wrote {
		t.Error("rewrote an unchanged context")
	}
}

func TestContextIdIsOneSafeFileName(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteContext(dir, "../../evil/x:1", ParseContext(ctxLine, 1)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ContextDir))
	if len(entries) != 1 || strings.ContainsAny(entries[0].Name(), "/:") {
		t.Errorf("entries %v", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Error("wrote outside the context folder")
	}
	if _, err := WriteContext(dir, "", ParseContext(ctxLine, 1)); err == nil {
		t.Error("wrote with an empty id")
	}
	if _, err := WriteContext(dir, "///", ParseContext(ctxLine, 1)); err == nil {
		t.Error("wrote with an id that has no name")
	}
}

func TestBadContextFilesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, ContextDir)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"corrupt.json": `{`, "schema.json": `{"schema":9,"tokens":1,"size":2}`,
		"nosize.json": `{"schema":1,"tokens":1}`, "note.txt": `x`,
	} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := ReadContexts(dir); len(got) != 0 {
		t.Errorf("contexts %+v, want none", got)
	}
	if got := ReadContexts(filepath.Join(dir, "missing")); len(got) != 0 {
		t.Errorf("missing folder gave %+v", got)
	}
	if got := ReadContexts(""); len(got) != 0 {
		t.Errorf("empty dir gave %+v", got)
	}
}

func TestStatuslineSavesTheContextOfItsTerminal(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runWith(strings.NewReader(ctxLine), &out, nil, dir, "w1-2", now, chainTimeout); err != nil {
		t.Fatal(err)
	}
	if got := ReadContext(dir, "w1-2"); got == nil || got.Tokens != 90000 {
		t.Errorf("context %+v", got)
	}
	// Outside hc (no terminal id) nothing is saved for a context.
	other := t.TempDir()
	if err := runWith(strings.NewReader(ctxLine), &out, nil, other, "", now, chainTimeout); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(other, ContextDir)) {
		t.Error("saved a context without a terminal id")
	}
	// A failed save does not fail the status line.
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "usage"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runWith(strings.NewReader(ctxLine), &out, nil, blocked, "w1-2", now, chainTimeout); err != nil {
		t.Errorf("a failed save returned %v", err)
	}
}

func TestSamplerReadsTheContexts(t *testing.T) {
	f := newFake(Unread())
	ctx := map[string]Context{ContextKey("w1-2"): {Schema: schema, Tokens: 90000, Size: 200000}}
	f.readContexts = func() map[string]Context { return ctx }
	f.SetVisible(true)
	if c, ok := f.Context("w1-2"); !ok || c.Tokens != 90000 {
		t.Fatalf("context %+v %v", c, ok)
	}
	if _, ok := f.Context("w1-9"); ok {
		t.Error("context for an unknown terminal")
	}
	if _, ok := f.Context(""); ok {
		t.Error("context for no terminal")
	}
	if f.notified != 1 {
		t.Errorf("notified %d times, want 1 (a new context redraws)", f.notified)
	}
	// An unchanged context does not redraw; a changed one does.
	f.clock.advance(ReadEvery)
	if f.notified != 1 {
		t.Errorf("notified %d times after an unchanged read", f.notified)
	}
	ctx = map[string]Context{ContextKey("w1-2"): {Schema: schema, Tokens: 100000, Size: 200000}}
	f.clock.advance(ReadEvery)
	if f.notified != 2 {
		t.Errorf("notified %d times after a change, want 2", f.notified)
	}
}

func TestParseContextKeepsTheShortModel(t *testing.T) {
	for _, tc := range []struct{ model, want string }{
		{`{"id":"claude-sonnet-5-5","display_name":"Sonnet 5.5"}`, "sonnet"},
		{`{"id":"claude-opus-5-5"}`, "opus"},
		{`{"id":"x-1","display_name":"Mystery 2"}`, "mystery 2"},
		{`{"id":"x-1"}`, "x-1"},
	} {
		in := `{"model":` + tc.model + `,"context_window":{"context_window_size":200000,"current_usage":{"input_tokens":1}}}`
		if c := ParseContext(in, 1); c == nil || c.Model != tc.want {
			t.Errorf("model %s: got %+v, want %q", tc.model, c, tc.want)
		}
	}
	if c := ParseContext(ctxLine, 1); c == nil || c.Model != "" {
		t.Errorf("input with no model: %+v", c)
	}
}

func TestAChangedModelIsWritten(t *testing.T) {
	dir := t.TempDir()
	c := &Context{Schema: schema, Size: 200000, Tokens: 10, Model: "sonnet"}
	if w, err := WriteContext(dir, "t1", c); err != nil || !w {
		t.Fatalf("first write %v %v", w, err)
	}
	if w, _ := WriteContext(dir, "t1", c); w {
		t.Error("same numbers and model were written again")
	}
	c2 := *c
	c2.Model = "opus"
	if w, _ := WriteContext(dir, "t1", &c2); !w {
		t.Error("a new model was not written")
	}
	if got := ReadContext(dir, "t1"); got == nil || got.Model != "opus" {
		t.Errorf("read %+v", got)
	}
}
