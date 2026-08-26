package upgrade

import (
	"regexp"
	"strconv"
	"strings"
)

// Track is the kind of binary version string produced at link time.
type Track int

const (
	// TrackUnknown is a local/dev build with no release identity ("dev").
	TrackUnknown Track = iota
	// TrackRelease is a semver tag (v1.2.3) or git-describe based on one.
	TrackRelease
	// TrackDevel is the rolling main-branch build (devel / devel-<sha>).
	TrackDevel
)

// Version is a parsed containerctl Version ldflag.
type Version struct {
	Raw          string
	Track        Track
	Semver       []int  // set when TrackRelease
	ExtraCommits bool   // git-describe or -dirty after the tag
	DevelSHA     string // short or full SHA when TrackDevel
}

var (
	// v1.2.3, v1.2.3-5-gabcdef, v1.2.3-dirty, v1.2.3-5-gabcdef-dirty
	releaseRe = regexp.MustCompile(`(?i)^v?(\d+\.\d+\.\d+)(?:-(\d+)-g([0-9a-f]+))?(?:-dirty)?$`)
	// devel, devel-abcdef1, devel-abcdef1-dirty
	develRe = regexp.MustCompile(`(?i)^devel(?:-([0-9a-f]+))?(?:-dirty)?$`)
	hexSHA  = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)
)

// ParseVersion parses a Version ldflag or a user-supplied --version tag.
func ParseVersion(s string) Version {
	s = strings.TrimSpace(s)
	v := Version{Raw: s}
	if s == "" || strings.EqualFold(s, "dev") || strings.EqualFold(s, "unknown") {
		v.Track = TrackUnknown
		return v
	}
	if m := develRe.FindStringSubmatch(s); m != nil {
		v.Track = TrackDevel
		v.DevelSHA = strings.ToLower(m[1])
		return v
	}
	if m := releaseRe.FindStringSubmatch(s); m != nil {
		v.Track = TrackRelease
		v.Semver = parseSemverParts(m[1])
		v.ExtraCommits = m[2] != "" || strings.HasSuffix(strings.ToLower(s), "-dirty")
		return v
	}
	// Best-effort: leading vX.Y.Z with an unknown suffix (e.g. -rc.1).
	if parts := parseSemverPrefix(s); len(parts) > 0 {
		v.Track = TrackRelease
		v.Semver = parts
		v.ExtraCommits = true
		return v
	}
	v.Track = TrackUnknown
	return v
}

// NormalizeTag maps a user --version value to the GitHub release tag.
// "1.2.4" → "v1.2.4", "devel-abc" → "devel", already-canonical tags pass through.
func NormalizeTag(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.EqualFold(s, "devel") || strings.HasPrefix(strings.ToLower(s), "devel-") {
		return "devel"
	}
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		return "v" + strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	}
	if len(parseSemverPrefix(s)) > 0 {
		return "v" + s
	}
	return s
}

// SemverGreater reports whether a is strictly greater than b.
func SemverGreater(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

func semverEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SameCommit reports whether two git SHAs name the same commit.
// Requires at least 7 hex characters of prefix overlap.
func SameCommit(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if len(a) < 7 || len(b) < 7 || !hexSHA.MatchString(a) || !hexSHA.MatchString(b) {
		return false
	}
	if len(a) > len(b) {
		return strings.HasPrefix(a, b)
	}
	return strings.HasPrefix(b, a)
}

func parseSemverParts(s string) []int {
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func parseSemverPrefix(s string) []int {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	end := 0
	for end < len(s) {
		c := s[end]
		if (c < '0' || c > '9') && c != '.' {
			break
		}
		end++
	}
	if end == 0 {
		return nil
	}
	return parseSemverParts(s[:end])
}
