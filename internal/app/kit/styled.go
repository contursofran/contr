package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Styled sets the font of a text from a type style.
func Styled(t ui.Element, st theme.TextStyle) ui.Element {
	return t.Font(st.Stack()).FontSize(st.Size).FontWeight(st.Weight)
}
