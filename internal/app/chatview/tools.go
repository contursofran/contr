package chatview

import "github.com/contursofran/contr/internal/transcript"

// kindIcons are the line icons of the tool kinds (D274). KindOther has none.
var kindIcons = map[transcript.ToolKind]string{
	transcript.KindSkill:     "skill",
	transcript.KindConnector: "connector",
	transcript.KindPlugin:    "plugin",
	transcript.KindMCP:       "connector",
	transcript.KindAgent:     "agent",
	transcript.KindWeb:       "globe",
	transcript.KindShell:     "terminal",
	transcript.KindFile:      "file",
	transcript.KindSearch:    "search",
}

// kindIcon gives the line icon of a tool kind, "" when it has none.
func kindIcon(k transcript.ToolKind) string { return kindIcons[k] }
