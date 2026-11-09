package settings

import (
	"strconv"
	"strings"
)

// Newer tells whether version a is newer than version b. A version is numbers with dots, with an
// optional "v" in front and an optional "-prerelease" after; a prerelease is older than its
// release. When a or b has another form, Newer tells whether they differ: the update service
// offers only versions newer than the running one.
func Newer(a, b string) bool {
	na, pa, okA := parseVersion(a)
	nb, pb, okB := parseVersion(b)
	if !okA || !okB {
		return a != b
	}
	for i := range max(len(na), len(nb)) {
		x, y := field(na, i), field(nb, i)
		if x != y {
			return x > y
		}
	}
	switch {
	case pa == pb:
		return false
	case pa == "":
		return true
	case pb == "":
		return false
	}
	return pa > pb
}

// parseVersion splits a version into its numbers and its prerelease.
func parseVersion(v string) (nums []int, pre string, ok bool) {
	v = strings.TrimPrefix(v, "v")
	v, pre, _ = strings.Cut(v, "-")
	if v == "" {
		return nil, "", false
	}
	for _, f := range strings.Split(v, ".") {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, "", false
		}
		nums = append(nums, n)
	}
	return nums, pre, true
}

// field is the i-th number, or 0 past the end.
func field(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

// JoinNotes joins the release notes of a newer version and the notes contr installed before it,
// newest first, with a blank line between (D226). Each notes text starts with its "## <version>"
// heading. A section of earlier whose version is also a heading in newer is left out, so no
// version shows twice.
func JoinNotes(newer, earlier string) string {
	newer, earlier = strings.TrimSpace(newer), strings.TrimSpace(earlier)
	have := map[string]bool{}
	for _, sec := range noteSections(newer) {
		if v := sectionVersion(sec); v != "" {
			have[v] = true
		}
	}
	parts := []string{}
	if newer != "" {
		parts = append(parts, newer)
	}
	for _, sec := range noteSections(earlier) {
		if v := sectionVersion(sec); v != "" && have[v] {
			continue
		}
		if sec = strings.TrimSpace(sec); sec != "" {
			parts = append(parts, sec)
		}
	}
	return strings.Join(parts, "\n\n")
}

// noteSections splits notes before each "## " heading line.
func noteSections(notes string) []string {
	var secs []string
	var cur []string
	for _, l := range strings.Split(notes, "\n") {
		if strings.HasPrefix(l, "## ") && len(cur) > 0 {
			secs = append(secs, strings.Join(cur, "\n"))
			cur = nil
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		secs = append(secs, strings.Join(cur, "\n"))
	}
	return secs
}

// sectionVersion is the version of a "## <version>" or "## [<version>] …" heading, or "".
func sectionVersion(sec string) string {
	head, _, _ := strings.Cut(sec, "\n")
	rest, ok := strings.CutPrefix(head, "## ")
	if !ok {
		return ""
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.Trim(f[0], "[]"), "v")
}
