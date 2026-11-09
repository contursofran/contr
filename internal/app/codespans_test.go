package app

import (
	"slices"
	"testing"
)

func TestSplitCode(t *testing.T) {
	code := func(s string) textPart { return textPart{Text: s, Code: true} }
	plain := func(s string) textPart { return textPart{Text: s} }
	for _, tc := range []struct {
		name, in string
		want     []textPart
	}{
		{"empty text", "", nil},
		{"none", "Test the bag", []textPart{plain("Test the bag")}},
		{"one", "Run `go run ./tmp/usagecheck` now", []textPart{plain("Run "), code("go run ./tmp/usagecheck"), plain(" now")}},
		{"only code", "`make`", []textPart{code("make")}},
		{"several", "`a` and `b c`.", []textPart{code("a"), plain(" and "), code("b c"), plain(".")}},
		{"unclosed", "Run `go test", []textPart{plain("Run `go test")}},
		{"closed then unclosed", "`a` then `b", []textPart{code("a"), plain(" then `b")}},
		{"empty span", "a `` b", []textPart{plain("a `` b")}},
		{"empty span then code", "`` `x`", []textPart{plain("`` "), code("x")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitCode(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("splitCode(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
