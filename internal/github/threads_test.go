package github

import (
	"context"
	"strings"
	"testing"
)

const repoJSON = `{"owner":{"login":"acme"},"name":"widgets"}`

const threadsJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
{"id":"T1","isResolved":false,"path":"a.go","line":12,"originalLine":10,"diffSide":"RIGHT",
 "comments":{"nodes":[
  {"id":"C1","author":{"login":"bob"},"body":"first","createdAt":"2026-11-03T10:00:00Z"},
  {"id":"C2","author":{"login":"alice"},"body":"second","createdAt":"2026-11-03T11:00:00Z"}]}},
{"id":"T2","isResolved":true,"path":"b.go","line":null,"originalLine":5,"diffSide":"LEFT",
 "comments":{"nodes":[{"id":"C3","author":null,"body":"gone","createdAt":"2026-11-03T12:00:00Z"}]}}
]}}}}}`

func TestThreadsParsesFixture(t *testing.T) {
	r := &seqRunner{rules: []rule{{"repo view", repoJSON, nil}, {"graphql", threadsJSON, nil}}}
	th, err := Client{R: r}.Threads(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(r.calls[1], " ")
	for _, w := range []string{"owner=acme", "name=widgets", "number=9", "reviewThreads(first:100)"} {
		if !strings.Contains(j, w) {
			t.Errorf("graphql args lack %q: %s", w, j)
		}
	}
	if len(th) != 2 {
		t.Fatalf("threads %+v", th)
	}
	a := th[0]
	if a.ID != "T1" || a.Path != "a.go" || a.Line != 12 || a.Side != "RIGHT" || a.Resolved ||
		len(a.Comments) != 2 || a.Comments[0].Who != "bob" || a.Comments[1].Body != "second" ||
		a.Comments[0].At.After(a.Comments[1].At) {
		t.Errorf("a: %+v", a)
	}
	if !th[1].Resolved || th[1].Side != "LEFT" {
		t.Errorf("b: %+v", th[1])
	}
}

func TestThreadsOutdatedUsesOriginalLine(t *testing.T) {
	r := &seqRunner{rules: []rule{{"repo view", repoJSON, nil}, {"graphql", threadsJSON, nil}}}
	th, err := Client{R: r}.Threads(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if th[1].Line != 5 {
		t.Errorf("line %d, want 5", th[1].Line)
	}
	if th[1].Comments[0].Who != "" {
		t.Errorf("deleted author: %+v", th[1].Comments[0])
	}
}

func TestThreadsGraphQLError(t *testing.T) {
	r := &seqRunner{rules: []rule{{"repo view", repoJSON, nil}, {"graphql", `{"errors":[{"message":"bad field"},{"message":"no access"}]}`, nil}}}
	_, err := Client{R: r}.Threads(context.Background(), 9)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") || !strings.Contains(err.Error(), "bad field; no access") {
		t.Errorf("err %v", err)
	}
}

func TestThreadsRepoViewFails(t *testing.T) {
	r := &seqRunner{rules: []rule{{"repo view", "", errString("not a repo")}}}
	_, err := Client{R: r}.Threads(context.Background(), 9)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") || len(r.calls) != 1 {
		t.Errorf("err %v calls %d", err, len(r.calls))
	}
}

func TestThreadsPRNotFound(t *testing.T) {
	r := &seqRunner{rules: []rule{{"repo view", repoJSON, nil}, {"graphql", `{"data":{"repository":{"pullRequest":null}}}`, nil}}}
	_, err := Client{R: r}.Threads(context.Background(), 9)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err %v", err)
	}
}
