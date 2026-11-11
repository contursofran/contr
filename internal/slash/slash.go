// Package slash lists the slash commands and skills that Claude Code offers in a session folder
// (D273): its built-in commands, and the commands and skills of the user, the project and the
// installed plugins, each with a one-line description. Claude Code gives no API for this list
// outside its own process, so the reader takes it from the same folders Claude Code reads. A read
// blocks: run it off the UI thread.
package slash

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Item is a command or a skill: its name without the slash and a one-line description.
type Item struct {
	Name, Detail string
}

// Builtins are the commands built into Claude Code.
var Builtins = []Item{
	{"add-dir", "Add a working directory"},
	{"agents", "Manage agent configurations"},
	{"clear", "Clear the conversation history"},
	{"compact", "Clear the history but keep a summary in context"},
	{"config", "Open the settings"},
	{"context", "Show the context usage"},
	{"cost", "Show the cost and duration of the session"},
	{"doctor", "Check the installation"},
	{"exit", "Exit Claude Code"},
	{"help", "Show help and the commands"},
	{"hooks", "Manage hook configurations"},
	{"init", "Write a CLAUDE.md for the codebase"},
	{"mcp", "Manage MCP servers"},
	{"memory", "Edit the memory files"},
	{"model", "Set the model"},
	{"permissions", "Manage the tool permission rules"},
	{"plugin", "Manage plugins"},
	{"resume", "Resume a conversation"},
	{"review", "Review a pull request"},
	{"rewind", "Restore the code and the conversation to an earlier point"},
	{"status", "Show the version, model and account"},
	{"usage", "Show the plan usage limits"},
}

// Reader reads the list for a session folder and keeps it per folder.
type Reader struct {
	// Home is the home folder that holds .claude.
	Home  string
	mu    sync.Mutex
	cache map[string][]Item
}

// Items returns the list for the session folder dir, sorted by name. The first read of a folder
// reads the files; the next ones give the kept list. A missing folder adds nothing.
func (r *Reader) Items(dir string) ([]Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if items, ok := r.cache[dir]; ok {
		return items, nil
	}
	seen := map[string]bool{}
	var items []Item
	add := func(list []Item) {
		for _, it := range list {
			if !seen[it.Name] {
				seen[it.Name] = true
				items = append(items, it)
			}
		}
	}
	if dir != "" {
		add(read(filepath.Join(dir, ".claude"), ""))
	}
	user := filepath.Join(r.Home, ".claude")
	add(read(user, ""))
	for name, path := range plugins(user) {
		add(read(path, name+":"))
	}
	add(Builtins)
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	if r.cache == nil {
		r.cache = map[string][]Item{}
	}
	r.cache[dir] = items
	return items, nil
}

// read lists the commands (commands/**/*.md, a sub folder joined with ":") and the skills
// (skills/*/SKILL.md) under root, each name with prefix.
func read(root, prefix string) []Item {
	var items []Item
	cmds := filepath.Join(root, "commands")
	_ = filepath.WalkDir(cmds, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, _ := filepath.Rel(cmds, strings.TrimSuffix(path, ".md"))
		_, detail := front(path) // a command takes its name from its file
		items = append(items, Item{prefix + strings.ReplaceAll(filepath.ToSlash(rel), "/", ":"), detail})
		return nil
	})
	skills, _ := os.ReadDir(filepath.Join(root, "skills"))
	for _, d := range skills {
		path := filepath.Join(root, "skills", d.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		name, detail := front(path)
		if name == "" {
			name = d.Name()
		}
		items = append(items, Item{prefix + name, detail})
	}
	return items
}

// front reads the name and the description of the frontmatter of the Markdown file at path. With
// no description, the first line of the text is the description.
func front(path string) (name, detail string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	in, first := false, true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case first && line == "---":
			in = true
		case in && line == "---":
			in = false
		case in:
			k, v, ok := strings.Cut(line, ":")
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if ok && k == "name" {
				name = v
			} else if ok && k == "description" {
				detail = v
			}
		case detail == "" && line != "" && !strings.HasPrefix(line, "#"):
			detail = line
		}
		first = false
		if !in && detail != "" {
			break
		}
	}
	return name, detail
}

// plugins gives the install folder of each installed plugin by its name, from
// plugins/installed_plugins.json under user.
func plugins(user string) map[string]string {
	data, err := os.ReadFile(filepath.Join(user, "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var file struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		log.Printf("slash: plugins: %v", err)
		return nil
	}
	out := map[string]string{}
	for id, list := range file.Plugins {
		name, _, _ := strings.Cut(id, "@")
		if len(list) > 0 && list[0].InstallPath != "" {
			out[name] = list[0].InstallPath
		}
	}
	return out
}
