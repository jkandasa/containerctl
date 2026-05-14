package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// TagUpdates categorises registry tags that are newer than the configured one.
type TagUpdates struct {
	SameMajor []string // newer tags within the same major (safe to apply)
	NewMajors []string // latest tag for each higher major version
}

// CheckTagUpdates queries the registry for tags newer than the one in image.
// max caps the number of entries in SameMajor (NewMajors returns one per major).
// Returns an empty TagUpdates (not nil) when the current tag is not a semver.
func CheckTagUpdates(ctx context.Context, image string, max int) (*TagUpdates, error) {
	reg, repo, currentTag := parseRef(image)
	client := &http.Client{}

	tags, err := listTagsWithAuth(ctx, client, reg, repo)
	if err != nil {
		return nil, err
	}

	suffix := tagSuffix(currentTag)
	currentVer := parseSemver(currentTag)
	if len(currentVer) == 0 {
		return &TagUpdates{}, nil
	}
	currentMajor := currentVer[0]

	bestTag := make(map[int]string) // major → best tag seen
	bestVer := make(map[int][]int)  // major → semver of best tag

	var sameMajor []string

	for _, t := range tags {
		if tagSuffix(t) != suffix {
			continue
		}
		tVer := parseSemver(t)
		if len(tVer) == 0 {
			continue
		}
		m := tVer[0]
		switch {
		case m == currentMajor && semverGreater(tVer, currentVer):
			sameMajor = append(sameMajor, t)
		case m > currentMajor:
			if prev, ok := bestVer[m]; !ok || semverGreater(tVer, prev) {
				bestTag[m] = t
				bestVer[m] = tVer
			}
		}
	}

	sort.Slice(sameMajor, func(i, j int) bool {
		return semverLess(parseSemver(sameMajor[i]), parseSemver(sameMajor[j]))
	})
	if len(sameMajor) > max {
		sameMajor = sameMajor[len(sameMajor)-max:]
	}

	var majorNums []int
	for m := range bestTag {
		majorNums = append(majorNums, m)
	}
	sort.Ints(majorNums)
	var newMajors []string
	for _, m := range majorNums {
		newMajors = append(newMajors, bestTag[m])
	}

	return &TagUpdates{SameMajor: sameMajor, NewMajors: newMajors}, nil
}

func listTagsWithAuth(ctx context.Context, client *http.Client, reg, repo string) ([]string, error) {
	tags, err := listTags(ctx, client, reg, repo, "")
	if err == nil {
		return tags, nil
	}
	// attempt bearer auth
	authReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://%s/v2/%s/tags/list", reg, repo), nil)
	resp, rerr := client.Do(authReq)
	if rerr != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		tok, terr := bearerToken(ctx, client, resp.Header.Get("Www-Authenticate"), repo)
		if terr == nil {
			return listTags(ctx, client, reg, repo, tok)
		}
	}
	return nil, err
}

func listTags(ctx context.Context, client *http.Client, reg, repo, token string) ([]string, error) {
	url := fmt.Sprintf("https://%s/v2/%s/tags/list", reg, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags/list returned status %d", resp.StatusCode)
	}
	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Tags, nil
}

// tagSuffix returns the non-numeric trailing part of a tag.
// "2.1.2-alpine" → "-alpine"   "1.8.10" → ""   "sha256-abc" → "sha256-abc"
func tagSuffix(tag string) string {
	s := strings.TrimPrefix(tag, "v")
	idx := strings.IndexFunc(s, func(r rune) bool {
		return !((r >= '0' && r <= '9') || r == '.')
	})
	if idx < 0 {
		return ""
	}
	return s[idx:]
}

// parseSemver returns the numeric version parts.
// Returns nil for non-version strings like "latest", "sha256-...", "alpine".
func parseSemver(tag string) []int {
	s := strings.TrimPrefix(tag, "v")
	if idx := strings.IndexFunc(s, func(r rune) bool {
		return !((r >= '0' && r <= '9') || r == '.')
	}); idx >= 0 {
		s = s[:idx]
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums = append(nums, n)
	}
	return nums
}

func semverGreater(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

func semverLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
