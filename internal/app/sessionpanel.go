package app

// The session sections of the files panel (docs/spec/SessionPanel.md, D197):
// the todos and the artifacts of the selected agent, read from the session
// file of its hc terminal (package session, D195), over the file tree.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/session"
)

// Element labels of the session sections.
const (
	LabelTodos     = "Todos"
	LabelArtifacts = "Artifacts"
	// LabelYouTag is the tag of a todo that came from the owner's prompt.
	LabelYouTag = "you"
)

// claudeArtifactURL starts the link of a claude.ai artifact.
const claudeArtifactURL = "https://claude.ai/artifact/"

// SessionPanel shows the todos and the artifacts of the agent in the
// selected hc terminal. Methods and View run on the UI thread; the file read
// runs on a goroutine and lands through Post. A watch of the session folder
// starts a new read.
type SessionPanel struct {
	// StateDir is the folder of the session files' folder; "" shows nothing.
	StateDir string
	// Terminal gives the selected hc terminal; "" shows nothing.
	Terminal func() string
	// Env reads the session file off the UI thread, redraws and opens web
	// addresses. The zero Env reads at once.
	Env Env
	// Watch watches the file at path and calls notify on a change, from any
	// goroutine. Nil uses mygo.WatchFiles on its folder.
	Watch func(path string, notify func()) (stop func(), err error)
	// Now gives the time for the ages; nil uses time.Now.
	Now func() time.Time

	id        string
	sess      *session.Session
	gen       uint64
	stopWatch func()
}

// sync points the panel at the selected terminal. Cheap when it did not change.
func (s *SessionPanel) sync() {
	id := ""
	if s.Terminal != nil {
		id = s.Terminal()
	}
	if id == s.id {
		return
	}
	s.Close()
	s.id = id
	path := session.Path(s.StateDir, id)
	if path == "" {
		return
	}
	gen := s.gen
	watch := s.Watch
	if watch == nil {
		watch = watchSessionFile
	}
	stop, err := watch(path, func() { s.Env.Post(func() { s.read(gen) }) })
	if err != nil {
		log.Printf("session watch %s: %v", filepath.Base(path), err)
	} else {
		s.stopWatch = stop
	}
	s.read(gen)
}

// Close stops the watch and drops the session shown.
func (s *SessionPanel) Close() {
	if s.stopWatch != nil {
		s.stopWatch()
		s.stopWatch = nil
	}
	s.gen++
	s.id, s.sess = "", nil
}

// read reads the file of the terminal on a goroutine. A result of an older
// terminal (gen) is dropped. A bad file shows nothing and is logged.
func (s *SessionPanel) read(gen uint64) {
	if gen != s.gen {
		return
	}
	dir, id := s.StateDir, s.id
	s.Env.Do(func() func() {
		sess, err := session.Read(dir, id)
		if err != nil {
			log.Printf("session: %v", err)
			sess = nil
		}
		return func() {
			if gen != s.gen {
				return
			}
			s.sess = sess
			s.Env.Redraw()
		}
	})
}

// watchSessionFile watches one session file through its folder, which it
// makes when missing: the hooks write the file with a temp file and a rename.
func watchSessionFile(path string, notify func()) (func(), error) {
	dir, name := filepath.Split(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{
		HintsOnly: true,
		Debounce:  headDebounce,
		Ignore:    func(p string, isDir bool) bool { return !isDir && filepath.Base(p) != name },
	})
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer w.Close()
		for {
			if _, err := w.Next(ctx); err != nil {
				return
			}
			notify()
		}
	}()
	return cancel, nil
}

// View builds the sections. A section with no items is not built.
func (s *SessionPanel) View(c *ui.Context) {
	s.sync()
	if s.sess == nil {
		return
	}
	if len(s.sess.Todos) > 0 {
		s.todos(c, s.sess.Todos)
	}
	if arts := DedupeArtifacts(s.sess.Artifacts); len(arts) > 0 {
		now := time.Now()
		if s.Now != nil {
			now = s.Now()
		}
		s.artifacts(c, arts, now)
	}
}

// TodoCount is the head count: "N of M" done.
func TodoCount(todos []session.Todo) string {
	done := 0
	for _, t := range todos {
		if t.Status == session.Completed {
			done++
		}
	}
	return strconv.Itoa(done) + " of " + strconv.Itoa(len(todos))
}

// section builds one section: the head with its count, the rows, and a
// hairline under them.
func section(c *ui.Context, name, count string, rows func()) {
	p := theme.Current()
	ui.Column(c).Label(name).Shrink(0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space2, theme.Space4, theme.Space4, theme.Space10).Children(func() {
			ui.Text(c, name).SingleLine().Grow(1).MinWidth(0).TextColor(p.Ink45).
				FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
			ui.Text(c, count).SingleLine().Shrink(0).PaddingEnd(theme.Space6).TextColor(p.Ink38).
				Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size)
		})
		rows()
		ui.Box(c).Height(theme.Space12).Shrink(0)
		ui.Box(c).Height(1).Shrink(0).Background(p.Hairline)
		ui.Box(c).Height(theme.Space10).Shrink(0)
	})
}

// TodoLabel is the label of a todo row: its text and its state.
func TodoLabel(t session.Todo) string {
	switch t.Status {
	case session.Completed:
		return t.Text + ", done"
	case session.InProgress:
		return todoText(t) + ", in progress"
	}
	return t.Text + ", pending"
}

