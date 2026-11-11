package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func names(t *testing.T, root, dir string) []string {
	t.Helper()
	list, err := listPaths(context.Background(), root, dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range list {
		out = append(out, it.Name)
	}
	return out
}

func write(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListPathsInGit(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".gitignore", "a/b.go", "c.md", "out/x.log", "new/n.go")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("out/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", ".gitignore", "a", "c.md"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.Remove(filepath.Join(root, "c.md")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nest")
	write(t, nested, "m.go")
	if out, err := exec.Command("git", "-C", nested, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	got := names(t, root, "")
	want := []string{".gitignore", "a/", "a/b.go", "nest/", "new/", "new/n.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %q, want %q (tracked and untracked; not ignored, not deleted; a nested repo once)", got, want)
	}
}

func TestListPathsOutsideGitReadsTheTypedFolder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/b.go", "a/sub/c.go", "d.md")
	if got, want := names(t, root, ""), []string{"a/", "d.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("root = %q, want %q", got, want)
	}
	if got, want := names(t, root, "a/"), []string{"a/sub/", "a/b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a/ = %q, want %q", got, want)
	}
}

func TestListPathsOfAnEmptyFolder(t *testing.T) {
	if got := names(t, t.TempDir(), ""); len(got) != 0 {
		t.Errorf("paths = %q, want none", got)
	}
}
