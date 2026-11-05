//go:build !e2e

package main

import "github.com/egoist/mygo"

// Only e2e builds run in the background and drop the glass (background_e2e.go).
func backgroundOptions(*mygo.WindowOptions) {}

func backgroundShow(*mygo.Window) {}
