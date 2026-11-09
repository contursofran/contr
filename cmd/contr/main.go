// Command contr is a native client for herdr-core, written in Go with MyGo's native UI.
// It opens the frame window, or the status icon legend with `--icons`.
// `contr statusline` runs without a window. docs/PORT-PLAN.md lists what the
// port adds next.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/prefs"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/statedir"
	"github.com/contursofran/contr/internal/updated"
	"github.com/contursofran/contr/internal/usage"
	"github.com/contursofran/contr/internal/widths"
	"github.com/egoist/mygo/plugins/terminal"
)

const (
	legendWidth  = 480
	legendHeight = 420
	minHeight    = 600
)

// The smallest frame window, and the width it opens with. Variables so that an
// e2e build can open a narrow window (automation_e2e.go).
var (
	minWidth    = 640
	windowWidth = theme.WindowWidth
)

// screenInset is the gap between the frame window and the edges of the visible frame of
// its screen when it opens (D167). It is the same as the gap between the panels.
const screenInset = 20

// fillScreen makes the frame window open at the screen's visible frame, inset. An e2e
// build with CONTR_E2E_WIDTH keeps its own size (automation_e2e.go).
var fillScreen = true

// insetFrame is the frame the window opens at: work, the visible frame of the screen
// (menu bar and Dock excluded), inset on every side, and not smaller than the minimum
// size. A screen smaller than the minimum gets the minimum, at the corner of work.
func insetFrame(work mygo.Rectangle, inset, minW, minH int) mygo.Rectangle {
	r := mygo.Rectangle{X: work.X + inset, Y: work.Y + inset, Width: work.Width - 2*inset, Height: work.Height - 2*inset}
	if r.Width < minW {
		r.Width, r.X = minW, work.X
	}
	if r.Height < minH {
		r.Height, r.Y = minH, work.Y
	}
	return r
}

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

// bundledFace is a font file in assets and the family it registers under.
type bundledFace struct {
	file   string
	family string
}

// bundledFonts lists the faces that ship with contr. JetBrains Mono (mono)
// has regular, bold, italic and bold italic; they register under
// theme.FontFamily, so italic and bold text pick the matching face. Inter
// (sans, D121) has the weights that the type styles use, 400 and 500; they
// register under theme.FontFamilySans.
var bundledFonts = []bundledFace{
	{"JetBrainsMono-Regular.ttf", theme.FontFamily},
	{"JetBrainsMono-Bold.ttf", theme.FontFamily},
	{"JetBrainsMono-Italic.ttf", theme.FontFamily},
	{"JetBrainsMono-BoldItalic.ttf", theme.FontFamily},
	{"Inter-Regular.ttf", theme.FontFamilySans},
	{"Inter-Medium.ttf", theme.FontFamilySans},
}

// registerFonts loads the bundled faces. A face that fails is reported and the
// UI falls back to a system font.
func registerFonts(files fs.FS, register func([]byte, string) error) {
	for _, f := range bundledFonts {
		data, err := fs.ReadFile(files, "fonts/"+f.file)
		if err == nil {
			err = register(data, f.family)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "contr: unable to load %s: %v\n", f.file, err)
		}
	}
}

// windowOptions are the options of the frame window, or of the legend when
// icons is set. TitleBarHidden keeps the system controls over the 46px title bar the
// view draws; the controls are centered in it. VibrancyGlass makes macOS draw the regular,
// frosted glass behind the view (docs/spec/Window.md, D159).
func windowOptions(icons bool) mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:         "contr",
		Width:         windowWidth,
		Height:        theme.WindowHeight,
		MinWidth:      minWidth,
		MinHeight:     minHeight,
		TitleBarStyle: mygo.TitleBarHidden,
	}
	if !icons {
		opts.TrafficLightPosition = &mygo.Point{X: 14, Y: (theme.TitleBarHeight - 12) / 2}
		opts.Vibrancy = mygo.VibrancyGlass
		// Liquid Glass shows what is behind the window only in a window that
		// is not opaque. In an opaque window it shows the window's own grey
		// background (D158).
		opts.Transparent = true
	}
	if icons {
		opts.Width, opts.Height = legendWidth, legendHeight
		opts.MinWidth, opts.MinHeight = 0, 0
	}
	return opts
}

