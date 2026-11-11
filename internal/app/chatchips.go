package app

// The chips of the chat (docs/spec/ChatView.md, D262): the frame's side of chatview.ChipSource.
// A worker's name and a number come from what the frame knows (the agent state, the PR of each
// worker's branch, numbers read by number); a path is checked off the UI thread. A click opens
// the PR or the issue as the agent rows do, selects the worker in the sidebar, or opens the file
// in the Review view as the files panel does.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/view"
)

type fileKey struct{ dir, rel string }

// chatChips is the chat's ChipSource. Use it on the UI thread.
type chatChips struct {
	f   *Frame
	env view.Env
	// refs reads the numbers on screen through gh.
	refs *ghsource.Refs
	// files holds whether a path is a file of a folder; a key without an answer is being read.
	files map[fileKey]bool
	// stat checks a path off the UI thread. Nil: os.Stat.
	stat func(path string) (os.FileInfo, error)
}

func newChatChips(f *Frame, env view.Env) *chatChips {
	var gh *ghsource.GHQueue
	if f.WorkerPRs != nil {
		gh = f.WorkerPRs.GH // one limit for all gh reads (#568)
	}
	return &chatChips{f: f, env: env, refs: &ghsource.Refs{Env: env, GH: gh}, files: map[fileKey]bool{}}
}

// dir is the folder of the session at addr.
func (s *chatChips) dir(addr string) string {
	w, _ := s.f.agentAt(addr)
	return w.Worktree
}

// worker is the worker with this name; a live one wins.
func (s *chatChips) worker(name string) (agents.Worker, bool) {
	var found agents.Worker
	ok := false
	for _, w := range s.f.snapshot().Workers {
		if w.Name == name && (!ok || w.Live && !found.Live) {
			found, ok = w, true
		}
	}
	return found, ok
}

// profileModel is the model of worker w's profile, for the mark of its chip (D292).
func (s *chatChips) profileModel(w agents.Worker) string {
	if s.f.deps.ModelOf == nil || w.Profile == "" {
		return ""
	}
	return s.f.deps.ModelOf(w.Profile)
}

// Look gives the chip of a worker's name, a number or a path in the session at addr.
func (s *chatChips) Look(addr string, t chatview.Token) (chatview.Chip, bool) {
	switch t.Kind {
	case chatview.TokenWord:
		w, ok := s.worker(t.Name)
		return chatview.WorkerChip(w, s.profileModel(w)), ok
	case chatview.TokenNumber:
		n, err := strconv.Atoi(t.Name)
		if err != nil {
			return chatview.Chip{}, false
		}
		r, ok := s.Ref(addr, n)
		return r.Chip, ok
	case chatview.TokenPath:
		if k, ok := fileOf(s.dir(addr), t.Name); ok && s.isFile(k) {
			return chatview.Chip{Kind: chatview.ChipFile}, true
		}
	}
	return chatview.Chip{}, false
}

// Worker is the worker of name with the PR of its branch (D104, D145), for a status board.
func (s *chatChips) Worker(_ string, name string) (chatview.WorkerInfo, bool) {
	w, ok := s.worker(name)
	if !ok {
		return chatview.WorkerInfo{}, false
	}
	info := chatview.WorkerInfo{Chip: chatview.WorkerChip(w, s.profileModel(w))}
	if w.Worktree != "" {
		info.PR = s.f.WorkerPRs.PR(w.Worktree)
	}
	return info, true
}

// Ref is number n in the session at addr: read by number through gh, or the PR of a worker's
// branch before that read lands (D104, D145), with no counts of its checks.
func (s *chatChips) Ref(addr string, n int) (chatview.RefInfo, bool) {
	dir := s.dir(addr)
	if dir == "" {
		return chatview.RefInfo{}, false
	}
	if ref, ok := s.refs.Get(dir, n); ok {
		return chatview.RefInfoOf(ref), true
	}
	for _, w := range s.f.snapshot().Workers {
		if w.Worktree != "" && s.f.WorkerPRs.PR(w.Worktree) == n {
			st := chatview.StateOpen
			if s.f.WorkerPRs.Waiting(w.Worktree) {
				st = chatview.StateRunning
			}
			return chatview.RefInfo{Chip: chatview.Chip{Kind: chatview.ChipPR, State: st}}, true
		}
	}
	return chatview.RefInfo{}, false
}

// fileOf is the key of path p in dir: a path inside dir, relative or absolute.
func fileOf(dir, p string) (fileKey, bool) {
	if dir == "" {
		return fileKey{}, false
	}
	rel := filepath.Clean(p)
	if filepath.IsAbs(rel) {
		r, err := filepath.Rel(dir, rel)
		if err != nil {
			return fileKey{}, false
		}
		rel = r
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return fileKey{}, false
	}
	return fileKey{dir, rel}, true
}

// isFile tells that k is a file, as last read; the first ask reads it off the UI thread and
// redraws when it is one.
func (s *chatChips) isFile(k fileKey) bool {
	if is, ok := s.files[k]; ok {
		return is
	}
	s.files[k] = false
	stat := s.stat
	if stat == nil {
		stat = os.Stat
	}
	s.env.Do(func() func() {
		fi, err := stat(filepath.Join(k.dir, k.rel))
		is := err == nil && fi.Mode().IsRegular()
		return func() {
			s.files[k] = is
			if is {
				s.env.Redraw()
			}
		}
	})
	return false
}

// Open opens the PR or the issue, selects the worker, or opens the file.
func (s *chatChips) Open(addr string, c chatview.Chip, name string) {
	f := s.f
	switch c.Kind {
	case chatview.ChipPR, chatview.ChipIssue:
		n, err := strconv.Atoi(name)
		dir := s.dir(addr)
		if err != nil || dir == "" {
			return
		}
		switch {
		case c.Kind == chatview.ChipIssue:
			f.OpenIssue(dir, n)
		case f.paneOpener() != nil:
			f.paneOpener()(dir, n)
		default:
			f.OpenPRInBrowser(dir, n)
		}
	case chatview.ChipWorker:
		w, ok := s.worker(name)
		if !ok {
			return
		}
		f.reviewOpen = false
		if f.SpacesList != nil {
			f.SpacesList.SelectWhenShown(w.Address)
		} else if f.Selection != nil {
			f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: w.Address})
		}
	case chatview.ChipFile:
		if k, ok := fileOf(s.dir(addr), name); ok {
			f.openFileInReview(k.dir, k.rel)
		}
	}
}
