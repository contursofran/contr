// Package assets embeds the files that contr ships: the status icons, the line icons, the fonts
// and the art of the empty terminal.
// The app icon joins them when a view needs it.
package assets

import "embed"

// Icons holds the status icons and the line icons of docs/spec/README.md as
// `icons/<name>.svg`, and the brand marks of servers (Simple Icons, CC0) as
// `icons/brands/<name>.svg`.
//
//go:embed icons/*.svg icons/brands/*.svg icons/github/*.svg
var Icons embed.FS

// Fonts holds the faces of JetBrains Mono and Inter as `fonts/<name>.ttf`.
//
//go:embed fonts/*.ttf
var Fonts embed.FS

// Hands is the empty terminal's background (docs/spec/Terminal.md): a dot drawing of the two
// hands of Michelangelo's The Creation of Adam, made by us from a public-domain photo of the
// fresco on Wikimedia Commons. Rows of dots, every second row empty: big dots for lit skin,
// very small dots for shadow. The dots are white and their strength is the alpha, so a tint
// gives them a token's color. The gap between the fingertips is at HandsGapX, HandsGapY.
//
//go:embed art/hands.png
var Hands []byte

// HandsGapX and HandsGapY place the gap between the fingertips of Hands, as fractions of its
// width and height.
const (
	HandsGapX = 0.49
	HandsGapY = 0.56
)