// activeAppearer is the part of a window that keeps its active look.
type activeAppearer interface{ SetActiveAppearance(bool) }

// keepGlassActive keeps the glass drawn as active when the window is not the key window
// (D153). Without glass, as in e2e builds (D150), there is nothing to keep.
func keepGlassActive(w activeAppearer, opts mygo.WindowOptions) {
	if opts.Vibrancy == mygo.VibrancyNone || opts.Vibrancy == "" {
		return
	}
	w.SetActiveAppearance(true)
}

// frameDeps returns what the frame gets from outside: the stores, the pickers, the agents
// and the update service.
func frameDeps() app.Deps {
	// The model on line 2 of an agent row comes from the profile. A config
	// that does not read is reported by the agent state; the defaults serve here.
	cfg, _ := agents.LoadConfig(configPath(os.Getenv))
	home, _ := os.UserHomeDir()
	return app.Deps{
		Projects: app.StoreAt(projects.Path, projects.Load,
			func(path string, s *projects.Store) error { return s.Save(path) }),
		Widths:  app.StoreAt(widths.Path, widths.Load, widths.Save),
		Prefs:   app.StoreAt(prefs.Path, prefs.Load, prefs.Save),
		Updated: updatedStore(updated.Path(statedir.Dir())),

		StartAgents:    startAgents(os.Getenv, dialAgents),
		AgentsNotice:   cfg.Notice,
		AgentsConfig:   configPath(os.Getenv),
		ModelOf:        cfg.ModelOf,
		EffortOf:       cfg.EffortOf,
		WatchHead:      files.WatchFile,
		TerminalSource: terminalSource(os.Getenv),
		OpenTerminal: func(project, dir string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return openTerminal(ctx, os.Getenv, dialAgents, project, dir, time.Now(), appColors())
		},
		PickFolder: pickFolder,
		SpawnOrchestrator: func(dir string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return spawnOrchestrator(ctx, os.Getenv, dialAgents, dir, time.Now(), appColors())
		},

		OpenFolder:      openFolder,
		CloneParent:     cloneParent(home),
		PickCloneParent: pickCloneParent,
		ProjectKey:      projectKey,

		Updates:       updateService(),
		Relaunch:      func() { mygo.App.Relaunch() },
		ThemePlatform: mygo.Theme,
	}
}

// updatedStore keeps the note of the last install in contr's state folder, at path (D209).
// With no state folder (path ""), there is no note to read and an install cannot record one.
func updatedStore(path string) app.Store[updated.Note] {
	if path == "" {
		return app.Store[updated.Note]{
			Load: func() (updated.Note, error) { return updated.Note{}, nil },
			Save: func(updated.Note) error { return errors.New("no state folder") },
		}
	}
	return app.StoreAt(func() (string, error) { return path, nil }, updated.Load, updated.Save)
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
		frame = app.NewFrame(frameDeps())
		opts.Content = ui.View(frame.View)
	}
	backgroundOptions(&opts)
	mygo.App.WhenReady(func() {
		if frame != nil {
			// The stored appearance applies before the window shows (D119).
			frame.StartTheme()
		}
		if frame != nil && fillScreen {
			// contr keeps no window frame between runs, so every start fills the screen.
			r := insetFrame(mygo.Screen.PrimaryDisplay().WorkArea, screenInset, opts.MinWidth, opts.MinHeight)
			opts.X, opts.Y, opts.Width, opts.Height = r.X, r.Y, r.Width, r.Height
		}
		win := mygo.NewWindow(opts)
		keepGlassActive(win, opts)
		backgroundShow(win)
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
