package app

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/egoist/mygo"
)

// headDebounce is how long a HEAD change rests before it is reported.
const headDebounce = 150 * time.Millisecond

// WatchFile watches the file at path with mygo.WatchFiles until ctx ends, and
// calls changed on each report of a change. It returns the error that ended
// the watch, nil when ctx ended it or the file's folder went away. Run it on a
// goroutine, once the app is running. The sidebar uses it for a repo's HEAD,
// which git replaces when the branch changes.
func WatchFile(ctx context.Context, path string, changed func()) error {
	// git replaces HEAD in steps: wait until the changes rest, so the read
	// that follows sees the new branch.
	w, err := mygo.WatchFiles(ctx, path, mygo.FileWatchOptions{Debounce: headDebounce, MaxDelay: 4 * headDebounce})
	if err != nil {
		return err
	}
	defer w.Close()
	for {
		if _, err := w.Next(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		changed()
	}
}
