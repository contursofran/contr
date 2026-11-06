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
