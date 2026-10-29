//go:build e2e

package main

// The automation plugin lets the e2e test list, click and capture the
// window. It is in e2e builds only.
import _ "github.com/egoist/mygo/plugins/automation"
