package main

import (
	"context"
	"errors"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/settings"
)

// mygoUpdates gives the Settings modal MyGo's updater (D115). Check keeps the update that it
// found, and Install installs that one.
type mygoUpdates struct {
	found *mygo.Update
}

func (u *mygoUpdates) Enabled() bool { return mygo.Updater.Enabled() }

func (u *mygoUpdates) Check(ctx context.Context) (*settings.UpdateInfo, error) {
	u.found = nil
	up, err := mygo.Updater.Check(ctx)
	if err != nil {
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return nil, errors.New(settings.TextUpdatesOff)
		}
		return nil, err
	}
	if up == nil {
		return nil, nil
	}
	u.found = up
	return &settings.UpdateInfo{Version: up.Version, Notes: up.Notes}, nil
}

func (u *mygoUpdates) Install(ctx context.Context, progress func(done, total int64)) error {
	if u.found == nil {
		return errors.New("no update to install: check first")
	}
	return u.found.Install(ctx, progress)
}
