package questions

// The menu of a blocked worker as read from its screen (D51). The questions card and the chat's
// needs-you line (D252) read and answer it with these two functions, so both send the same keys
// after the same check.

import (
	"github.com/contursofran/contr/internal/prompt"
)

// Menu names a blocked worker's terminal and how to read its menu: Command is its command line
// or profile, Kind its blocked kind (permission, question or auth).
type Menu struct {
	Address, Command, Kind string
	// Read returns the last lines of a terminal. It blocks.
	Read func(address string, lines int) (string, error)
	// SendKeys presses raw keys in a terminal, with no Enter. It blocks.
	SendKeys func(address string, keys []byte) error
}

// ReadMenu reads the worker's screen and parses its menu. ok is false when no menu was found.
// It blocks: call it off the UI thread.
func (m Menu) ReadMenu() (p prompt.Prompt, ok bool, err error) {
	screen, err := m.Read(m.Address, promptLines)
	if err != nil {
		return prompt.Prompt{}, false, err
	}
	p, ok = prompt.Parse(m.Command, m.Kind, screen)
	return p, ok, nil
}

// Choose sends the keys of choice i of seen. It reads the screen again first, and the keys go
// only when the menu is the one the owner saw (D51); changed tells that it is not, and now and
// ok are the menu read. It blocks: call it off the UI thread.
func (m Menu) Choose(seen prompt.Prompt, i int) (now prompt.Prompt, ok, changed bool, err error) {
	now, ok, err = m.ReadMenu()
	if err != nil {
		return now, ok, false, err
	}
	if !ok || !now.Equal(seen) {
		return now, ok, true, nil
	}
	return now, ok, false, m.SendKeys(m.Address, seen.Choices[i].Keys)
}
