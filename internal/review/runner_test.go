package review

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/git"
)

// recordGit runs the real git and records each call and its stdin.
type recordGit struct {
	calls []string
	stdin []string
}

func (g *recordGit) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return g.RunInput(ctx, dir, nil, args...)
}

func (g *recordGit) RunInput(ctx context.Context, dir string, in io.Reader, args ...string) ([]byte, error) {
	g.calls = append(g.calls, strings.Join(args, " "))
	if in != nil {
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		g.stdin = append(g.stdin, string(b))
		in = strings.NewReader(string(b))
	}
	return git.ExecRunner{}.RunInput(ctx, dir, in, args...)
}

func TestRepoRunsGitThroughItsRunner(t *testing.T) {
	s := scratch(t)
	s.write(".gitignore", "out\n")
	s.write("out", "x\n")
	s.write("a.txt", "a\n")
	g := &recordGit{}
	r := Repo{Dir: s.dir, Base: "main", Git: g}
	es, err := r.Entries(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(es), " "); got != ".gitignore a.txt" {
		t.Fatalf("entries %q", got)
	}
	var ignore string
	for _, c := range g.calls {
		if !strings.HasPrefix(c, "-C "+s.dir+" -c core.quotepath=false ") {
			t.Errorf("call %q lacks -C and core.quotepath=false", c)
		}
		if strings.HasSuffix(c, "check-ignore -z --stdin") {
			ignore = c
		}
	}
	if ignore == "" || len(g.stdin) != 1 || g.stdin[0] != ".gitignore\x00a.txt\x00out\x00" {
		t.Fatalf("check-ignore %q, stdin %q", ignore, g.stdin)
	}
}

func TestEntriesFailWhenTheContextEnded(t *testing.T) {
	s := scratch(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.repo().Entries(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	if _, err := s.repo().Branches(ctx); !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "git for-each-ref ") {
		t.Fatalf("error %v, want context.Canceled from git for-each-ref", err)
	}
}
