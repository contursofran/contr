//go:build e2e

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNeedsRowCapture shows the needs-you row with two questions and a to-do
// from a scratch owner.json, and saves the window as tmp/e2e/needs.png. hc is
// not reachable (CONTR_HC names no program), so the row works from the file
// alone. Start the dev herdr first:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags e2e -run TestNeedsRowCapture ./cmd/contr
func TestNeedsRowCapture(t *testing.T) {
	if !strings.Contains(os.Getenv("HERDR_SOCKET_PATH"), "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tmp", "e2e")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(out, "contr-e2e")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
	dir, err := os.MkdirTemp("", "contr-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ownerFile := filepath.Join(dir, "owner.json")
	const doc = `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"Which database?","options":["sqlite","redis"],"asked":"2026-11-03T10:00:00Z"},
{"id":"D2","project":"demo","title":"Ship the 0.2 release today?","question":"Ship?","asked":"2026-11-03T10:01:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the bag drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":3,"next_todo":2}`
	if err := os.WriteFile(ownerFile, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), "MYGO_AUTOMATION="+sock, "CONTR_OWNER_FILE="+ownerFile,
		"CONTR_HC="+filepath.Join(dir, "no-hc"),
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(15 * time.Second)
	for !c.labels(t)["needs row"] {
		if time.Now().After(deadline) {
			t.Fatalf("the needs row did not show; labels %v", keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	labels := c.labels(t)
	for _, want := range []string{"D1 Which database for the cache?", "D2 Ship the 0.2 release today?", "T1 Test the bag drag on the Mac"} {
		if !labels[want] {
			t.Errorf("no tab %q; labels %v", want, keys(labels))
		}
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	c.capture(t, filepath.Join(out, "needs.png"))
}
