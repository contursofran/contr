package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/agents"
)

// openAgents opens the modal on the Agents tab with path as agents.json.
func (r *settingsRig) openAgents(path string) {
	r.t.Helper()
	r.s.AgentsConfig = path
	r.open()
	r.click(SettingsTabs[TabAgents])
}

func (r *settingsRig) click(label string) {
	r.t.Helper()
	r.reveal(label)
	if err := r.tt.Click(label); err != nil {
		r.t.Fatalf("click %q: %v; texts %q", label, err, r.texts())
	}
	r.tt.Frame()
}

// reveal scrolls the Agents area until label is in the window, as the owner does.
func (r *settingsRig) reveal(label string) {
	r.t.Helper()
	for range 20 {
		el, ok := r.tt.Find(label)
		area, inArea := r.tt.Find(LabelAgentsScroll)
		if !ok || !inArea || el.Y+el.H <= area.Y+area.H && el.Y >= area.Y {
			return
		}
		dy := float32(120)
		if el.Y < area.Y {
			dy = -120
		}
		r.tt.Scroll(area.X+area.W/2, area.Y+area.H/2, 0, dy)
		r.tt.Frame()
	}
}

// pick opens a role dropdown and picks one item.
func (r *settingsRig) pick(dropdown, item string) {
	r.t.Helper()
	r.click(dropdown)
	r.click(dropdown + " " + item)
}

