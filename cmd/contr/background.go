//go:build !e2e

package main

import "github.com/egoist/mygo"

// Only e2e builds run in the background (background_e2e.go).
func backgroundOptions(*mygo.WindowOptions) {}

func backgroundShow(*mygo.Window) {}
