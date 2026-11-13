package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	for _, want := range []string{"The flow:\n", "- Plan first: when needed.", "- Review: code PRs.", "after 2 failed fix rounds", "at most 4", "- Merge: ask", "- Profiles: builder: builder, planner: planner, reviewer: reviewer, debugger: debugger, explorer: explorer."} {
		if !strings.Contains(got, want) {
			t.Errorf("text lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n- "); n != 6 {
		t.Errorf("%d rule lines", n)
	}
}

func TestSaveWritesOnlyChangedOrPresentProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"planner":{"argv":["claude","--model","opus","--effort","low","--permission-mode","auto"]}},"other":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Profiles["debugger"] = Profile{Argv: []string{"claude", "--model", "opus", "--effort", "max"}}
	cfg.Profiles["orchestrator"] = Profile{Argv: []string{"claude"}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Profiles map[string]Profile `json:"profiles"`
		Other    int                `json:"other"`
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range got.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "debugger,orchestrator,planner" || got.Other != 1 {
		t.Fatalf("file = %s", data)
	}
}

func TestSaveOfDefaultsWritesNoProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := DefaultConfig().Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "profiles") {
		t.Fatalf("file = %s", data)
	}
}

func TestDefaultConfigHasTheOrchestrator(t *testing.T) {
	p, ok := DefaultConfig().Profiles["orchestrator"]
	want := []string{"claude", "--model", "opus", "--effort", "medium", "--permission-mode", "auto"}
	if !ok || !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("orchestrator %+v", p)
	}
}

func TestSaveDoesNotWriteTheDefaultOrchestrator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "orchestrator") {
		t.Fatalf("default frozen in file: %s", data)
	}
}

func TestLoadConfigOldFormatGivesANotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	body := `{"version":1,"presets":[{"name":"x","argv":["codex"]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notice != OldFormatNotice {
		t.Errorf("notice %q", cfg.Notice)
	}
	if _, ok := cfg.Profiles["orchestrator"]; !ok {
		t.Error("defaults do not apply")
	}
	if data, _ := os.ReadFile(path); string(data) != body {
		t.Errorf("the file changed: %q", data)
	}
	// A file with profiles, or with neither key, gives no notice.
	for _, b := range []string{`{"presets":[],"profiles":{}}`, `{"version":1}`} {
		if err := os.WriteFile(path, []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
		if c, err := LoadConfig(path); err != nil || c.Notice != "" {
			t.Errorf("%s: %v, notice %q", b, err, c.Notice)
		}
	}
}

func TestLoadConfigOwnOrchestratorWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"orchestrator":{"argv":["codex"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || !reflect.DeepEqual(cfg.Profiles["orchestrator"].Argv, []string{"codex"}) {
		t.Fatalf("%v %+v", err, cfg.Profiles["orchestrator"])
	}
}

func TestFlowProfilesLoadAndCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"flow":{"plan":"never"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || cfg.Flow.Profiles != (RoleProfiles{}) {
		t.Fatalf("old file: %v %+v", err, cfg.Flow.Profiles)
	}
	if err := cfg.CheckProfile("builder"); err != nil {
		t.Errorf("builder: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{"b2":{"argv":["claude"]}},"flow":{"profiles":{"builder":"b2"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := cfg.CheckProfile("b2"); err != nil {
		t.Errorf("b2: %v", err)
	}
	err = cfg.CheckProfile("builder")
	if err == nil || !strings.Contains(err.Error(), "allowed: b2, planner, reviewer, debugger, explorer") {
		t.Errorf("builder after the change: %v", err)
	}
	if !strings.Contains(cfg.Flow.Text(), "- Profiles: builder: b2, planner: planner,") {
		t.Errorf("text:\n%s", cfg.Flow.Text())
	}
	if err := cfg.CheckProfile("unknown"); err != nil {
		t.Errorf("a profile c lacks is left to Spawn: %v", err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"builder": "b2"`) {
		t.Errorf("saved: %s", data)
	}
}
