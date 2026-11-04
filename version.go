// Package contr holds the version text that is embedded in the program.
package contr

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version returns the first line of the VERSION file (D17), or "dev" when
// that line is empty.
func Version() string {
	line, _, _ := strings.Cut(versionFile, "\n")
	if v := strings.TrimSpace(line); v != "" {
		return v
	}
	return "dev"
}
