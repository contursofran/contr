// Package chatsource gives the chat view the transcript of the selected hc
// terminal (D243): its path, which the kit's hooks save in the terminal's
// session file (package session), and a signal when the transcript changes.
// It does not read the transcript: package transcript does. A reset signal
// tells the reader to start again at offset 0.
package chatsource

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/session"
)

// Source follows the transcript of one hc terminal. It watches the session
// file of the terminal for a new path, and the transcript file for changes.
// Nothing polls: with no change it uses no CPU (D55). Methods run on the UI
// thread; the session file read runs on a goroutine, and the watches reach
// the UI thread through Env.Post.
type Source struct {
	// StateDir is the folder of the session files' folder; "" follows nothing.
	StateDir string
	// Env reads the session file off the UI thread, posts the watch signals
	// to the UI thread and redraws. The zero Env runs all at once.
	Env view.Env
	// Changed runs on the UI thread when the path moved or the transcript
	// changed. reset is true when the path moved, or when the file is
	// shorter than before (it was replaced or removed): read it again from
	// offset 0. Nil: only a redraw.
	Changed func(reset bool)
	// Watch watches the file at path and calls notify after each change,
	// from any goroutine, until stop. A missing folder gives an error that
	// wraps fs.ErrNotExist. Nil uses mygo.WatchFiles on the file's folder.
	Watch func(path string, notify func()) (stop func(), err error)

	id   string
	path string
	rev  uint64
	// reads drops a read of the session file of an older terminal.
	reads       view.Latest
	stopSession func()
	// gen drops the signals of a transcript watch that stopped.
	gen            uint64
	stopTranscript func()
	// size is the last size of the transcript file; -1 while not known.
	size int64
}

// Sync points the source at terminal id; "" stops it. Call it with the
// selected terminal, "" when none is selected or it is gone. Cheap when the
// terminal did not change.
func (s *Source) Sync(id string) {
	if id == s.id {
		return
	}
	s.Close()
	s.id = id
	path := session.Path(s.StateDir, id)
	if path == "" {
		return
	}
	read := s.reads.Bind(s.read)
	stop, err := s.watchSession(path, func() { s.Env.Post(read) })
	if err != nil {
		log.Printf("chat source: watch %s: %v", filepath.Base(path), err)
	} else {
		s.stopSession = stop
	}
	s.read()
}

// Terminal is the terminal the source follows; "" for none.
func (s *Source) Terminal() string { return s.id }

// Path is the transcript path of the terminal; "" while no hook saved one.
// The file may not exist yet (D216).
func (s *Source) Path() string { return s.path }

// Rev goes up by one at each change: a new path or a change of the file.
// It goes up at a reset too.
func (s *Source) Rev() uint64 { return s.rev }

// Close stops the watches and forgets the terminal and its path.
func (s *Source) Close() {
	if s.stopSession != nil {
		s.stopSession()
		s.stopSession = nil
	}
	s.reads.Reset()
	s.stopWatch()
	s.id, s.path = "", ""
}

// read reads the session file on a goroutine and follows its path. A bad
// file keeps the path and is logged; a missing file has no path.
func (s *Source) read() {
	dir, id := s.StateDir, s.id
	s.reads.Do(s.Env, func() func() {
		sess, err := session.Read(dir, id)
		if err != nil {
			log.Printf("chat source: %v", err)
			return nil
		}
		path := sess.TranscriptPath
		return func() { s.follow(path) }
	})
}

// follow moves the transcript watch to path. When the watch could not start
// before (no folder yet), a new read of the same path tries again.
func (s *Source) follow(path string) {
	if path == s.path && (s.stopTranscript != nil || path == "") {
		return
	}
	moved := path != s.path
	s.stopWatch()
	s.path = path
	started := false
	if path != "" {
		gen := s.gen
		watch := s.Watch
		if watch == nil {
			watch = watchFile
		}
		// The size is read on the watch goroutine, off the UI thread.
		stop, err := watch(path, func() {
			size := fileSize(path)
			s.Env.Post(func() {
				if gen == s.gen {
					s.grew(size)
				}
			})
		})
		switch {
		case err == nil:
			s.stopTranscript, started = stop, true
			// The first size, so that a shorter file later is a reset.
			s.Env.Do(func() func() {
				size := fileSize(path)
				return func() {
					if gen == s.gen && s.size < 0 {
						s.size = size
					}
				}
			})
		case !errors.Is(err, fs.ErrNotExist):
			log.Printf("chat source: watch %s: %v", filepath.Base(path), err)
		}
	}
	if moved || started {
		s.changed(moved)
	}
}

// grew takes a new size of the file after a change of it.
func (s *Source) grew(size int64) {
	reset := s.size >= 0 && size < s.size
	s.size = size
	s.changed(reset)
}

// fileSize is the size of the file at path, 0 when it cannot be read: a
// missing file has no text (D216).
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *Source) stopWatch() {
	s.gen++
	s.size = -1
	if s.stopTranscript != nil {
		s.stopTranscript()
		s.stopTranscript = nil
	}
}

func (s *Source) changed(reset bool) {
	s.rev++
	if s.Changed != nil {
		s.Changed(reset)
	}
	s.Env.Redraw()
}

// watchSession watches the session file, and makes its folder first: the
// hooks may write the first file after the watch starts.
func (s *Source) watchSession(path string, notify func()) (func(), error) {
	if s.Watch != nil {
		return s.Watch(path, notify)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return watchFile(path, notify)
}

// watchFile watches one file through its folder, so a file that is made,
// replaced (temp file and rename) or removed is seen too. The other files of
// the folder are not looked at.
func watchFile(path string, notify func()) (func(), error) {
	dir, name := filepath.Split(path)
	ctx, cancel := context.WithCancel(context.Background())
	w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{
		Ignore: func(p string, _ bool) bool { return p != name && p != "." },
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
