package app

import (
	"context"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/review"
)

const (
	reviewListW = 260
	// reviewBase is the branch that the others are compared with (D27).
	reviewBase = "main"
	// gitReadTimeout is the time that one read of the Review view or of the
	// file tree may run. A git that hangs then ends with an error.
	gitReadTimeout = 30 * time.Second
)

// The two modes of the switch, as option indexes.
const (
	reviewWhole = iota
	reviewByCommit
)

// Review is the Review view: the branches that are ahead of main, the files
// of the selected branch (or the commits and their files), and the selected
// file as a code view. Methods and View run on the UI thread. The git reads
// run on goroutines (see [Review.run]).
type Review struct {
	repo review.Repo

	branches   []review.Branch
	branch     string
	files      []review.File
	fileState  ui.ListState
	fileSel    int // row chosen in the file list, -1 for none
	fileSynced int // row of the selected file at the last build
	file       string
	commits    []review.Commit // of the selected branch, oldest first
	commit     string          // sha of the selected commit, in By commit mode
	code       *editor.DiffState
	binary     bool
	err        string
	// branchesRead is true once the branches of the repo are read.
	branchesRead bool

	mode int // reviewWhole or reviewByCommit

	// Env runs the git reads off the UI thread and redraws. Tests set
	// Env.Run.Go to a direct call.
	Env Env
	// want is a file that the next branch read should open, if the branch
	// changes it ([Review.OpenFile]).
	want string
	// gen counts the reads started. A result of an older read is dropped.
	gen uint64
	// cancel ends the read in flight; nil when none is.
	cancel context.CancelFunc
	// git runs git; nil runs the `git` command. timeout is the time limit
	// of one read; 0 is gitReadTimeout. Tests set both.
	git     git.InputRunner
	timeout time.Duration
}

// NewReview returns an empty view that reads and redraws through env.
func NewReview(env Env) *Review {
	return &Review{Env: env}
}

// State for tests and for the frame.
func (r *Review) Branch() string            { return r.branch }
func (r *Review) File() string              { return r.file }
func (r *Review) Commit() string            { return r.commit }
func (r *Review) Err() string               { return r.err }
func (r *Review) Files() []review.File      { return r.files }
func (r *Review) Commits() []review.Commit  { return r.commits }
func (r *Review) Branches() []review.Branch { return r.branches }
func (r *Review) Code() *editor.DiffState   { return r.code }
func (r *Review) ByCommit() bool            { return r.mode == reviewByCommit }

// run reads on a goroutine and applies the result on the UI thread, unless a
// newer read started meanwhile. It ends the read in flight. read gets a
// context that ends when a newer read starts, when the view closes, or at
// the time limit.
func (r *Review) run(read func(ctx context.Context) func()) {
	r.drop()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	gen, limit := r.gen, r.timeout
	if limit <= 0 {
		limit = gitReadTimeout
	}
	r.Env.Do(func() func() {
		rctx, stop := context.WithTimeout(ctx, limit)
		apply := read(rctx)
		stop()
		return func() {
			if gen != r.gen {
				return
			}
			r.cancel()
			r.cancel = nil
			apply()
			r.Env.Redraw()
		}
	})
}

