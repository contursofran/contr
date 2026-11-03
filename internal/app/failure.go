package app

import (
	"errors"
	"fmt"

	"github.com/contursofran/contr/internal/herdr"
)

// failureLine is the status line for a failed herdr command.
func failureLine(err error) string {
	var wire *herdr.WireError
	if errors.As(err, &wire) && wire.IsNotRunning() {
		return "herdr is not running"
	}
	return fmt.Sprintf("herdr: %v", err)
}