// todoText is the text a row shows: the active form while in progress.
func todoText(t session.Todo) string {
	if t.Status == session.InProgress && t.Active != "" {
		return t.Active
	}
	return t.Text
}

func (s *SessionPanel) todos(c *ui.Context, todos []session.Todo) {
	section(c, LabelTodos, TodoCount(todos), func() {
		for i, t := range todos {
			todoRow(c.Key("todo "+strconv.Itoa(i)), t)
		}
	})
}

// sessionRow is the 26px row of the session sections.
func sessionRow(e ui.Element, label string) ui.Element {
	p := theme.Current()
	e = e.Label(label).Height(theme.SizeFileRowHeight).Shrink(0).AlignItems(ui.Center).Gap(theme.Space8).
		PaddingX(theme.Space10).Radius(theme.Radius6)
	if e.Hovered() {
		return e.Background(p.Hover)
	}
	return e.Background(ui.Transparent)
}

func todoRow(c *ui.Context, t session.Todo) {
	p := theme.Current()
	ink, iconInk := p.Ink60, p.Ink35
	switch t.Status {
	case session.Completed:
		ink, iconInk = p.Ink30, p.Ink30
	case session.InProgress:
		ink = p.Ink90
	}
	sessionRow(ui.Row(c), TodoLabel(t)).Children(func() {
		switch t.Status {
		case session.InProgress:
			kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true, Color: ink})
		case session.Completed:
			lineIcon(c, "todo-done", iconInk)
		default:
			lineIcon(c, "todo", iconInk)
		}
		tx := ui.Text(c, todoText(t)).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).TextColor(ink).
			FontSize(theme.StyleRowSmall.Size)
		if t.Status == session.Completed {
			tx.Strikethrough()
		}
		if t.You {
			ui.Text(c, LabelYouTag).Label(LabelYouTag).SingleLine().Shrink(0).TextColor(p.Ink50).
				Font(theme.StyleMeta.Family()).FontSize(10.5).PaddingX(5).
				Radius(theme.Radius4).Border(1, p.InputLine)
		}
	})
}

func lineIcon(c *ui.Context, name string, ink ui.Color) {
	if svg := icons.Line(name); svg != nil {
		ui.Icon(c, svg).Size(14, 14).Shrink(0).TextColor(ink)
	}
}

// ArtifactAge is the time since t as "now", "38m", "5h" or "2d". A time in
// the future, or 0, gives "".
func ArtifactAge(now time.Time, unix uint64) string {
	if unix == 0 {
		return ""
	}
	t := time.Unix(int64(unix), 0)
	if t.After(now) {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// artifactID is the claude.ai id of an artifact when its slug or URL is a
// claude.ai link, else its slug.
func artifactID(a session.Artifact) string {
	for _, s := range []string{a.Slug, a.URL} {
		if id, ok := strings.CutPrefix(s, claudeArtifactURL); ok {
			id, _, _ = strings.Cut(id, "?")
			if id = strings.Trim(id, "/"); id != "" {
				return id
			}
		}
	}
	return a.Slug
}

// artifactURL is the address a click opens: the URL, else a slug that is a link.
func artifactURL(a session.Artifact) string {
	if a.URL != "" {
		return a.URL
	}
	if strings.HasPrefix(a.Slug, claudeArtifactURL) {
		return a.Slug
	}
	return ""
}

// DedupeArtifacts keeps one entry per artifact: the first (most recent) one.
// An entry whose slug is a claude.ai link and an entry with the same id are
// one artifact. A title or URL missing on the kept entry comes from the other.
func DedupeArtifacts(list []session.Artifact) []session.Artifact {
	out := make([]session.Artifact, 0, len(list))
	at := map[string]int{}
	for _, a := range list {
		id := artifactID(a)
		if i, ok := at[id]; ok {
			if out[i].Title == "" {
				out[i].Title = a.Title
			}
			if artifactURL(out[i]) == "" {
				out[i].URL = artifactURL(a)
			}
			continue
		}
		at[id] = len(out)
		out = append(out, a)
	}
	return out
}

// artifactTitle is the row text: the title, else the slug.
func artifactTitle(a session.Artifact) string {
	if a.Title != "" {
		return a.Title
	}
	return a.Slug
}

func (s *SessionPanel) artifacts(c *ui.Context, arts []session.Artifact, now time.Time) {
	p := theme.Current()
	section(c, LabelArtifacts, strconv.Itoa(len(arts)), func() {
		for i, a := range arts {
			title, url := artifactTitle(a), artifactURL(a)
			row := sessionRow(ui.ButtonBase(c.Key("artifact "+strconv.Itoa(i))), "Open "+title).TextColor(p.Ink60)
			if url != "" {
				row = row.Cursor(ui.CursorPointer)
			}
			row.Children(func() {
				lineIcon(c, "artifact", p.Ink38)
				ui.Text(c, title).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).TextColor(p.Ink60).
					FontSize(theme.StyleRowSmall.Size)
				if age := ArtifactAge(now, a.UpdatedAt); age != "" {
					ui.Text(c, age).SingleLine().Shrink(0).TextColor(p.Ink38).
						Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size)
				}
			})
			if row.Clicked() && url != "" {
				s.Env.Open(url)
			}
		}
	})
}