// drop ends the read in flight, and its result is not applied.
func (r *Review) drop() {
	r.gen++
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Close ends the read in flight, and its result is not applied. Call it
// when the view ends (the window closes).
func (r *Review) Close() { r.drop() }

// SetRepo shows the repository in dir. An empty dir shows "Select a
// project" and runs no git.
func (r *Review) SetRepo(dir string) { r.setRepo(dir, "") }

// OpenFile shows the repository in dir on its current branch and opens the
// file rel, when that branch changes it; else the first file.
func (r *Review) OpenFile(dir, rel string) { r.setRepo(dir, rel) }

func (r *Review) setRepo(dir, want string) {
	r.want = want
	r.repo = review.Repo{Dir: dir, Base: reviewBase, Git: r.git}
	r.branches, r.branch, r.files, r.file = nil, "", nil, ""
	r.commits, r.commit, r.code, r.binary, r.err = nil, "", nil, false, ""
	r.branchesRead = false
	r.mode = reviewWhole
	if dir == "" {
		r.drop()
		r.Env.Redraw()
		return
	}
	repo := r.repo
	r.run(func(ctx context.Context) func() {
		bs, err := repo.Branches(ctx)
		cur := ""
		if want != "" {
			cur = repo.Current(ctx)
		}
		return func() {
			r.branches, r.err, r.branchesRead = bs, errText(err), true
			if err == nil && len(bs) > 0 {
				name := bs[0].Name
				for _, b := range bs {
					if b.Name == cur {
						name = cur
					}
				}
				r.selectBranch(name)
			}
		}
	})
}

// SelectBranch lists the files and commits of name and opens the first file
// (Whole branch) or the first commit (By commit).
func (r *Review) SelectBranch(name string) { r.selectBranch(name) }

func (r *Review) selectBranch(name string) {
	r.branch, r.file, r.files, r.code, r.binary = name, "", nil, nil, false
	r.commit, r.commits = "", nil
	r.Env.Redraw()
	repo, byCommit := r.repo, r.ByCommit()
	r.run(func(ctx context.Context) func() {
		fs, err := repo.Files(ctx, name)
		var cs []review.Commit
		if err == nil {
			cs, err = repo.Commits(ctx, name)
		}
		return func() {
			r.files, r.commits, r.err = fs, cs, errText(err)
			if err != nil {
				return
			}
			if byCommit && len(cs) > 0 {
				r.SelectCommit(cs[0].SHA)
			} else if !byCommit && len(fs) > 0 {
				first := fs[0].Path
				for _, f := range fs {
					if f.Path == r.want {
						first = r.want
					}
				}
				r.want = ""
				r.SelectFile(first)
			}
		}
	})
}

// SetMode switches between Whole branch and By commit, reading the selected
// branch again.
func (r *Review) SetMode(mode int) {
	if r.mode == mode {
		return
	}
	r.mode = mode
	if r.branch != "" {
		r.selectBranch(r.branch)
	} else {
		r.Env.Redraw()
	}
}

// SelectCommit lists the files that commit sha changes and opens the first.
func (r *Review) SelectCommit(sha string) {
	r.commit, r.file, r.files, r.code, r.binary = sha, "", nil, nil, false
	r.Env.Redraw()
	repo := r.repo
	r.run(func(ctx context.Context) func() {
		fs, err := repo.CommitFiles(ctx, sha)
		return func() {
			r.files, r.err = fs, errText(err)
			if err == nil && len(fs) > 0 {
				r.SelectFile(fs[0].Path)
			}
		}
	})
}

// SelectFile shows the whole file with its changes: of the selected branch,
// or in By commit mode of the selected commit.
func (r *Review) SelectFile(path string) {
	branch, sha := r.branch, ""
	if r.ByCommit() {
		sha = r.commit
	}
	r.file, r.code, r.binary = path, nil, false
	for _, f := range r.files {
		if f.Path == path {
			r.binary = f.Binary
		}
	}
	r.Env.Redraw()
	if r.binary {
		r.drop()
		return
	}
	repo := r.repo
	r.run(func(ctx context.Context) func() {
		before, after, err := repo.Texts(ctx, branch, sha, path)
		return func() {
			r.err = errText(err)
			if err == nil {
				r.code = kit.NewCode(path, before, after)
			}
		}
	})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// View builds the three columns.
func (r *Review) View(c *ui.Context) {
	ui.Row(c).Fill().Background(theme.Color(theme.BgWindow)).
		Font(theme.FontFamily).FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
		TextColor(theme.Color(theme.Text)).Children(func() {
		r.branchList(c)
		r.fileList(c)
		r.codePane(c)
	})
}

func (r *Review) column(c *ui.Context, key string, bg uint32, body func()) {
	ui.Column(c.Key(key)).Width(reviewListW).Shrink(0).FillHeight().Background(theme.Color(bg)).
		BorderWidth(0, 1, 0, 0).BorderColor(theme.Color(theme.Line)).Children(body)
}

func (r *Review) heading(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(theme.Color(theme.TextDim)).Padding(10, 12).Shrink(0)
}

func (r *Review) branchList(c *ui.Context) {
	r.column(c, "branches", theme.BgBars, func() {
		r.heading(c, "Branches")
		switch {
		case r.repo.Dir == "":
			r.note(c, "Select a project")
		case r.err != "" && len(r.branches) == 0:
			r.note(c, r.err)
		case len(r.branches) == 0 && r.branch == "":
			// Nothing is shown while the first read runs; an empty result
			// is told once the read is done (err stays empty).
			if r.loaded() {
				r.note(c, "No branches ahead of "+reviewBase)
			}
		}
		ui.Scroll(c.Key("branchlist")).Fill().Children(func() {
			for _, b := range r.branches {
				r.row(c, kit.RowOpts{Label: "branch " + b.Name, Text: b.Name, Sub: commitsText(b.Ahead),
					Counts: true, Added: b.Added, Removed: b.Removed,
					Selected: b.Name == r.branch, OnClick: func() { r.SelectBranch(b.Name) }})
			}
		})
	})
}

// loaded tells that the branches read is done.
func (r *Review) loaded() bool { return r.branchesRead }

func commitsText(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return strconv.Itoa(n) + " commits"
}

func (r *Review) note(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(theme.Color(theme.TextDim)).Padding(0, 12).Shrink(0)
}

func (r *Review) row(c *ui.Context, o kit.RowOpts) {
	ui.Column(c.Key(o.Label)).FillWidth().Shrink(0).Children(func() { kit.Row(c, o) })
}

func (r *Review) fileList(c *ui.Context) {
	r.column(c, "files", theme.BgWindow, func() {
		ui.Row(c.Key("mode")).FillWidth().Shrink(0).Padding(8, 12).Children(func() {
			mode := r.mode
			kit.Switch(c, "review mode", &mode, "Whole branch", "By commit").Grow(1)
			r.SetMode(mode)
		})
		if r.ByCommit() {
			r.heading(c, "Commits")
			ui.Scroll(c.Key("commits")).Fill().Children(func() {
				for _, cm := range r.commits {
					r.row(c, kit.RowOpts{Label: "commit " + cm.Short, Mark: cm.Short, Text: cm.Subject,
						Counts: true, Added: cm.Added, Removed: cm.Removed,
						Selected: cm.SHA == r.commit, OnClick: func() { r.SelectCommit(cm.SHA) }})
				}
			})
		}
		r.heading(c, "Files")
		r.fileState.Key = func(i int) any { return r.files[i].Path }
		r.fileState.Label = func(i int) string { return r.files[i].Path }
		// An arrow key moved the row since the last build: open that file.
		if r.fileSel >= 0 && r.fileSel < len(r.files) && r.fileSel != r.fileSynced && r.files[r.fileSel].Path != r.file {
			r.SelectFile(r.files[r.fileSel].Path)
		}
		r.fileSel, r.fileSynced = -1, -1
		for i, f := range r.files {
			if f.Path == r.file {
				r.fileSel, r.fileSynced = i, i
			}
		}
		r.fileState.Selected = &r.fileSel
		ui.List(c.Key("filelist"), &r.fileState, len(r.files), func(i int) {
			f := r.files[i]
			p := theme.Current()
			mark := p.Ink50
			switch f.Status {
			case review.Added:
				mark = p.ANSI[2]
			case review.Deleted:
				mark = p.ANSI[1]
			}
			kit.Row(c, kit.RowOpts{Label: "file " + f.Path, Mark: string(f.Status), MarkColor: mark, Text: f.Path,
				Counts: true, Added: f.Added, Removed: f.Removed,
				Selected: f.Path == r.file, OnClick: func() { r.SelectFile(f.Path) }})
		}).Fill()
	})
}

func (r *Review) codePane(c *ui.Context) {
	ui.Column(c.Key("code")).Grow(1).MinWidth(0).FillHeight().Children(func() {
		title := r.file
		if title == "" {
			title = "Select a file"
		}
		r.heading(c, title)
		switch {
		case r.err != "" && r.repo.Dir != "":
			r.note(c, r.err)
		case r.binary:
			r.note(c, "Binary file")
		case r.code != nil:
			kit.CodeView(c, r.code).Fill()
		}
	})
}
