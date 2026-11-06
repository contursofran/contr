package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultFlowIsThePlaybook(t *testing.T) {
	want := Flow{Plan: "when-needed", Review: "code", DebuggerAfter: 2, Workers: 4, Merge: "ask"}
	if got := DefaultConfig().Flow; got != want {
		t.Fatalf("default flow %+v", got)
	}
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || cfg.Flow != want {
		t.Fatalf("missing file: %+v %v", cfg.Flow, err)
	}
}

func TestLoadConfigFlowKeepsDefaultsForMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"flow":{"review":"every","workers":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Flow{Plan: "when-needed", Review: "every", DebuggerAfter: 2, Workers: 2, Merge: "ask"}
	if cfg.Flow != want {
		t.Fatalf("flow %+v", cfg.Flow)
	}
}

func TestLoadConfigBadFlowIsAnError(t *testing.T) {
	for _, body := range []string{
		`{"flow":{"plan":"sometimes"}}`,
		`{"flow":{"review":"all"}}`,
		`{"flow":{"merge":"yes"}}`,
		`{"flow":{"debugger_after":0}}`,
		`{"flow":{"workers":-1}}`,
		`{"flow":{"workers":"four"}}`,
	} {
		path := filepath.Join(t.TempDir(), "agents.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("%s: no error", body)
		}
	}
	if err := (Config{Flow: Flow{Plan: "x"}}).Save(filepath.Join(t.TempDir(), "a.json")); err == nil {
		t.Error("Save of a bad flow: no error")
	}
}

func TestSaveRoundTripKeepsUnknownKeysAndProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	in := `{"later":{"x":1},"profiles":{"mine":{"argv":["my-agent","--odd"]}},"setup":{"~/r":"make"}}`
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Flow.Merge = "auto"
	cfg.Flow.Plan = "never"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["later"]) != `{"x":1}` && !strings.Contains(string(raw["later"]), `"x": 1`) {
		t.Errorf("unknown key lost: %s", data)
	}
	back, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Flow != cfg.Flow {
		t.Errorf("flow %+v, want %+v", back.Flow, cfg.Flow)
	}
	if got := back.Profiles["mine"].Argv; len(got) != 2 || got[1] != "--odd" {
		t.Errorf("custom argv %v", got)
	}
	if back.Setup["~/r"] != "make" || len(back.Profiles) != len(cfg.Profiles) {
		t.Errorf("setup %v profiles %d", back.Setup, len(back.Profiles))
	}
}

func TestFlowText(t *testing.T) {
	got := DefaultFlow().Text()
	for _, want := range []string{"The flow:\n", "- Plan first: when needed.", "- Review: code PRs.", "after 2 failed fix rounds", "at most 4", "- Merge: ask"} {
		if !strings.Contains(got, want) {
			t.Errorf("text lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n- "); n != 5 {
		t.Errorf("%d rule lines", n)
	}
}
