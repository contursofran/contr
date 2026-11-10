package git

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type initRunner struct {
	calls [][]string
	fail  string // the first arg of the call that fails
}

func (r *initRunner) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{dir}, args...))
	if args[0] == r.fail {
		return nil, errors.New(r.fail + " failed")
	}
	return nil, nil
}

func TestInitRunsInitThenAnEmptyCommit(t *testing.T) {
	r := &initRunner{}
	if err := Init(context.Background(), r, "/p"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"/p", "init"}, {"/p", "commit", "--allow-empty", "-m", "Initial commit"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
}

func TestInitStopsAtTheFirstError(t *testing.T) {
	r := &initRunner{fail: "init"}
	if err := Init(context.Background(), r, "/p"); err == nil || !strings.Contains(err.Error(), "init failed") {
		t.Errorf("err %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("calls %q, want only init", r.calls)
	}
	r = &initRunner{fail: "commit"}
	if err := Init(context.Background(), r, "/p"); err == nil || !strings.Contains(err.Error(), "commit failed") {
		t.Errorf("commit err %v", err)
	}
}
