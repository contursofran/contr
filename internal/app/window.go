package app

// The window rules of docs/spec/Window.md: which panels show and how wide.

import "github.com/contursofran/contr/internal/app/theme"

// narrowWindow tells that a window of width win is too narrow for the files
// panel (open point O6, settled by the owner). An unknown width (0) is not
// narrow.
func narrowWindow(win float32) bool { return win > 0 && win < theme.FilesHideBelow }

// fitWidths returns the widths to draw for a window of width win, and whether
// the files panel shows. sidebar and files are the widths the owner set; they
// stay as they are, and only the drawn widths give way so that the terminal
// keeps TerminalMinWidth: the files panel first, down to its minimum, then
// the sidebar down to its own. A closed sidebar draws nothing; its width is
// returned as it is. An unknown window width (0) changes nothing.
func fitWidths(win, sidebar, files float32, sidebarOpen, filesOpen bool) (s, f float32, showFiles bool) {
	showFiles = filesOpen && !narrowWindow(win)
	if win <= 0 {
		return sidebar, files, showFiles
	}
	gaps := float32(2 * theme.PanelGap)
	room := win - gaps - theme.TerminalMinWidth // what the sidebar and files share
	drawn := sidebar
	if !sidebarOpen {
		drawn = 0
	} else {
		room -= theme.PanelGap
	}
	if showFiles {
		room -= theme.PanelGap
		files = max(min(files, room-drawn), theme.PanelMinWidth)
		if drawn+files > room {
			drawn = max(room-files, theme.SidebarMinWidth)
		}
		if drawn+files > room {
			// Not even the two minimums fit: the files panel gives way.
			showFiles = false
			room += theme.PanelGap
		}
	}
	if !showFiles && drawn > room {
		drawn = max(room, theme.SidebarMinWidth)
	}
	if !sidebarOpen {
		drawn = sidebar
	}
	return drawn, files, showFiles
}
