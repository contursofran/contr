package agentstate

import (
	"context"
	"fmt"
	"os"

	"github.com/egoist/mygo"
)

// watchDir calls fn once the watch is set and after each change in the
// folder dir, until ctx ends. It watches the folder, not the file, because a
// rename replaces the file (decision D32). MyGo waits for the folder to be
// quiet and joins changes in one batch, so fn runs once per batch.
func watchDir(ctx context.Context, dir string, fn func()) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{})
	if err != nil {
		return fmt.Errorf("agentstate: watch: %w", err)
	}
	defer w.Close()
	// A change before the watch started has no event: read once now.
	fn()
	for {
		if _, err := w.Next(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("agentstate: watch: %w", err)
		}
		fn()
	}
}
