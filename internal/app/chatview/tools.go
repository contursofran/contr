package chatview

import (
	"strings"

	"github.com/contursofran/contr/internal/transcript"
)

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

// brands maps a server name, in lower case with no spaces, "_" or "-", to its brand mark in
// assets/icons/brands (D274). A server that is not here keeps the kind icon.
var brands = map[string]string{
	"slack":          "slack",
	"notion":         "notion",
	"gmail":          "gmail",
	"googlecalendar": "googlecalendar",
	"googledrive":    "googledrive",
	"github":         "github",
	"linear":         "linear",
	"figma":          "figma",
	"mixpanel":       "mixpanel",
	"spotify":        "spotify",
	"jira":           "jira",
	"atlassian":      "atlassian",
	"datadog":        "datadog",
	"intercom":       "intercom",
}

// brandOf gives the brand mark of a call's server, "" when it has none. Only connector, plugin
// and other MCP calls have a server. Case and the prefixes "claude.ai ", "claude_ai_" and
// "plugin_<plugin>_" do not count: "mcp__plugin_design_slack__x" and "claude.ai Slack" are both
// Slack.
func brandOf(call *transcript.Call) string {
	rest, ok := strings.CutPrefix(call.Name, "mcp__")
	if !ok {
		return ""
	}
	srv, _, _ := strings.Cut(rest, "__")
	return brand(srv)
}

// brand gives the brand mark of a server name, "" when it has none.
func brand(server string) string {
	s := strings.ToLower(strings.TrimSpace(server))
	for _, p := range []string{"claude.ai ", "claude_ai_"} {
		s = strings.TrimPrefix(s, p)
	}
	if rest, ok := strings.CutPrefix(s, "plugin_"); ok {
		// "plugin_<plugin>_<server>": the plugin name and the server name can both have "_", so
		// try each tail, longest first.
		for {
			_, tail, more := strings.Cut(rest, "_")
			if !more {
				return ""
			}
			if b := brands[squash(tail)]; b != "" {
				return b
			}
			rest = tail
		}
	}
	return brands[squash(s)]
}

// squash drops the spaces, "_" and "-" of a name.
func squash(s string) string { return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s) }
