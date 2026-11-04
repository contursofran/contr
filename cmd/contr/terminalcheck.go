package main

import (
	"fmt"
	"io"
)

// terminalCheck is `contr terminal-check`. It loads the terminal library and
// prints its path. The release bundle job runs it on the packaged app, so a
// missing library fails the build. It returns the exit code.
func terminalCheck(load func() error, path func() (string, error), stdout, stderr io.Writer) int {
	if err := load(); err != nil {
		fmt.Fprintf(stderr, "contr: terminal unavailable: %v\n", err)
		return 1
	}
	p, err := path()
	if err != nil {
		fmt.Fprintf(stderr, "contr: terminal library: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, p); err != nil {
		return 1
	}
	return 0
}
