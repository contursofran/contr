package main

import (
	"fmt"
	"io"
	"strings"
)

// usage lists every form that the command line accepts.
const usageText = `Usage:
  contr                                       open the window
  contr --icons                               open the window with the status icon legend
  contr statusline [-- <command> [args…]]     print the Claude Code status line
  contr --help, -h                            print this text
`

// kind is what the command line asks for.
type kind int

const (
	// kindWindow opens the window. Command.Icons selects the legend.
	kindWindow kind = iota
	// kindHelp prints the usage. `--help` or `-h`.
	kindHelp
	// kindStatusline is `statusline`. Command.Then is the command after `--`.
	kindStatusline
	// kindUnknown is an argument that is not part of the command line.
	kindUnknown
)

// Command is the parsed command line.
type Command struct {
	Kind kind
	// Icons is set for kindWindow with `--icons`.
	Icons bool
	// Then is the command to chain after the status line, if any.
	Then []string
	// Unknown is the rejected argument for kindUnknown.
	Unknown string
}

// parseArgs reads args, which hold the program name first. `statusline` must be
// the first argument. Otherwise the arguments are read in order: the first
// `--help` or `-h` asks for the usage, and the first argument other than
// `--icons` is unknown.
func parseArgs(args []string) Command {
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "statusline" {
		return Command{Kind: kindStatusline, Then: statuslineCommand(args[1:])}
	}
	icons := false
	for _, arg := range args {
		switch {
		case arg == "--icons":
			icons = true
		case arg == "--help" || arg == "-h":
			return Command{Kind: kindHelp}
		// macOS adds a process serial number when an app starts from Finder or the Dock.
		case strings.HasPrefix(arg, "-psn_"):
		default:
			return Command{Kind: kindUnknown, Unknown: arg}
		}
	}
	return Command{Kind: kindWindow, Icons: icons}
}

// statuslineCommand returns the words after `--`, which must come right after
// `statusline`. Without the separator nothing is chained. The other words are
// not checked, so the status line never fails on them.
func statuslineCommand(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return nil
}

// writeUsage writes the usage text. After an unknown argument, the error line
// comes first.
func writeUsage(w io.Writer, unknown *string) error {
	if unknown != nil {
		if _, err := fmt.Fprintf(w, "contr: unknown argument %s\n", *unknown); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, usageText)
	return err
}

// printUsage is `--help`: the usage on stdout. It returns exit code 1 when
// stdout cannot be written.
func printUsage(stdout io.Writer) int {
	if writeUsage(stdout, nil) != nil {
		return 1
	}
	return 0
}

// rejectArgument prints the error line and the usage on stderr and returns exit
// code 2.
func rejectArgument(arg string, stderr io.Writer) int {
	// With stderr closed there is nowhere else to report. The code is 2 either way.
	_ = writeUsage(stderr, &arg)
	return 2
}

// statusline is `contr statusline`, the Claude Code status line. It runs
// without a window and always returns 0, so the status line never breaks.
func statusline(stdin io.Reader, stdout, stderr io.Writer, then []string) int {
	if err := runStatusline(stdin, stdout, then); err != nil {
		fmt.Fprintf(stderr, "contr statusline: %v\n", err)
	}
	return 0
}
