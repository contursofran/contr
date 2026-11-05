// Package assets embeds the files that contr ships: the status icons, the line icons and the fonts.
// The app icon joins them when a view needs it.
package assets

import "embed"

// Icons holds the status icons and the line icons of docs/spec/README.md as
// `icons/<name>.svg`.
//
//go:embed icons/*.svg
var Icons embed.FS

// Fonts holds the faces of JetBrains Mono and Inter as `fonts/<name>.ttf`.
//
//go:embed fonts/*.ttf
var Fonts embed.FS
