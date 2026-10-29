package board

// The pure parts of the board state: find the bridge pane, find the board
// directory, read one board file. Ported from the Rust contr module board.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/contursofran/contr/internal/herdr"
)

// DirEnv is the environment variable that replaces the board directory.
const DirEnv = "CONTR_BOARD_DIR"

// dirUnderHome is the board directory below $HOME when DirEnv is not set.
const dirUnderHome = ".local/state/herdr-board"

// MaxBytes is the largest board file that is read. A board is a few KB; a
// larger file is refused unparsed.
const MaxBytes int64 = 1 << 20

// The problem lines for the panel.
const (
	// NoBoardDir: no board directory (no HOME and no override).
	NoBoardDir = "no board dir"
	// OtherSession: a board file names another session than the bridge token.
	OtherSession = "board file is for another session"
	// NoBoardFile: the board file is missing. The coordinator may still be
	// writing it.
	NoBoardFile = "no board file yet"
	// BoardUnreadable: the file exists but cannot be read. The OS error text
	// is not shown.
	BoardUnreadable = "board file unreadable"
	// BoardTooLarge: the board file is larger than MaxBytes.
	BoardTooLarge = "board file too large"
)

// BridgePane is the pane that carries a valid bridge token, and what the
// token names.
type BridgePane struct {
	PaneID string
	Bridge BridgeRef
}

// FindBridge returns the bridge pane in tree. A pane in the focused
// workspace wins; otherwise the first in tree order. A token that does not
// parse is ignored.
func FindBridge(tree *herdr.HerdrTree) (BridgePane, bool) {
	if tree == nil {
		return BridgePane{}, false
	}
	var first BridgePane
	haveFirst := false
	for _, ws := range tree.Workspaces {
		for _, tab := range ws.Tabs {
			for _, pane := range tab.Panes {
				value, ok := pane.Tokens[BridgeToken]
				if !ok {
					continue
				}
				ref, ok := ParseBridgeToken(value)
				if !ok {
					continue
				}
				found := BridgePane{PaneID: pane.ID, Bridge: ref}
				if ws.Focused {
					return found, true
				}
				if !haveFirst {
					first, haveFirst = found, true
				}
			}
		}
	}
	return first, haveFirst
}

// Dir returns the board directory: $CONTR_BOARD_DIR when set and not empty,
// else $HOME/.local/state/herdr-board. It returns false when neither is set.
func Dir() (string, bool) {
	return DirFrom(os.Getenv(DirEnv), os.Getenv("HOME"))
}

// DirFrom is [Dir] with the two values given. An empty value is not set.
func DirFrom(override, home string) (string, bool) {
	if override != "" {
		return override, true
	}
	if home != "" {
		return filepath.Join(home, dirUnderHome), true
	}
	return "", false
}

// Read reads and parses one board file. The size is capped before the bytes
// are parsed, and the error is the panel's line, so only that reaches the
// UI. It blocks on file I/O: call it off the UI thread.
func Read(path string) (*Snapshot, string) {
	data, line := readCapped(path, MaxBytes)
	if line != "" {
		return nil, line
	}
	snap, err := Parse(data)
	if err != nil {
		var be Error
		if errors.As(err, &be) {
			return nil, be.Line()
		}
		return nil, err.Error()
	}
	return snap, ""
}

// readCapped reads at most limit bytes. A file longer than limit is a
// problem line, never a truncated read. The line is empty on success.
func readCapped(path string, limit int64) ([]byte, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ioProblem(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, ioProblem(err)
	}
	if int64(len(data)) > limit {
		return nil, BoardTooLarge
	}
	return data, ""
}

// ioProblem is the panel's line for an I/O failure. The OS text is not shown.
func ioProblem(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return NoBoardFile
	}
	return BoardUnreadable
}
