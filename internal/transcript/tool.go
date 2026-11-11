package transcript

import (
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strings"
)

// ToolKind is the kind of a tool call, from its name (D274).
type ToolKind int

// The kinds of tool calls.
const (
	KindOther ToolKind = iota
	KindSkill
	KindConnector
	KindPlugin
	KindMCP
	KindAgent
	KindWeb
	KindShell
	KindFile
	KindSearch
)

// argsMax is the most key and value pairs that Describe gives.
const argsMax = 6

// mcpKeys are the input keys of an MCP tool, in the order that picks the key
// argument. Plugin tools also read pluginKeys.
var (
	mcpKeys    = []string{"channel", "query", "title", "url", "id", "text", "message", "prompt"}
	pluginKeys = []string{"to", "worker"}
)

// Describe tells the kind of a tool call and how a chat row names it: the
// server or plugin ("" when none), the label ("Slack · send message"), the
// key argument and at most argsMax input pairs (values cut to summaryMax
// runes, keys in order).
func Describe(name string, input json.RawMessage) (kind ToolKind, server, label, arg string, args [][2]string) {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		in = nil
	}
	args = pairs(in)
	label = name
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := in[k].(string); ok && strings.TrimSpace(v) != "" {
				return cut(v, summaryMax)
			}
		}
		return ""
	}
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		srv, tool, _ := strings.Cut(rest, "__")
		keys := mcpKeys
		switch {
		case strings.HasPrefix(srv, "claude_ai_"):
			kind, server = KindConnector, strings.ReplaceAll(strings.TrimPrefix(srv, "claude_ai_"), "_", " ")
		case strings.HasPrefix(srv, "plugin_"):
			kind, keys = KindPlugin, append(append([]string{}, mcpKeys...), pluginKeys...)
			server = strings.TrimPrefix(srv, "plugin_")
			if i := strings.LastIndexByte(server, '_'); i >= 0 && i < len(server)-1 {
				server = server[i+1:]
			}
		default:
			kind, server = KindMCP, srv
		}
		label = server
		if tool != "" {
			label += " · " + strings.ToLower(strings.ReplaceAll(tool, "_", " "))
		}
		return kind, server, label, str(keys...), args
	}
	switch name {
	case "Skill":
		kind = KindSkill
		skill := str("skill")
		if p, s, ok := strings.Cut(skill, ":"); ok {
			server, skill = p, s
		}
		if skill != "" {
			label = "Skill · " + skill
		}
		arg = str("args")
	case "Task", "Agent":
		kind = KindAgent
		t := str("subagent_type")
		if t == "" {
			t = "general"
		}
		label, arg = "Agent · "+t, str("description", "prompt")
	case "WebFetch":
		kind, label, arg = KindWeb, "Fetch", str("url")
		if u, err := url.Parse(arg); err == nil && u.Host != "" {
			arg = strings.TrimSuffix(u.Host+u.Path, "/")
		}
	case "WebSearch":
		kind, label, arg = KindWeb, "Search", str("query")
	case "Bash", "BashOutput", "KillShell":
		kind, arg = KindShell, str("description", "command", "bash_id", "shell_id")
	case "Read", "Edit", "MultiEdit", "Write", "NotebookEdit":
		kind, arg = KindFile, str("file_path", "notebook_path")
		if arg != "" {
			arg = path.Base(arg)
		}
	case "Grep", "Glob":
		kind, arg = KindSearch, str("pattern")
	default:
		arg = str("file_path", "notebook_path", "command", "pattern", "path", "url", "query", "description", "prompt", "skill")
	}
	return kind, server, label, arg, args
}

// pairs gives at most argsMax key and value pairs of in, keys in order.
func pairs(in map[string]any) [][2]string {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out [][2]string
	for _, k := range keys {
		if len(out) == argsMax {
			break
		}
		v, ok := in[k].(string)
		if !ok {
			b, err := json.Marshal(in[k])
			if err != nil {
				continue
			}
			v = string(b)
		}
		out = append(out, [2]string{k, clip(v, summaryMax)})
	}
	return out
}
