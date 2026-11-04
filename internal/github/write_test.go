package github

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommentArgs(t *testing.T) {
	f := &fakeRunner{}
	if err := (Client{R: f}).Comment(context.Background(), 7, "looks good"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "comment", "7", "--body", "looks good"}
	if !reflect.DeepEqual(f.args, want) {
		t.Errorf("args %v", f.args)
	}
}

func TestApproveArgs(t *testing.T) {
	f := &fakeRunner{}
	if err := (Client{R: f}).Approve(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "review", "7", "--approve"}
	if !reflect.DeepEqual(f.args, want) {
		t.Errorf("args %v", f.args)
	}
}

func TestReplyMutation(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"data":{}}`)}
	if err := (Client{R: f}).Reply(context.Background(), "T1", "thanks"); err != nil {
		t.Fatal(err)
	}
	a := f.args
	if len(a) != 8 || a[0] != "api" || a[1] != "graphql" || a[2] != "-f" ||
		!strings.HasPrefix(a[3], "query=") || !strings.Contains(a[3], "addPullRequestReviewThreadReply") ||
		a[4] != "-f" || a[5] != "id=T1" || a[6] != "-f" || a[7] != "body=thanks" {
		t.Errorf("args %q", a)
	}
}

func TestResolveMutation(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"data":{}}`)}
	if err := (Client{R: f}).Resolve(context.Background(), "T1"); err != nil {
		t.Fatal(err)
	}
	a := f.args
	if len(a) != 6 || !strings.Contains(a[3], "resolveReviewThread") || a[4] != "-f" || a[5] != "id=T1" {
		t.Errorf("args %q", a)
	}
}

func TestMutationGraphQLError(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"errors":[{"message":"Thread not found"}]}`)}
	err := (Client{R: f}).Resolve(context.Background(), "T1")
	if err == nil || err.Error() != "gh: Thread not found" {
		t.Errorf("err %v", err)
	}
}

func TestMutationBadJSON(t *testing.T) {
	f := &fakeRunner{out: []byte("nope")}
	err := (Client{R: f}).Reply(context.Background(), "T1", "x")
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("err %v", err)
	}
}

func TestMergePinsHeadSHA(t *testing.T) {
	f := &fakeRunner{}
	if err := (Client{R: f}).Merge(context.Background(), 7, "abc123"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "merge", "7", "--squash", "--match-head-commit", "abc123"}
	if !reflect.DeepEqual(f.args, want) {
		t.Errorf("args %v", f.args)
	}
}

func TestMergeNoSHAIsError(t *testing.T) {
	f := &fakeRunner{}
	for _, sha := range []string{"", "  "} {
		err := (Client{R: f}).Merge(context.Background(), 7, sha)
		if err == nil || err.Error() != "gh: merge needs the head commit" {
			t.Errorf("err %v", err)
		}
	}
	if f.args != nil {
		t.Errorf("gh was called: %v", f.args)
	}
}

func TestViewerLogin(t *testing.T) {
	f := &fakeRunner{out: []byte("octo\n")}
	got, err := (Client{R: f}).Viewer(context.Background())
	if err != nil || got != "octo" {
		t.Errorf("got %q, %v", got, err)
	}
	want := []string{"api", "user", "--jq", ".login"}
	if !reflect.DeepEqual(f.args, want) {
		t.Errorf("args %v", f.args)
	}
	f.out = []byte("\n")
	if _, err := (Client{R: f}).Viewer(context.Background()); err == nil {
		t.Error("empty login: want error")
	}
}

func TestWriteErrorsHavePrefix(t *testing.T) {
	f := &fakeRunner{err: errors.New("HTTP 403: forbidden")}
	c := Client{R: f}
	ctx := context.Background()
	errs := map[string]error{
		"comment": c.Comment(ctx, 1, "x"),
		"approve": c.Approve(ctx, 1),
		"reply":   c.Reply(ctx, "T", "x"),
		"resolve": c.Resolve(ctx, "T"),
		"merge":   c.Merge(ctx, 1, "abc"),
	}
	_, errs["viewer"] = c.Viewer(ctx)
	for name, err := range errs {
		if err == nil || err.Error() != "gh: HTTP 403: forbidden" {
			t.Errorf("%s: err %v", name, err)
		}
	}
}