func writeAgents(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func argvOf(t *testing.T, path, profile string) []string {
	t.Helper()
	cfg, err := agents.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Profiles[profile]
	if !ok {
		return nil
	}
	return p.Argv
}

func TestSettingsAgentsTabShowsRolesAndCommandLines(t *testing.T) {
	r := newSettingsRig(t, nil)
	r.openAgents(writeAgents(t, ""))
	for _, want := range []string{TextRolesHint, "Orchestrator", "plans and lands work", "Planner", "writes PLAN.md",
		"Builder", "builds, opens the PR", "Reviewer", "reviews the PR head", "Debugger", "after failed fixes",
		"claude --model sonnet --effort medium --permission-mode auto", TextAgentsFoot, AgentsReset} {
		if !r.hasText(want) {
			t.Errorf("no %q in %q", want, r.texts())
		}
	}
}

func TestSettingsModelDropdownWritesArgvAndKeepsOtherFlags(t *testing.T) {
	path := writeAgents(t, `{"profiles":{"builder":{"argv":["claude","--model","sonnet","--effort","medium","--permission-mode","auto","--verbose"]}},"x":true}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.pick("Builder model", "Opus 5.5")
	want := []string{"claude", "--model", "opus", "--effort", "medium", "--permission-mode", "auto", "--verbose"}
	if got := argvOf(t, path, "builder"); !slices.Equal(got, want) {
		t.Fatalf("argv %q", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"x": true`) {
		t.Fatalf("unknown key lost: %s", data)
	}
}

func TestSettingsEffortDropdownWritesArgv(t *testing.T) {
	path := writeAgents(t, "")
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.pick("Debugger effort", "max")
	want := []string{"claude", "--model", "opus", "--effort", "max", "--permission-mode", "auto"}
	if got := argvOf(t, path, "debugger"); !slices.Equal(got, want) {
		t.Fatalf("argv %q", got)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "builder") {
		t.Fatalf("default profile written: %s", data)
	}
}

func TestSettingsCustomArgvStaysUntilAPick(t *testing.T) {
	path := writeAgents(t, `{"profiles":{"reviewer":{"argv":["codex","-m","gpt"]}}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	if !r.hasText("codex -m gpt") || !r.hasText(TextCustom) {
		t.Fatalf("texts %q", r.texts())
	}
	r.pick("Planner effort", "high")
	if got := argvOf(t, path, "reviewer"); !slices.Equal(got, []string{"codex", "-m", "gpt"}) {
		t.Fatalf("custom argv changed: %q", got)
	}
	r.pick("Reviewer CLI", TextClaudeCode)
	want := []string{"claude", "--model", "opus", "--effort", "low", "--permission-mode", "auto"}
	if got := argvOf(t, path, "reviewer"); !slices.Equal(got, want) {
		t.Fatalf("argv %q", got)
	}
}

func TestSettingsOrchestratorShowsTheDefaultAndAPickWritesIt(t *testing.T) {
	path := writeAgents(t, "")
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	if !r.hasText("claude --model opus --effort medium --permission-mode auto") || r.hasText("not set") {
		t.Fatalf("texts %q", r.texts())
	}
	if fileHasOrchestrator(path) {
		t.Fatal("opening wrote the default")
	}
	r.pick("Orchestrator effort", "high")
	want := []string{"claude", "--model", "opus", "--effort", "high", "--permission-mode", "auto"}
	if got := argvOf(t, path, "orchestrator"); !slices.Equal(got, want) {
		t.Fatalf("argv %q", got)
	}
}

func TestSettingsResetPutsTheDefaultsBack(t *testing.T) {
	path := writeAgents(t, `{"profiles":{"builder":{"argv":["codex"]},"orchestrator":{"argv":["claude","--model","haiku"]}}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.click(AgentsReset)
	if got := argvOf(t, path, "builder"); !slices.Equal(got, agents.DefaultConfig().Profiles["builder"].Argv) {
		t.Fatalf("builder %q", got)
	}
	if got := argvOf(t, path, "orchestrator"); !slices.Equal(got, roleDefault("orchestrator").Argv) {
		t.Fatalf("orchestrator %q", got)
	}
}

func TestSettingsResetDoesNotFreezeTheDefaultOrchestrator(t *testing.T) {
	path := writeAgents(t, "")
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.click(AgentsReset)
	if fileHasOrchestrator(path) {
		t.Fatal("reset wrote the default orchestrator")
	}
}

// fileHasOrchestrator reports whether the file itself holds an orchestrator profile.
func fileHasOrchestrator(path string) bool {
	data, _ := os.ReadFile(path)
	return strings.Contains(string(data), "orchestrator")
}

func TestSettingsAFailedSaveShowsTheError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	if err := os.Mkdir(path, 0o755); err != nil { // a folder in the place of the file
		t.Fatal(err)
	}
	r.pick("Builder model", "Haiku 5.5")
	if !r.hasText("save failed") {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestSettingsAFailedReadShowsTheError(t *testing.T) {
	r := newSettingsRig(t, nil)
	r.openAgents(writeAgents(t, "{"))
	if !r.hasText("read failed") {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestSettingsUpdatesTabShowsTheDotWhileAnUpdateWaits(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{On: true, Info: &UpdateInfo{Version: "9.9.9"}})
	r.open()
	if r.has("update dot") {
		t.Fatal("dot without an update")
	}
	r.s.found = &UpdateInfo{Version: "9.9.9"}
	r.tt.Frame()
	if !r.has("update dot") {
		t.Fatal("no dot")
	}
}

func TestSetFlagAddsAMissingFlag(t *testing.T) {
	got := setFlag([]string{"claude", "--model", "opus", "--x"}, "--effort", "low")
	if !slices.Equal(got, []string{"claude", "--model", "opus", "--effort", "low", "--x"}) {
		t.Fatalf("%q", got)
	}
	got = setFlag([]string{"claude", "--model=opus"}, "--model", "haiku")
	if !slices.Equal(got, []string{"claude", "--model=haiku"}) {
		t.Fatalf("%q", got)
	}
}

func flowOf(t *testing.T, path string) agents.Flow {
	t.Helper()
	cfg, err := agents.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Flow
}

func TestSettingsFlowLoadsFromAgentsJSON(t *testing.T) {
	path := writeAgents(t, `{"flow":{"plan":"always","review":"every","debugger_after":3,"workers":2,"merge":"auto"}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	for _, s := range []string{"debugger after 3", "every PR", "when CI and review pass"} {
		if !r.hasText(s) {
			t.Errorf("no %q; texts %q", s, r.texts())
		}
	}
	if r.hasText(FlowDocsNote) {
		t.Error("Every PR shows the docs-only note")
	}
}

func TestSettingsFlowChangeIsSavedAndKeepsOtherKeys(t *testing.T) {
	path := writeAgents(t, `{"profiles":{"builder":{"argv":["codex"]}},"extra":{"a":1}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.click("Plan, build, review")
	r.click("When CI and review pass")
	f := flowOf(t, path)
	if f.Plan != "always" || f.Review != "code" || f.Merge != "auto" || f.Workers != 4 {
		t.Fatalf("flow %+v", f)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"extra"`) || !slices.Equal(argvOf(t, path, "builder"), []string{"codex"}) {
		t.Fatalf("file %s", data)
	}
}

func TestSettingsFlowBadValueShowsTheReadError(t *testing.T) {
	path := writeAgents(t, `{"flow":{"plan":"sometimes"}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	if !r.hasText("read failed") || r.hasText(LabelFlowPreset) {
		t.Fatalf("texts %q", r.texts())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "sometimes") {
		t.Fatalf("the file changed: %s", data)
	}
}

func TestSettingsResetPutsTheDefaultFlowBack(t *testing.T) {
	path := writeAgents(t, `{"flow":{"plan":"never","review":"none","debugger_after":5,"workers":1,"merge":"auto"}}`)
	r := newSettingsRig(t, nil)
	r.openAgents(path)
	r.click(AgentsReset)
	if f := flowOf(t, path); f != agents.DefaultFlow() {
		t.Fatalf("flow %+v", f)
	}
}
