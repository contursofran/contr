package review

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one item of a folder: a file or a sub-folder.
type Entry struct {
	Name string
	Dir  bool
}

// maxCountBytes is the biggest new file whose lines Changes counts.
const maxCountBytes = 1 << 20

// IsRepo reports whether Dir is inside a git work tree.
func (r Repo) IsRepo(ctx context.Context) bool {
	out, err := r.git(ctx, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Changes lists what the work tree changes against the merge base of Base and
// HEAD (HEAD when Base does not exist): committed, staged and unstaged
// changes, and the new files that git does not ignore. A deleted file is in
// the list with status Deleted. A folder that is not a git work tree gives no
// changes and no error.
func (r Repo) Changes(ctx context.Context) ([]File, error) {
	if !r.IsRepo(ctx) {
		return nil, nil
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		// No commit yet: every file that git does not ignore is new.
		return r.untracked(ctx)
	}
	from := "HEAD"
	if mb, err := r.git(ctx, "merge-base", r.Base, "HEAD"); err == nil {
		from = strings.TrimSpace(mb)
	}
	files, err := r.filesFrom(ctx, []string{"diff", "--no-renames"}, from)
	if err != nil {
		return nil, err
	}
	more, err := r.untracked(ctx)
	if err != nil {
		return nil, err
	}
	return append(files, more...), nil
}

// untracked lists the new files that git does not ignore, with their line
// counts. A file that is too big, or binary, shows without counts.
func (r Repo) untracked(ctx context.Context) ([]File, error) {
	out, err := r.git(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []File
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		f := File{Path: p, Status: Added}
		if data, ok := readSmall(filepath.Join(r.Dir, p)); !ok || bytes.IndexByte(data, 0) >= 0 {
			f.Binary = true
		} else {
			f.Added = lineCount(data)
		}
		files = append(files, f)
	}
	return files, nil
}

// readSmall reads a file of at most maxCountBytes.
func readSmall(path string) ([]byte, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxCountBytes {
		return nil, false
	}
	data, err := os.ReadFile(path)
	return data, err == nil
}

func lineCount(data []byte) int {
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// Entries lists the folder rel (slash-separated, relative to Dir; "" is Dir
// itself) one level deep: folders first, then files, each by name. The .git
// folder and the paths that git ignores are left out. In a folder that is not
// a git work tree nothing is ignored.
func (r Repo) Entries(ctx context.Context, rel string) ([]Entry, error) {
	des, err := os.ReadDir(filepath.Join(r.Dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	var list []Entry
	var paths []string
	for _, de := range des {
		if de.Name() == ".git" {
			continue
		}
		dir := de.IsDir()
		if !dir && de.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(rel), de.Name())); err == nil {
				dir = st.IsDir()
			}
		}
		list = append(list, Entry{Name: de.Name(), Dir: dir})
		paths = append(paths, joinRel(rel, de.Name()))
	}
	ignored := r.ignored(ctx, paths)
	if err := ctx.Err(); err != nil {
		return nil, err // the ignored paths are not known
	}
	kept := list[:0]
	for i, e := range list {
		if !ignored[paths[i]] {
			kept = append(kept, e)
		}
	}
	SortEntries(kept)
	return kept, nil
}

// SortEntries puts folders first, then files, each by name.
func SortEntries(list []Entry) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}

// ignored asks git which of paths it ignores. It returns none when Dir is not
// a git work tree or git fails: the tree then shows every path.
func (r Repo) ignored(ctx context.Context, paths []string) map[string]bool {
	set := map[string]bool{}
	if len(paths) == 0 || !r.IsRepo(ctx) {
		return set
	}
	out, err := r.gitInput(ctx, strings.NewReader(strings.Join(paths, "\x00")+"\x00"), "check-ignore", "-z", "--stdin")
	// Exit status 1 means that no path is ignored.
	if err != nil {
		return set
	}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			set[p] = true
		}
	}
	return set
}

func joinRel(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}
