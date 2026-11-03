package agentstate

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// watchDir calls fn after each change in the folder dir, until ctx ends. It
// watches the folder, not the file, because a rename replaces the file
// (decision D32). It blocks on kqueue: no timer and no polling.
func watchDir(ctx context.Context, dir string, fn func()) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	kq, err := unix.Kqueue()
	if err != nil {
		return fmt.Errorf("agentstate: kqueue: %w", err)
	}
	defer unix.Close(kq)

	const wake = 1
	changes := []unix.Kevent_t{
		{Ident: uint64(f.Fd()), Filter: unix.EVFILT_VNODE, Flags: unix.EV_ADD | unix.EV_CLEAR,
			Fflags: unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE | unix.NOTE_RENAME},
		{Ident: wake, Filter: unix.EVFILT_USER, Flags: unix.EV_ADD | unix.EV_CLEAR},
	}
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		return fmt.Errorf("agentstate: kevent: %w", err)
	}
	// Wake the blocked Kevent when ctx ends.
	stop := context.AfterFunc(ctx, func() {
		_, _ = unix.Kevent(kq, []unix.Kevent_t{{Ident: wake, Filter: unix.EVFILT_USER, Fflags: unix.NOTE_TRIGGER}}, nil, nil)
	})
	defer stop()

	events := make([]unix.Kevent_t, 4)
	for {
		n, err := unix.Kevent(kq, nil, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("agentstate: kevent: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if n > 0 {
			fn()
		}
	}
}
