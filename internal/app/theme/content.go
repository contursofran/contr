package theme

import "github.com/egoist/mygo/ui"

// DiffTintAlpha is the alpha of the tint behind an added or a removed line of a diff block in the
// chat (D261): the one place where a content color fills a line.
const DiffTintAlpha = 0.09

// DiffTint is the tint of a diff line whose text is in color c: content-done for an added line,
// content-failed for a removed one.
func DiffTint(c ui.Color) ui.Color { return c.Alpha(DiffTintAlpha) }

// CodeBase is the color a code block of the chat shows on the stage: hover over the stage (D261).
// The contrast of the code colors is measured on it.
func (p *Palette) CodeBase() ui.Color { return Over(p.Hover, p.StageBase()) }
