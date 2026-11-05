package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/review"
)

const (
	reviewListW = 260
	// reviewBase is the branch that the others are compared with (D27).
	reviewBase = "main"
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

	// start runs fn on a goroutine. post runs fn on the UI thread
	// (Window.Update). Tests replace start with a direct call.
	start func(func())
	post  func(func())
	// want is a file that the next branch read should open, if the branch
	// changes it ([Review.OpenFile]).
	want string
	// invalidate asks for a redraw. Nil in tests.
	invalidate func()
	// gen counts the reads started. A result of an older read is dropped.
	gen uint64
}

// NewReview returns an empty view. post runs a func on the UI thread
// (Window.Update; nil runs it at once). invalidate is Window.Invalidate.
func NewReview(post func(func()), invalidate func()) *Review {
	r := &Review{start: func(f func()) { go f() }, post: post, invalidate: invalidate}
	if r.post == nil {
		r.post = func(f func()) { f() }
	}
	return r
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

func (r *Review) redraw() {
	if r.invalidate != nil {
		r.invalidate()
	}
}

// run reads on a goroutine and applies the result on the UI thread, unless a
// newer read started meanwhile.
func (r *Review) run(read func() func()) {
	r.gen++
	gen := r.gen
	r.start(func() {
		apply := read()
		r.post(func() {
			if gen != r.gen {
				return
			}
			apply()
			r.redraw()
		})
	})
}

// SetRepo shows the repository in dir. An empty dir shows "Select a
// project" and runs no git.
func (r *Review) SetRepo(dir string) { r.setRepo(dir, "") }

// OpenFile shows the repository in dir on its current branch and opens the
// file rel, when that branch changes it; else the first file.
func (r *Review) OpenFile(dir, rel string) { r.setRepo(dir, rel) }

func (r *Review) setRepo(dir, want string) {
	r.want = want
	r.repo = review.Repo{Dir: dir, Base: reviewBase}
	r.branches, r.branch, r.files, r.file = nil, "", nil, ""
	r.commits, r.commit, r.code, r.binary, r.err = nil, "", nil, false, ""
	r.branchesRead = false
	r.mode = reviewWhole
	if dir == "" {
		r.gen++ // drop reads in flight
		r.redraw()
		return
	}
	repo := r.repo
	r.run(func() func() {
		bs, err := repo.Branches()
		cur := ""
		if want != "" {
			cur = repo.Current()
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
	r.redraw()
	repo, byCommit := r.repo, r.ByCommit()
	r.run(func() func() {
		fs, err := repo.Files(name)
		var cs []review.Commit
		if err == nil {
			cs, err = repo.Commits(name)
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
		r.redraw()
	}
}

// SelectCommit lists the files that commit sha changes and opens the first.
func (r *Review) SelectCommit(sha string) {
	r.commit, r.file, r.files, r.code, r.binary = sha, "", nil, nil, false
	r.redraw()
	repo := r.repo
	r.run(func() func() {
		fs, err := repo.CommitFiles(sha)
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
	r.redraw()
	if r.binary {
		r.gen++ // drop reads in flight
		return
	}
	repo := r.repo
	r.run(func() func() {
		before, after, err := repo.Texts(branch, sha, path)
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
			mark := theme.TextDim
			switch f.Status {
			case review.Added:
				mark = theme.DiffAdded
			case review.Deleted:
				mark = theme.DiffDeleted
			}
			kit.Row(c, kit.RowOpts{Label: "file " + f.Path, Mark: string(f.Status), MarkColor: uint32(mark), Text: f.Path,
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
