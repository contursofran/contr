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

func TestChipLabel(t *testing.T) {
	for _, tc := range []struct {
		name, title, want string
	}{
		{"none", "Test the bag", "Test the bag"},
		{"one span", "Run `go run ./tmp/usagecheck` now", "Run go run ./tmp/usagecheck now"},
		{"unclosed", "Run `go test", "Run `go test"},
		{"empty title", "", "b-250"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chipLabel(NeedTab{Key: "b-250", Title: tc.title}); got != tc.want {
				t.Errorf("chipLabel(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}
