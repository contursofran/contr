// Changed for contr from the x/vt module v0.1.0 (MIT, see LICENSE): the
// scrollback is a ring. When it is full, a new line takes the place and the
// memory of the oldest line, instead of moving all lines and copying the new
// one. Push finds the trailing blank cells with a plain compare first.

package vt

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

// DefaultScrollbackSize is the default maximum number of lines in the scrollback buffer.
const DefaultScrollbackSize = 10000

// Scrollback represents a scrollback buffer that stores lines scrolled off the screen.
// It is a ring: when full, a new line takes the place of the oldest.
type Scrollback struct {
	lines    []uv.Line
	head     int // index of the oldest line once the ring is full
	maxLines int
}

// NewScrollback creates a new scrollback buffer with the given maximum number of lines.
func NewScrollback(maxLines int) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	return &Scrollback{
		lines:    make([]uv.Line, 0, min(maxLines, 1000)), // Pre-allocate reasonable capacity
		maxLines: maxLines,
	}
}

// Push adds a line to the scrollback buffer.
// If the buffer is full, the oldest line is removed.
func (s *Scrollback) Push(line uv.Line) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	// Find last non-empty cell to trim trailing empty cells.
	// This helps with wrapping and window resizing.
	lastNonEmpty := -1
	for i := len(line) - 1; i >= 0; i-- {
		c := &line[i]
		if *c == uv.EmptyCell {
			continue // the usual blank cell, without the color compares of Equal
		}
		if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
			lastNonEmpty = i
			break
		}
	}
	line = line[:lastNonEmpty+1]

	if len(s.lines) < s.maxLines {
		s.lines = append(s.lines, slices.Clone(line))
		return
	}
	// Full: the new line takes the oldest line's place and memory.
	old := s.lines[s.head]
	clear(old)
	s.lines[s.head] = append(old[:0], line...)
	s.head = (s.head + 1) % len(s.lines)
}

// PushN adds n lines from the buffer starting at line y to the scrollback.
func (s *Scrollback) PushN(buf *uv.RenderBuffer, y, n int) {
	if s == nil || buf == nil || n <= 0 {
		return
	}

	for i := range min(n, buf.Height()-y) {
		if line := buf.Line(y + i); line != nil {
			s.Push(line)
		}
	}
}

// Len returns the number of lines in the scrollback buffer.
func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return len(s.lines)
}

// MaxLines returns the maximum number of lines the scrollback buffer can hold.
func (s *Scrollback) MaxLines() int {
	if s == nil {
		return 0
	}
	return s.maxLines
}

// SetMaxLines sets the maximum number of lines in the scrollback buffer.
// If the current number of lines exceeds the new maximum, oldest lines are removed.
func (s *Scrollback) SetMaxLines(maxLines int) {
	if s == nil || maxLines <= 0 {
		return
	}

	lines := s.Lines()
	if len(lines) > maxLines {
		// Remove oldest lines
		lines = lines[len(lines)-maxLines:]
	}
	s.lines, s.head, s.maxLines = lines, 0, maxLines
}

// Line returns the line at the given index.
// Index 0 is the oldest line, Len()-1 is the most recent.
// Returns nil if index is out of bounds. The line stays valid until a Push
// takes its place.
func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= len(s.lines) {
		return nil
	}
	return s.lines[(s.head+index)%len(s.lines)]
}

// Lines returns all lines in the scrollback buffer, in a new slice.
// Index 0 is the oldest line. A line stays valid until a Push takes its place.
func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	out := make([]uv.Line, 0, len(s.lines))
	out = append(out, s.lines[s.head:]...)
	return append(out, s.lines[:s.head]...)
}

// Clear removes all lines from the scrollback buffer.
func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	s.lines = s.lines[:0]
	s.head = 0
}

// CellAt returns the cell at the given position in the scrollback buffer.
// x is the column, y is the line index (0 = oldest).
// Returns nil if position is out of bounds.
func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	line := s.Line(y)
	if line == nil || x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}
