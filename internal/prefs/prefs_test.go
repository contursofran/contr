package prefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	if err := Save(path, Prefs{Appearance: mygo.ThemeLight}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Source() != mygo.ThemeLight {
		t.Fatalf("Load = %+v, %v; want light", got, err)
	}
}

func TestLoadMissingFileGivesSystem(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || got.Source() != mygo.ThemeSystem {
		t.Fatalf("Load = %+v, %v; want system and no error", got, err)
	}
}

func TestUnknownAppearanceFallsBackToSystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"appearance":"sepia"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Appearance != mygo.ThemeSystem {
		t.Fatalf("Load = %+v, %v; want system and no error", got, err)
	}
}

func TestLoadBadFileGivesSystemAndError(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"junk":    "{not json",
		"version": `{"version":9,"appearance":"dark"}`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err == nil || got.Source() != mygo.ThemeSystem {
			t.Errorf("%s: Load = %+v, %v; want system and an error", name, got, err)
		}
		if data, _ := os.ReadFile(path); string(data) != body {
			t.Errorf("%s: Load changed the file", name)
		}
	}
}

func TestSaveWritesSystemForABadValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, Prefs{Appearance: "sepia"}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Appearance != mygo.ThemeSystem {
		t.Fatalf("Load = %+v, %v; want system", got, err)
	}
}
