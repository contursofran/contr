package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/owner"
)

// runOwnerCmd runs `contr owner args…` against the owner file at path.
func runOwnerCmd(t *testing.T, core *cliCore, path string, args ...string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	dial := func(context.Context, func(string) string) (agentsCore, error) {
		if core == nil {
			return nil, errors.New("no hc")
		}
		return core, nil
	}
	env := map[string]string{"CONTR_OWNER_FILE": path}
	code := runOwner(args, &out, &errb, dial, func(k string) string { return env[k] })
	return code, errb.String()
}

func TestOwnerParsesAsASubcommand(t *testing.T) {
	if c := parseArgs([]string{"contr", "owner", "done", "T1"}); c.Kind != kindOwner || strings.Join(c.Then, " ") != "done T1" {
		t.Errorf("parse: %+v", c)
	}
}

func TestOwnerAnswerSavesAndTellsTheCoordinator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	store := &owner.Store{Path: path}
	if _, err := store.Ask(owner.Question{Project: "p", Coordinator: "t-7", Question: "Which?", Options: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	core := &cliCore{}
	if code, errs := runOwnerCmd(t, core, path, "answer", "D1", "use", "b"); code != 0 {
		t.Fatalf("answer: %d %q", code, errs)
	}
	st, _ := store.Read()
	if q := st.Questions[0]; q.Answer != "use b" || q.AnsweredBy != "owner" {
		t.Errorf("saved: %+v", q)
	}
	if len(core.calls) != 1 || core.calls[0] != "terminal.send" || core.params[0]["terminal"] != "t-7" || core.params[0]["text"] != "Owner answered D1: use b\n" {
		t.Errorf("sent: %v %v", core.calls, core.params)
	}
	if !core.closed {
		t.Error("connection left open")
	}
	if code, errs := runOwnerCmd(t, core, path, "answer", "D1", "again"); code != 1 || !strings.Contains(errs, "no open question D1") {
		t.Errorf("second answer: %d %q", code, errs)
	}
}

func TestOwnerDoneSavesAndTellsTheCoordinator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	store := &owner.Store{Path: path}
	if _, err := store.Pin(owner.Todo{Project: "p", Coordinator: "t-7", Kind: "do", Text: "Test the build"}); err != nil {
		t.Fatal(err)
	}
	core := &cliCore{}
	if code, errs := runOwnerCmd(t, core, path, "done", "T1"); code != 0 {
		t.Fatalf("done: %d %q", code, errs)
	}
	st, _ := store.Read()
	if st.Todos[0].Done.IsZero() {
		t.Error("not marked done")
	}
	if len(core.params) != 1 || core.params[0]["text"] != "Owner did T1: Test the build\n" {
		t.Errorf("sent: %v", core.params)
	}
}

func TestOwnerWithoutCoordinatorOnlySaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	store := &owner.Store{Path: path}
	if _, err := store.Pin(owner.Todo{Project: "p", Kind: "know", Text: "FYI"}); err != nil {
		t.Fatal(err)
	}
	if code, errs := runOwnerCmd(t, nil, path, "done", "T1"); code != 0 {
		t.Errorf("done: %d %q", code, errs)
	}
}

func TestOwnerSavedWhenTheCoordinatorIsNotReached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	store := &owner.Store{Path: path}
	if _, err := store.Ask(owner.Question{Project: "p", Coordinator: "t-7", Question: "Ok?"}); err != nil {
		t.Fatal(err)
	}
	code, errs := runOwnerCmd(t, nil, path, "answer", "D1", "yes")
	if code != 1 || !strings.Contains(errs, "saved; coordinator not reached") {
		t.Errorf("answer: %d %q", code, errs)
	}
	if st, _ := store.Read(); st.Questions[0].Answer != "yes" {
		t.Error("answer not saved")
	}
}

func TestOwnerUsageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	for _, args := range [][]string{{}, {"answer", "D1"}, {"answer", "D1", " "}, {"done"}, {"done", "T1", "x"}, {"ls"}} {
		if code, _ := runOwnerCmd(t, nil, path, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	if code, _ := runOwnerCmd(t, nil, path, "help"); code != 0 {
		t.Errorf("help: code %d", code)
	}
}
