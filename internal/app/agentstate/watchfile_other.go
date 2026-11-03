//go:build !darwin

package agentstate

import (
	"context"
	"errors"
)

// watchDir is only available on macOS, where contr runs.
func watchDir(context.Context, string, func()) error {
	return errors.New("agentstate: watching owner.json needs macOS")
}
