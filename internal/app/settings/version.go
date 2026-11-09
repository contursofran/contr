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
