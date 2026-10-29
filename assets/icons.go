// Package assets embeds the files that contr ships: the status icons and the fonts.
// The app icon joins them when a view needs it.
package assets

import "embed"

// Icons holds the status icons as `icons/<name>.svg`.
//
//go:embed icons/*.svg
var Icons embed.FS

// Fonts holds the four faces of JetBrains Mono as `fonts/<name>.ttf`.
//
//go:embed fonts/*.ttf
var Fonts embed.FS
