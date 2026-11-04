package widths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "widths.json")
	want := Widths{Sidebar: 300, Panel: 500}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, want)
	}
}

func TestLoadMissingFileGivesZero(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || got != (Widths{}) {
		t.Fatalf("Load = %+v, %v; want zero and no error", got, err)
	}
}

func TestLoadBadFileGivesZeroAndError(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"junk":     "{not json",
		"version":  `{"version":9,"sidebar":300,"panel":400}`,
		"negative": `{"version":1,"sidebar":-3,"panel":400}`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err == nil || got != (Widths{}) {
			t.Errorf("%s: Load = %+v, %v; want zero and an error", name, got, err)
		}
		if data, _ := os.ReadFile(path); string(data) != body {
			t.Errorf("%s: Load changed the file", name)
		}
	}
}
