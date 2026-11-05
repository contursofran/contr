package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloneParent(t *testing.T) {
	if got := cloneParent(""); got != "" {
		t.Errorf("no home: %q", got)
	}
	home := t.TempDir()
	if got := cloneParent(home); got != home {
		t.Errorf("no folder: %q", got)
	}
	docs := filepath.Join(home, "Documents", "code")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cloneParent(home); got != docs {
		t.Errorf("documents/code: %q", got)
	}
	dev := filepath.Join(home, "Developer")
	if err := os.Mkdir(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cloneParent(home); got != dev {
		t.Errorf("Developer first: %q", got)
	}
}
