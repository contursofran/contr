// Package assets embeds the files that contr ships: the status icons.
// Fonts and the app icon join them when a view needs them.
package assets

import "embed"

// Icons holds the status icons as `icons/<name>.svg`.
//
//go:embed icons/*.svg
var Icons embed.FS
