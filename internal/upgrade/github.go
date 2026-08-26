package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repository that publishes containerctl releases.
const DefaultRepo = "jkandasa/containerctl"

const defaultAPI = "https://api.github.com"

var builtFromCommit = regexp.MustCompile(`(?i)built from commit\s+([0-9a-f]{7,40})`)

// Release is a GitHub release we can install from.
type Release struct {
	Tag         string
	Name        string
	Body        string
	CommitSHA   string
	Prerelease  bool
	PublishedAt time.Time
	Assets      []Asset
}

// Asset is a downloadable file attached to a Release.
type Asset struct {
	Name               string
	BrowserDownloadURL string
	Size               int64
	Digest             string // "sha256:hex" when GitHub provides it
}

// Client talks to the GitHub Releases API.
type Client struct {
	HTTP    *http.Client
	BaseURL string // API origin, default https://api.github.com
	Repo    string // owner/name, default DefaultRepo
	Token   string // optional; GITHUB_TOKEN when set
}

// NewClient builds a Client from the environment.
// CONTAINERCTL_GITHUB_REPO overrides the repo; GITHUB_TOKEN is sent when set.
func NewClient() *Client {
	repo := os.Getenv("CONTAINERCTL_GITHUB_REPO")
	if repo == "" {
		repo = DefaultRepo
	}
	return &Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		BaseURL: defaultAPI,
		Repo:    repo,
		Token:   os.Getenv("GITHUB_TOKEN"),
	}
}

// Latest returns the latest non-prerelease release.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	return c.get(ctx, "/repos/"+c.repo()+"/releases/latest")
}

// ByTag returns the release for tag (e.g. "v1.2.4" or "devel").
func (c *Client) ByTag(ctx context.Context, tag string) (*Release, error) {
	tag = NormalizeTag(tag)
	if tag == "" {
		return nil, fmt.Errorf("empty tag")
	}
	return c.get(ctx, "/repos/"+c.repo()+"/releases/tags/"+tag)
}

func (c *Client) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	return DefaultRepo
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPI
}

func (c *Client) get(ctx context.Context, path string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path, nil)
	if err != nil {
		return nil, err
	}
	c.headers(req)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("github: read body: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("github: release not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", strings.TrimSpace(string(body)))
	}
	return parseRelease(body)
}

func (c *Client) headers(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "containerctl")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type ghRelease struct {
	TagName         string    `json:"tag_name"`
	Name            string    `json:"name"`
	Body            string    `json:"body"`
	Draft           bool      `json:"draft"`
	Prerelease      bool      `json:"prerelease"`
	TargetCommitish string    `json:"target_commitish"`
	PublishedAt     time.Time `json:"published_at"`
	Assets          []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

func parseRelease(data []byte) (*Release, error) {
	var raw ghRelease
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("github: decode release: %w", err)
	}
	if raw.Draft {
		return nil, fmt.Errorf("github: release %s is a draft", raw.TagName)
	}
	rel := &Release{
		Tag:         raw.TagName,
		Name:        raw.Name,
		Body:        raw.Body,
		Prerelease:  raw.Prerelease,
		PublishedAt: raw.PublishedAt,
		CommitSHA:   commitFromRelease(raw.Body, raw.TargetCommitish),
	}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset(a))
	}
	return rel, nil
}

func commitFromRelease(body, targetCommitish string) string {
	if m := builtFromCommit.FindStringSubmatch(body); len(m) == 2 {
		return strings.ToLower(m[1])
	}
	t := strings.TrimSpace(targetCommitish)
	if hexSHA.MatchString(t) {
		return strings.ToLower(t)
	}
	return ""
}
