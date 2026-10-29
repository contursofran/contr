package state

// SelectedKind tells what a [Selected] row is.
type SelectedKind uint8

// The kinds of selected row.
const (
	// KindWorktree is a worktree row. Its ID is the canonical root path of
	// the checkout.
	KindWorktree SelectedKind = iota + 1
	// KindAgent is an agent row. Its ID is the herdr pane id.
	KindAgent
)

// Selected is the selected sidebar row.
type Selected struct {
	Kind SelectedKind
	ID   string
}

// Selection is the sidebar selection, shared by the spaces column and the
// agents list, so a click in either list moves the card.
type Selection struct {
	current  *Selected
	onChange func()
}

// NewSelection returns an empty Selection. onChange may be nil. It runs
// when the selection changes.
func NewSelection(onChange func()) *Selection { return &Selection{onChange: onChange} }

// Current returns the selected row, or false when there is none.
func (s *Selection) Current() (Selected, bool) {
	if s.current == nil {
		return Selected{}, false
	}
	return *s.current, true
}

// Select sets the selection. onChange runs only when the value changes.
func (s *Selection) Select(sel Selected) {
	if s.current != nil && *s.current == sel {
		return
	}
	s.current = &sel
	s.changed()
}

// Clear clears the selection. onChange runs only when there was one.
func (s *Selection) Clear() {
	if s.current == nil {
		return
	}
	s.current = nil
	s.changed()
}

func (s *Selection) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}
