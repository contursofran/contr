//go:build bench && e2e

package main

// The automation plugin lets a script capture the window. It is in e2e builds
// only, so the benchmark runs without it.
import _ "github.com/egoist/mygo/plugins/automation"
