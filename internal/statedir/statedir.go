// Package statedir finds the folder where contr keeps its own files, and
// names the variable that holds an agent's hc terminal. Files in that folder
// are keyed by the terminal. It has no UI and reads the system only.
package statedir

import (
	"os"
	"path/filepath"
	"runtime"
)

// EnvTerminal names the variable that holds the hc terminal of the agent.
const EnvTerminal = "HC_TERMINAL_ID"

// Dir is where contr keeps its own files. See From. It returns "" when no
// folder can be found.
func Dir() string {
	home, _ := os.UserHomeDir()
	return From(os.Getenv, home, runtime.GOOS == "darwin")
}

// From returns $CONTR_STATE_DIR; else on macOS
// "~/Library/Application Support/contr"; else $XDG_STATE_HOME/contr, or
// ~/.local/state/contr. Empty variables count as unset. The environment and
// home are passed in so tests need no real variables. An empty home means no
// home; the result is "" when no folder can be found.
func From(env func(string) string, home string, macos bool) string {
	if dir := env("CONTR_STATE_DIR"); dir != "" {
		return dir
	}
	if macos {
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library/Application Support/contr")
	}
	if state := env("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "contr")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local/state/contr")
}
