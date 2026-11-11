package slash

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestItemsReadsUserProjectPluginsAndBuiltins(t *testing.T) {
	tmp := t.TempDir()
	home, proj, plug := filepath.Join(tmp, "home"), filepath.Join(tmp, "proj"), filepath.Join(tmp, "plug")
	write(t, filepath.Join(home, ".claude/commands/ship.md"), "---\ndescription: \"Ship it\"\n---\nbody\n")
	write(t, filepath.Join(home, ".claude/commands/git/sync.md"), "# Title\n\nSync the branch\n")
	write(t, filepath.Join(home, ".claude/skills/pdf/SKILL.md"), "---\nname: pdf-tools\ndescription: Read PDFs\n---\n")
	write(t, filepath.Join(proj, ".claude/skills/deploy/SKILL.md"), "Deploy the app\n")
	write(t, filepath.Join(proj, ".claude/commands/ship.md"), "---\ndescription: Project ship\n---\n")
	write(t, filepath.Join(plug, "commands/orchestrate.md"), "---\ndescription: Lead workers\n---\n")
	write(t, filepath.Join(home, ".claude/plugins/installed_plugins.json"),
		`{"version":2,"plugins":{"contr@local":[{"installPath":"`+plug+`"}]}}`)
	r := &Reader{Home: home}
	items, err := r.Items(proj)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.Name] = it.Detail
	}
	want := map[string]string{"ship": "Project ship", "git:sync": "Sync the branch", "pdf-tools": "Read PDFs",
		"deploy": "Deploy the app", "contr:orchestrate": "Lead workers", "clear": "Clear the conversation history"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].Name > items[i].Name {
			t.Fatalf("not sorted at %d: %v", i, items)
		}
	}
	// The list is kept per folder: a new file shows only for a new folder.
	write(t, filepath.Join(proj, ".claude/commands/late.md"), "Late\n")
	again, _ := r.Items(proj)
	if len(again) != len(items) {
		t.Errorf("second read = %d items, want the kept %d", len(again), len(items))
	}
}

func TestItemsWithNoFoldersGivesBuiltins(t *testing.T) {
	r := &Reader{Home: t.TempDir()}
	items, err := r.Items("")
	if err != nil || len(items) != len(Builtins) {
		t.Fatalf("items = %d, %v; want %d builtins", len(items), err, len(Builtins))
	}
}

func TestBadPluginsFileIsSkipped(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude/plugins/installed_plugins.json"), "{")
	if items, _ := (&Reader{Home: home}).Items(""); len(items) != len(Builtins) {
		t.Fatalf("items = %d, want builtins only", len(items))
	}
}
