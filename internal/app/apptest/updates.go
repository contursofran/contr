package apptest

import (
	"context"
	"time"
)

// Updates is an update service that the test controls. T is the update info type of package
// settings; apptest does not import settings, so that the tests in settings can use Updates.
type Updates[T any] struct {
	On         bool // what Enabled gives
	Info       *T
	CheckErr   error
	InstallErr error
	Checks     int
	Installs   int
	// Progress is sent to the install callback.
	Progress [][2]int64
	// CheckLimit is the time left to the deadline of the last check's context; zero without one.
	CheckLimit time.Duration
}

// Enabled gives On.
func (f *Updates[T]) Enabled() bool { return f.On }

// Check counts the check and gives Info and CheckErr.
func (f *Updates[T]) Check(ctx context.Context) (*T, error) {
	f.Checks++
	f.CheckLimit = 0
	if d, ok := ctx.Deadline(); ok {
		f.CheckLimit = time.Until(d)
	}
	return f.Info, f.CheckErr
}

// Install counts the install, sends Progress and gives InstallErr.
func (f *Updates[T]) Install(_ context.Context, progress func(done, total int64)) error {
	f.Installs++
	for _, p := range f.Progress {
		progress(p[0], p[1])
	}
	return f.InstallErr
}
