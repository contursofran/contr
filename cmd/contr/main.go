// Command contr is a native client for herdr-core, written in Go with MyGo's native UI.
// It opens the frame window, or the status icon legend with `--icons`.
// `contr statusline` runs without a window. docs/PORT-PLAN.md lists what the
// port adds next.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
	"github.com/contursofran/contr/internal/widths"
	"github.com/egoist/mygo/plugins/terminal"
)

const (
	legendWidth  = 480
	legendHeight = 420
	// The smallest frame window.
	minWidth  = 960
	minHeight = 600
)

// runStatusline is usage.RunStatusline. Tests replace it.
var runStatusline = usage.RunStatusline

func main() {
	cmd := parseArgs(os.Args)
	switch cmd.Kind {
	case kindHelp:
		os.Exit(printUsage(os.Stdout))
	case kindUnknown:
		os.Exit(rejectArgument(cmd.Unknown, os.Stderr))
	case kindStatusline:
		os.Exit(statusline(os.Stdin, os.Stdout, os.Stderr, cmd.Then))
	case kindAgents:
		os.Exit(runAgents(cmd.Then, os.Stdout, os.Stderr, dialAgents, os.Getenv))
	case kindOwner:
		os.Exit(runOwner(cmd.Then, os.Stdout, os.Stderr, dialAgents, os.Getenv))
	case kindMCP:
		os.Exit(runMCP(os.Stdin, os.Stdout, os.Stderr, dialAgents, os.Getenv))
	case kindTerminalCheck:
		os.Exit(terminalCheck(terminal.Load, terminal.LibraryPath, os.Stdout, os.Stderr))
	case kindHook:
		os.Exit(runHook(cmd.Then, os.Stdin, os.Stdout, os.Stderr, dialAgents, os.Getenv))
	}
	os.Exit(openWindow(cmd.Icons))
}

// bundledFonts lists the four faces of JetBrains Mono: regular, bold, italic
// and bold italic. They register under theme.FontFamily, so italic and bold
// text pick the matching face.
var bundledFonts = []string{
	"JetBrainsMono-Regular.ttf",
	"JetBrainsMono-Bold.ttf",
	"JetBrainsMono-Italic.ttf",
	"JetBrainsMono-BoldItalic.ttf",
}

// registerFonts loads the bundled faces. A face that fails is reported and the
// UI falls back to a system font.
func registerFonts(files fs.FS, register func([]byte, string) error) {
	for _, name := range bundledFonts {
		data, err := fs.ReadFile(files, "fonts/"+name)
		if err == nil {
			err = register(data, theme.FontFamily)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "contr: unable to load %s: %v\n", name, err)
		}
	}
}

// windowOptions are the options of the frame window, or of the legend when
// icons is set. MyGo has no option for the traffic light position (the Rust
// app put them at 14, 13); TitleBarHidden keeps the system controls.
func windowOptions(icons bool) mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:         "contr",
		Width:         theme.WindowWidth,
		Height:        theme.WindowHeight,
		MinWidth:      minWidth,
		MinHeight:     minHeight,
		TitleBarStyle: mygo.TitleBarHidden,
	}
	if icons {
		opts.Width, opts.Height = legendWidth, legendHeight
		opts.MinWidth, opts.MinHeight = 0, 0
	}
	return opts
}

// openWindow opens the frame, or the legend with icons, and runs until the
// last window closes. It returns the exit code.
func openWindow(icons bool) int {
	registerFonts(assets.Fonts, ui.RegisterFont)
	if err := disablePressAndHold(); err != nil {
		log.Printf("contr: press and hold stays on: %v", err)
	}
	app.InitActions()
	var frame *app.Frame
	opts := windowOptions(icons)
	if icons {
		opts.Content = ui.View((&app.Legend{}).View)
	} else {
		frame = app.NewFrame()
		frame.StartAgents = startAgents(os.Getenv)
		frame.LoadProjects = func() (*projects.Store, error) {
			path, err := projects.Path()
			if err != nil {
				return nil, err
			}
			return projects.Load(path)
		}
		frame.SaveProjects = func(s *projects.Store) error {
			path, err := projects.Path()
			if err != nil {
				return err
			}
			return s.Save(path)
		}
		frame.LoadWidths = func() (widths.Widths, error) {
			path, err := widths.Path()
			if err != nil {
				return widths.Widths{}, err
			}
			return widths.Load(path)
		}
		frame.SaveWidths = func(w widths.Widths) error {
			path, err := widths.Path()
			if err != nil {
				return err
			}
			return widths.Save(path, w)
		}
		frame.TerminalSource = terminalSource(os.Getenv)
		frame.OpenTerminal = func(project, dir string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return openTerminal(ctx, os.Getenv, dialAgents, project, dir, time.Now())
		}
		frame.PickFolder = pickFolder
		frame.Updates = &mygoUpdates{}
		frame.Relaunch = func() { mygo.App.Relaunch() }
		frame.SpawnOrchestrator = func(dir string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return spawnOrchestrator(ctx, os.Getenv, dialAgents, dir, time.Now())
		}
		opts.Content = ui.View(frame.View)
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(opts)
		if frame != nil {
			stop := frame.Attach(win)
			win.OnClosed(stop)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
