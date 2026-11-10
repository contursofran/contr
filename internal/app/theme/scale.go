package theme

// Tokens of docs/spec/tokens.md that have one value in every theme: type
// styles, spacing, radius, sizes and glass effects. The names follow the
// token names. The unprefixed constants of theme.go are the v0.2 ones.

// Font stacks of the two families. Inter and JetBrains Mono are bundled
// (D121); the rest of each stack is the fallback.
const (
	FontSans = `Inter, -apple-system, system-ui, sans-serif`
	FontMono = `"JetBrains Mono", ui-monospace, monospace`
)

// FontFamilySans is the name that the bundled Inter faces register under.
// The mono name is FontFamily.
const FontFamilySans = "Inter"

// TextStyle is a type style: family, size, line height and weight.
type TextStyle struct {
	Mono   bool // mono family; else sans
	Size   float32
	Line   float32
	Weight int
}

// Family is the registered family name of the style: Inter or JetBrains Mono.
func (s TextStyle) Family() string {
	if s.Mono {
		return FontFamily
	}
	return FontFamilySans
}

// Stack is the font stack of the style, for `Font(...)`: the registered
// family first, then the fallbacks of tokens.md.
func (s TextStyle) Stack() string {
	if s.Mono {
		return FontMono
	}
	return FontSans
}

// Type styles. Use the style, not a size.
var (
	StyleQuestion   = TextStyle{Size: 14, Line: 20, Weight: 400}
	StyleProse      = TextStyle{Size: 13.5, Line: 21, Weight: 400}
	StyleBody       = TextStyle{Size: 13, Line: 18, Weight: 400}
	StyleBodyStrong = TextStyle{Size: 13, Line: 18, Weight: 500}
	StyleRowSmall   = TextStyle{Size: 12.5, Line: 18, Weight: 400}
	StyleSmall      = TextStyle{Size: 12, Line: 17, Weight: 400}
	StyleLabel      = TextStyle{Size: 12, Line: 18, Weight: 500}
	StyleChip       = TextStyle{Size: 11.5, Line: 16, Weight: 400}
	StyleTerminal   = TextStyle{Mono: true, Size: 13, Line: 20, Weight: 400}
	StyleValue      = TextStyle{Mono: true, Size: 11.5, Line: 16, Weight: 400}
	StyleMeta       = TextStyle{Mono: true, Size: 11, Line: 16, Weight: 400}
	StyleKbd        = TextStyle{Mono: true, Size: 10.5, Line: 14, Weight: 500}
	StyleTile       = TextStyle{Size: 11, Line: 14, Weight: 600}
	StyleCount      = TextStyle{Mono: true, Size: 11, Line: 14, Weight: 500}
	StyleBadge      = TextStyle{Mono: true, Size: 11, Line: 16, Weight: 600}
)

// Styles maps each style name of tokens.md to its style.
var Styles = map[string]TextStyle{
	"question":    StyleQuestion,
	"prose":       StyleProse,
	"body":        StyleBody,
	"body-strong": StyleBodyStrong,
	"row-small":   StyleRowSmall,
	"small":       StyleSmall,
	"label":       StyleLabel,
	"chip":        StyleChip,
	"terminal":    StyleTerminal,
	"value":       StyleValue,
	"meta":        StyleMeta,
	"count":       StyleCount,
	"badge":       StyleBadge,
}

// Style returns the style with a token name, and false when there is none.
func Style(name string) (TextStyle, bool) {
	s, ok := Styles[name]
	return s, ok
}

// Spacing
const (
	Space2  = 2
	Space4  = 4
	Space6  = 6
	Space7  = 7
	Space8  = 8
	Space9  = 9
	Space10 = 10
	Space12 = 12
	Space14 = 14
	Space16 = 16
	Space18 = 18
	Space20 = 20
)

// Radius
const (
	Radius1  = 1
	Radius2  = 2
	Radius4  = 4
	Radius6  = 6
	Radius7  = 7
	Radius8  = 8
	Radius9  = 9
	Radius12 = 12
	Radius14 = 14
)

// Size
const (
	SizeTitlebarHeight   = 46
	SizeSidebarWidth     = 312
	SizeFilesWidth       = 280
	SizeRowHeight        = 30
	SizeFileRowHeight    = 26
	SizeIconButton       = 28
	SizeTabMaxWidth      = 180
	SizeNotchHeight      = 30
	SizeNotchOpenWidth   = 600
	SizeOutlineCardWidth = 380
	SizeTickHeight       = 2
	SizeMeterHeight      = 4
)

// Effect: backdrop blur and saturation of glass-window and glass-float.
const (
	GlassBlur     = 48
	GlassSaturate = 1.4
)
