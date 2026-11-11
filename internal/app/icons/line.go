package icons

import "github.com/egoist/mygo/ui"

// LineNames lists the line icons of docs/spec/README.md (Icons): 16px
// drawings that take the color of the text, so a state changes only the text
// color.
var LineNames = [...]string{
	"project", "computer", "server", "folder", "folder-open", "file-go",
	"file-md", "file", "close", "plus", "split", "search", "sidebar-right",
	"filter", "terminal", "settings", "download", "chevron-down",
	"todo", "todo-done", "artifact", "skill", "connector", "plugin", "agent",
	"globe",
}

// Line returns the line icon with this name, or nil when the file is missing
// or does not parse (logged once).
func Line(name string) *ui.SVG { return svg(name) }

// Brand returns the brand mark with this name (`icons/brands/<name>.svg`, from Simple Icons), or
// nil when there is none. A mark has one fill, so it takes the color of the text like a line
// icon (D274).
func Brand(name string) *ui.SVG { return svg(brandPrefix + name) }
