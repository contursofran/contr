package updated

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", FileName)
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := Note{Version: "0.4.0", From: "0.3.2", Notes: "- Pill\n- Tabs", InstalledAt: at}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.From != want.From || got.Notes != want.Notes ||
		!got.InstalledAt.Equal(at) || got.Shown {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	want.Shown = true
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); !got.Shown {
		t.Error("shown did not save")
	}
	left, _ := os.ReadDir(filepath.Dir(path))
	if len(left) != 1 {
		t.Errorf("the folder holds %d files, want only %s", len(left), FileName)
	}
}

func TestTheFileHasTheIssueFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Save(path, Note{Version: "0.4.0"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, k := range []string{`"version"`, `"from"`, `"notes"`, `"installed_at"`, `"shown"`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("no %s in %s", k, data)
		}
	}
}

func TestAMissingFileIsNoNoteAndNoError(t *testing.T) {
	n, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil || n != (Note{}) {
		t.Fatalf("got %+v, %v", n, err)
	}
}

func TestACorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := Load(path)
	if err == nil {
		t.Fatal("no error for a corrupt file")
	}
	if n.Due("0.4.0") {
		t.Error("a corrupt file is due")
	}
}

func TestDue(t *testing.T) {
	n := Note{Version: "0.4.0", From: "0.3.2"}
	if !n.Due("0.4.0") {
		t.Error("the running version is not due")
	}
	if n.Due("0.3.2") || n.Due("0.5.0") {
		t.Error("another version is due")
	}
	n.Shown = true
	if n.Due("0.4.0") {
		t.Error("a shown note is due")
	}
	if (Note{}).Due("") {
		t.Error("an empty note is due")
	}
}

func TestPath(t *testing.T) {
	if Path("") != "" {
		t.Error("no state folder gives a path")
	}
	if got := Path("/s"); got != filepath.Join("/s", FileName) {
		t.Errorf("Path = %q", got)
	}
}
