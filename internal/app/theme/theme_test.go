package theme

import "testing"

func grey(c uint32) bool {
	r, g, b := c>>16&0xFF, c>>8&0xFF, c&0xFF
	return r == g && g == b
}

// Port of the Rust test tokens_are_greyscale.
func TestTokensAreGreyscale(t *testing.T) {
	colors := map[string]uint32{
		"BgWindow": BgWindow, "BgBars": BgBars, "BgTerminal": BgTerminal,
		"BgCard": BgCard, "CardBorder": CardBorder, "BgAnswer": BgAnswer,
		"AnswerBorder": AnswerBorder, "Line": Line, "Text": Text,
		"Text2": Text2, "TextDim": TextDim, "TextDim2": TextDim2,
		"YouBg": YouBg, "YouText": YouText, "IconYou": IconYou,
		"IconGrey": IconGrey, "IconFaint": IconFaint, "ReportCodeBg": ReportCodeBg,
	}
	for name, c := range colors {
		if !grey(c) {
			t.Errorf("%s is %#06X, which is not grey", name, c)
		}
	}
}

func TestColorSplitsChannels(t *testing.T) {
	c := Color(0x1C2C3C)
	if c.R != 0x1C || c.G != 0x2C || c.B != 0x3C || c.A != 255 {
		t.Errorf("Color(0x1C2C3C) = %+v", c)
	}
}
