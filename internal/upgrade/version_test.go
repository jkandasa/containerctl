package upgrade

import (
	"reflect"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in     string
		track  Track
		semver []int
		extra  bool
		devel  string
	}{
		{"dev", TrackUnknown, nil, false, ""},
		{"", TrackUnknown, nil, false, ""},
		{"v1.2.3", TrackRelease, []int{1, 2, 3}, false, ""},
		{"1.2.3", TrackRelease, []int{1, 2, 3}, false, ""},
		{"v1.2.3-5-gabcdef1", TrackRelease, []int{1, 2, 3}, true, ""},
		{"v1.2.3-dirty", TrackRelease, []int{1, 2, 3}, true, ""},
		{"devel", TrackDevel, nil, false, ""},
		{"devel-abc1234", TrackDevel, nil, false, "abc1234"},
		{"devel-abc1234-dirty", TrackDevel, nil, false, "abc1234"},
		{"v1.2.3-rc.1", TrackRelease, []int{1, 2, 3}, true, ""},
	}
	for _, tc := range tests {
		got := ParseVersion(tc.in)
		if got.Track != tc.track || got.ExtraCommits != tc.extra || got.DevelSHA != tc.devel || !reflect.DeepEqual(got.Semver, tc.semver) {
			t.Errorf("ParseVersion(%q) = track=%d semver=%v extra=%v sha=%q",
				tc.in, got.Track, got.Semver, got.ExtraCommits, got.DevelSHA)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"v1.2.4":        "v1.2.4",
		"1.2.4":         "v1.2.4",
		"devel":         "devel",
		"devel-abc1234": "devel",
		"DEVEL":         "devel",
	}
	for in, want := range tests {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameCommit(t *testing.T) {
	if !SameCommit("abc1234", "abc1234def") {
		t.Fatal("short vs long should match")
	}
	if !SameCommit("ABC1234DEF", "abc1234") {
		t.Fatal("case should not matter")
	}
	if SameCommit("abc1234", "abc9999") {
		t.Fatal("different SHAs")
	}
	if SameCommit("abc", "abcdef0") {
		t.Fatal("fewer than 7 hex chars is not enough")
	}
}

func TestSemverGreater(t *testing.T) {
	if !SemverGreater([]int{1, 2, 4}, []int{1, 2, 3}) {
		t.Fatal("1.2.4 > 1.2.3")
	}
	if SemverGreater([]int{1, 2, 3}, []int{1, 2, 3}) {
		t.Fatal("equal is not greater")
	}
	if SemverGreater([]int{1, 2, 3}, []int{1, 2, 4}) {
		t.Fatal("1.2.3 is not > 1.2.4")
	}
	if !SemverGreater([]int{2, 0, 0}, []int{1, 99, 99}) {
		t.Fatal("2.0.0 > 1.99.99")
	}
}
