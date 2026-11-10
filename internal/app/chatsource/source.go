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
// thread. The watches start and the session file is read on goroutines. A
// change of the transcript reaches the UI thread through Env.PostQuiet and
// asks for no redraw: a working session writes its transcript many times a
// second, and the window shows nothing of it while the chat does not show
// (#660, D257).
type Source struct {
	// StateDir is the folder of the session files' folder; "" follows nothing.
	StateDir string
	// Env starts the watches and reads the session file off the UI thread,
	// and posts the watch signals to the UI thread. The zero Env runs all at
	// once.
	Env view.Env
	// Changed runs on the UI thread when the path moved or the transcript
	// changed. reset is true when the path moved, or when the file is
	// shorter than before (it was replaced or removed): read it again from
	// offset 0. The source asks for no redraw: Changed does, when what shows
	// changed.
	Changed func(reset bool)
	// Watch watches the file at path and calls notify after each change,
	// from any goroutine, until stop. A missing folder gives an error that
	// wraps fs.ErrNotExist. Nil uses mygo.WatchFiles on the file's folder.
	Watch func(path string, notify func()) (stop func(), err error)

	id   string
	path string
	rev  uint64
	// reads drops a read of the session file of an older terminal.
	reads view.Latest
	// syncs drops a session watch that started after Close.
	syncs       uint64
	stopSession func()
	// gen drops the signals of a transcript watch that stopped, and a watch
	// that started after it stopped. starting is set while the watch of the
	// path next starts.
	gen            uint64
	starting       bool
	next           string
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
	// A watch start lists the folder: it runs off the UI thread (#660). The
	// first read comes after it, so no change of the file is lost.
	watch, syncs := s.Watch, s.syncs
	s.Env.Do(func() func() {
		stop, err := watchSession(watch, path, func() { s.Env.Post(read) })
		return func() {
			switch {
			case err != nil:
				log.Printf("chat source: watch %s: %v", filepath.Base(path), err)
			case syncs != s.syncs:
				stop()
				return
			default:
				s.stopSession = stop
			}
			read()
		}
	})
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
	s.syncs++
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

// follow moves the transcript watch to path. The watch starts off the UI
// thread (#660). Path keeps the old path until the watch started, or
// failed: then the path moves and the change signal comes, in one step, so
// no read takes the new file at the old offset. When the watch could not
// start before (no folder yet), a new read of the same path tries again.
func (s *Source) follow(path string) {
	if s.starting && path == s.next {
		return
	}
	if !s.starting && path == s.path && (s.stopTranscript != nil || path == "") {
		return
	}
	s.stopWatch()
	if path == "" {
		if s.path != "" {
			s.path = ""
			s.changed(true)
		}
		return
	}
	gen := s.gen
	watch := s.Watch
	if watch == nil {
		watch = watchFile
	}
	s.starting, s.next = true, path
	s.Env.Do(func() func() {
		// The sizes are read on the watch goroutine, off the UI thread.
		stop, err := watch(path, func() {
			size := fileSize(path)
			s.Env.PostQuiet(func() {
				if gen == s.gen {
					s.grew(size)
				}
			})
		})
		// The first size, so that a shorter file later is a reset.
		size := fileSize(path)
		return func() {
			if gen != s.gen {
				if stop != nil {
					stop()
				}
				return
			}
			s.starting = false
			moved := path != s.path
			s.path = path
			started := err == nil
			switch {
			case started:
				s.stopTranscript = stop
				if s.size < 0 {
					s.size = size
				}
			case !errors.Is(err, fs.ErrNotExist):
				log.Printf("chat source: watch %s: %v", filepath.Base(path), err)
			}
			if moved || started {
				s.changed(moved)
			}
		}
	})
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
	s.starting = false
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
}

// watchSession watches the session file with watch (nil: watchFile), and
// makes its folder first: the hooks may write the first file after the
// watch starts. Any goroutine.
func watchSession(watch func(string, func()) (func(), error), path string, notify func()) (func(), error) {
	if watch != nil {
		return watch(path, notify)
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
