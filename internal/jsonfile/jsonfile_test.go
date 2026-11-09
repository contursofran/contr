package jsonfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// names returns the names in dir.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestWriteMakesTheFolderAndTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	path := filepath.Join(dir, "f.json")
	if err := Write(path, []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"x":1}` {
		t.Fatalf("got %q, %v", data, err)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("temp file left: %v", got)
	}
}

func TestWriteReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	if err := Write(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatalf("got %q, %v", data, err)
	}
	if got := names(t, filepath.Dir(path)); len(got) != 1 {
		t.Fatalf("temp file left: %v", got)
	}
}

func TestWriteKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	for _, perm := range []os.FileMode{0o600, 0o644} {
		path := filepath.Join(dir, perm.String()+".json")
		if err := Write(path, []byte("{}"), perm); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		// The umask can only take bits away.
		if got := st.Mode().Perm(); got&^perm != 0 || got&0o600 != 0o600 {
			t.Errorf("%v: mode %v", perm, got)
		}
	}
}

func TestAFailedCreateLeavesTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := Write(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := Write(path, []byte("new"), 0o644); err == nil {
		t.Skip("folder is writable (running as root?)")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
		t.Fatalf("old file changed: %q, %v", data, err)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("temp file left: %v", got)
	}
}

func TestAFailedRenameRemovesTheTempFile(t *testing.T) {
	dir := t.TempDir()
	// A folder that is not empty at path: the rename fails.
	path := filepath.Join(dir, "f.json")
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o644); err == nil {
		t.Fatal("want an error")
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "f.json" {
		t.Fatalf("temp file left: %v", got)
	}
}

func TestWriteIntoAFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(dir, "x", "f.json"), []byte("{}"), 0o644); err == nil {
		t.Fatal("want an error")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	type v struct{ X int }

	got := v{X: 7}
	if ok, err := Load(filepath.Join(dir, "missing.json"), &got); ok || err != nil || got.X != 7 {
		t.Fatalf("missing: %v, %v, %+v", ok, err, got)
	}

	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"X":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := Load(good, &got); !ok || err != nil || got.X != 3 {
		t.Fatalf("good: %v, %v, %+v", ok, err, got)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{nope`), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := Load(bad, &got); ok || err == nil || !strings.HasPrefix(err.Error(), "parse "+bad+": ") {
		t.Fatalf("bad: %v, %v", ok, err)
	}

	// A folder at the path: the read fails.
	if ok, err := Load(dir, &got); ok || err == nil || !strings.HasPrefix(err.Error(), "read "+dir+": ") {
		t.Fatalf("folder: %v, %v", ok, err)
	}
}
