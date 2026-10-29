package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/herdr"
)

func pane(id, token string, has bool) herdr.Pane {
	p := herdr.Pane{ID: id, Tokens: map[string]string{}}
	if has {
		p.Tokens[BridgeToken] = token
	}
	return p
}

func workspace(id string, focused bool, panes ...herdr.Pane) herdr.Workspace {
	return herdr.Workspace{ID: id, Label: id, Focused: focused, Tabs: []herdr.Tab{{ID: id + ":t1", Label: "t1", Panes: panes}}}
}

func tree(ws ...herdr.Workspace) *herdr.HerdrTree {
	return &herdr.HerdrTree{Version: "0.9.3", Workspaces: ws}
}

const testSession = "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f"

func TestATreeWithoutTheTokenHasNoBridge(t *testing.T) {
	if _, ok := FindBridge(tree(workspace("w1", true, pane("w1:p1", "", false)))); ok {
		t.Fatal("found a bridge")
	}
	if _, ok := FindBridge(nil); ok {
		t.Fatal("found a bridge in a nil tree")
	}
}

func TestABadTokenIsIgnored(t *testing.T) {
	bad := tree(workspace("w1", true,
		pane("w1:p1", "2 s1 1", true),
		pane("w1:p2", "1 s1", true),
		pane("w1:p3", "1 s1 1", true)))
	found, ok := FindBridge(bad)
	if !ok || found.PaneID != "w1:p3" || found.Bridge.Session != "s1" {
		t.Fatalf("found = %+v, %v", found, ok)
	}
}

func TestThePaneInTheFocusedWorkspaceWins(t *testing.T) {
	two := tree(
		workspace("w1", false, pane("w1:p1", "1 s1 1", true)),
		workspace("w2", true, pane("w2:p1", "1 s2 5", true)))
	found, ok := FindBridge(two)
	if !ok || found.PaneID != "w2:p1" || found.Bridge != (BridgeRef{Session: "s2", Rev: 5}) {
		t.Fatalf("found = %+v, %v", found, ok)
	}
}

func TestWithNoFocusedWorkspaceTheFirstInTreeOrderWins(t *testing.T) {
	two := tree(
		workspace("w1", false, pane("w1:p1", "1 s1 1", true)),
		workspace("w2", false, pane("w2:p1", "1 s2 5", true)))
	if found, _ := FindBridge(two); found.PaneID != "w1:p1" {
		t.Fatalf("pane = %q", found.PaneID)
	}
}

func TestDirPrefersTheOverrideThenHome(t *testing.T) {
	cases := []struct {
		override, home, want string
		ok                   bool
	}{
		{"/x", "/h", "/x", true},
		{"", "/h", filepath.Join("/h", ".local/state/herdr-board"), true},
		{"", "home", filepath.Join("home", ".local/state/herdr-board"), true},
		{"", "", "", false},
	}
	for _, c := range cases {
		got, ok := DirFrom(c.override, c.home)
		if got != c.want || ok != c.ok {
			t.Errorf("DirFrom(%q, %q) = %q, %v; want %q, %v", c.override, c.home, got, ok, c.want, c.ok)
		}
	}
}

func TestDirReadsTheEnvironment(t *testing.T) {
	t.Setenv(DirEnv, "/over")
	t.Setenv("HOME", "/h")
	if got, ok := Dir(); !ok || got != "/over" {
		t.Fatalf("Dir = %q, %v", got, ok)
	}
}

func dirWith(t *testing.T, text []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir, testSession), text, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadCappedRefusesAFileOverTheCap(t *testing.T) {
	path := Path(dirWith(t, []byte("0123456789")), testSession)
	if _, line := readCapped(path, 9); line != BoardTooLarge {
		t.Fatalf("line = %q", line)
	}
	data, line := readCapped(path, 10)
	if line != "" || string(data) != "0123456789" {
		t.Fatalf("data = %q, line = %q", data, line)
	}
}

func TestReadCappedReportsAMissingFileCalmly(t *testing.T) {
	if _, line := readCapped(Path(t.TempDir(), testSession), 10); line != NoBoardFile {
		t.Fatalf("line = %q", line)
	}
}

func TestReadCappedReportsAnUnreadableFileWithoutTheOSText(t *testing.T) {
	// A directory opens, but reading it fails: not "not found".
	dir := t.TempDir()
	path := Path(dir, testSession)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, line := readCapped(path, 10); line != BoardUnreadable {
		t.Fatalf("line = %q", line)
	}
}

func TestReadParsesAndMapsErrorsToLines(t *testing.T) {
	snap, line := Read(Path(dirWith(t, fixtureJSON), testSession))
	if line != "" || snap == nil || len(snap.Rows) != 3 {
		t.Fatalf("snap = %v, line = %q", snap, line)
	}
	_, line = Read(Path(dirWith(t, []byte(`{"version": 2, "session": "s", "rev": 1}`)), testSession))
	if line != "board v2 · this contr reads v1" {
		t.Fatalf("line = %q", line)
	}
}
