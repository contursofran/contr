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
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
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

func f64(v float64) *float64 { return &v }

func TestTintSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	in := Prefs{Appearance: mygo.ThemeDark, Tint: Tints{
		Dark:  Tint{Window: f64(0.2), Panel: f64(0.7)},
		Light: Tint{Panel: f64(0)},
	}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d, l := got.Tint.Dark, got.Tint.Light
	if d.Window == nil || *d.Window != 0.2 || d.Panel == nil || *d.Panel != 0.7 {
		t.Errorf("dark tint = %+v, want 0.2 and 0.7", d)
	}
	if l.Window != nil || l.Panel == nil || *l.Panel != 0 {
		t.Errorf("light tint = %+v, want no window and panel 0", l)
	}
}

func TestOldFileWithoutTintLoadsWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"appearance":"light"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Source() != mygo.ThemeLight || got.Tint != (Tints{}) {
		t.Fatalf("Load = %+v, %v; want light and no tint values", got, err)
	}
}

func TestTintValuesAreLimitedToZeroToOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"tint":{"dark":{"window":1.5,"panel":-2}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || *got.Tint.Dark.Window != 1 || *got.Tint.Dark.Panel != 0 {
		t.Fatalf("Load = %+v, %v; want 1 and 0", got.Tint.Dark, err)
	}
}

func TestChatTextSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, Prefs{ChatText: ChatLarger}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.ChatText != ChatLarger || got.ChatScale() != 1.3 {
		t.Fatalf("Load = %+v, %v; want larger at 1.3", got, err)
	}
}

func TestChatTextMissingOrUnknownIsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, body := range []string{`{"version":1}`, `{"version":1,"chatText":"huge"}`} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got.ChatText != ChatDefault || got.ChatScale() != 1 {
			t.Errorf("%s: Load = %+v, %v; want default at 1", body, got, err)
		}
	}
	if (Prefs{}).ChatScale() != 1 {
		t.Error("zero Prefs: scale is not 1")
	}
}

func TestSetupSeenSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if got, err := Load(path); err != nil || got.SetupSeen {
		t.Fatalf("missing file: SetupSeen = %v, %v; want false", got.SetupSeen, err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || got.SetupSeen {
		t.Fatalf("missing field: SetupSeen = %v, %v; want false", got.SetupSeen, err)
	}
	if err := Save(path, Prefs{SetupSeen: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || !got.SetupSeen {
		t.Fatalf("SetupSeen = %v, %v; want true", got.SetupSeen, err)
	}
}
