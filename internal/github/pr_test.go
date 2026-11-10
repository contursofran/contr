package github

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIssueTitleReadsTheTitleOfAnIssue(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"title":"  Agent rows on ui.List \n"}`)}
	got, err := Client{R: f, Dir: "/wt"}.IssueTitle(context.Background(), 443)
	if err != nil || got != "Agent rows on ui.List" {
		t.Fatalf("title %q, err %v", got, err)
	}
	if want := "issue view 443 --json title"; strings.Join(f.args, " ") != want || f.dir != "/wt" {
		t.Errorf("gh %v in %q, want %q", f.args, f.dir, want)
	}
}

func TestIssueTitleErrors(t *testing.T) {
	if _, err := (Client{R: &fakeRunner{err: errors.New("not found")}}).IssueTitle(context.Background(), 1); err == nil {
		t.Error("a gh error gave no error")
	}
	if _, err := (Client{R: &fakeRunner{out: []byte(`nope`)}}).IssueTitle(context.Background(), 1); err == nil {
		t.Error("bad JSON gave no error")
	}
}

// argsRunner answers each gh run by its first two arguments.
type argsRunner struct {
	answers map[string]func() ([]byte, error)
	runs    []string
}

func (r *argsRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.runs = append(r.runs, strings.Join(args, " "))
	if f, ok := r.answers[strings.Join(args[:2], " ")]; ok {
		return f()
	}
	return nil, errors.New("unexpected run")
}

func TestRefStateOfAPullRequest(t *testing.T) {
	r := &argsRunner{answers: map[string]func() ([]byte, error){
		"pr view": func() ([]byte, error) {
			return []byte(`{"number":667,"state":"OPEN","statusCheckRollup":[` +
				`{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"FAILURE"},` +
				`{"status":"IN_PROGRESS"},{"status":"COMPLETED","conclusion":"SKIPPED"}]}`), nil
		},
	}}
	got, err := Client{R: r}.RefState(context.Background(), 667)
	want := Ref{Number: 667, PR: true, State: "OPEN", Checks: CheckSummary{Pass: 1, Fail: 1, Pending: 1, Skip: 1}}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	if len(r.runs) != 1 || r.runs[0] != "pr view 667 --json number,state,statusCheckRollup" {
		t.Errorf("runs %q", r.runs)
	}
}

func TestRefStateOfAnIssue(t *testing.T) {
	r := &argsRunner{answers: map[string]func() ([]byte, error){
		"pr view": func() ([]byte, error) {
			return nil, errors.New("GraphQL: Could not resolve to a PullRequest with the number of 649.")
		},
		"issue view": func() ([]byte, error) { return []byte(`{"number":649,"state":"OPEN"}`), nil },
	}}
	got, err := Client{R: r}.RefState(context.Background(), 649)
	if err != nil || got != (Ref{Number: 649, State: "OPEN"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(r.runs) != 2 || r.runs[1] != "issue view 649 --json number,state" {
		t.Errorf("runs %q", r.runs)
	}
}

func TestRefStateOfNothingAndErrors(t *testing.T) {
	missing := func() ([]byte, error) { return nil, errors.New("GraphQL: Could not resolve to an issue") }
	r := &argsRunner{answers: map[string]func() ([]byte, error){"pr view": missing, "issue view": missing}}
	if got, err := (Client{R: r}).RefState(context.Background(), 3); err != nil || got != (Ref{}) {
		t.Errorf("unknown number: %+v, %v", got, err)
	}
	r = &argsRunner{answers: map[string]func() ([]byte, error){
		"pr view": func() ([]byte, error) { return nil, errors.New("network down") },
	}}
	if _, err := (Client{R: r}).RefState(context.Background(), 3); err == nil {
		t.Error("a gh error gave no error")
	}
	r = &argsRunner{answers: map[string]func() ([]byte, error){
		"pr view":    func() ([]byte, error) { return []byte(`nope`), nil },
		"issue view": func() ([]byte, error) { return []byte(`nope`), nil },
	}}
	if _, err := (Client{R: r}).RefState(context.Background(), 3); err == nil {
		t.Error("bad JSON gave no error")
	}
	r.answers["pr view"] = missing
	if _, err := (Client{R: r}).RefState(context.Background(), 3); err == nil {
		t.Error("bad issue JSON gave no error")
	}
	r.answers["issue view"] = func() ([]byte, error) { return nil, errors.New("network down") }
	if _, err := (Client{R: r}).RefState(context.Background(), 3); err == nil {
		t.Error("an issue gh error gave no error")
	}
}
