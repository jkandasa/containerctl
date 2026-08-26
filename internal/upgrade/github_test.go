package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const latestJSON = `{
  "tag_name": "v1.2.4",
  "name": "v1.2.4",
  "body": "notes",
  "draft": false,
  "prerelease": false,
  "target_commitish": "main",
  "published_at": "2026-08-01T00:00:00Z",
  "assets": [
    {
      "name": "containerctl-linux-amd64",
      "browser_download_url": "http://example.test/containerctl-linux-amd64",
      "size": 12,
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  ]
}`

const develJSON = `{
  "tag_name": "devel",
  "name": "devel (rolling)",
  "body": "Rolling release from the main branch. Built from commit abcdef1234567890abcdef1234567890abcdef12.\n",
  "draft": false,
  "prerelease": true,
  "target_commitish": "main",
  "published_at": "2026-08-26T12:00:00Z",
  "assets": [
    {
      "name": "containerctl-linux-amd64",
      "browser_download_url": "http://example.test/devel",
      "size": 8
    }
  ]
}`

func TestClientLatestAndByTag(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "" {
			t.Error("missing Accept")
		}
		_, _ = w.Write([]byte(latestJSON))
	})
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/tags/devel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(develJSON))
	})
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/tags/v9.9.9", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL, Repo: DefaultRepo}

	latest, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest.Tag != "v1.2.4" || len(latest.Assets) != 1 {
		t.Fatalf("latest = %+v", latest)
	}

	devel, err := c.ByTag(context.Background(), "devel")
	if err != nil {
		t.Fatal(err)
	}
	if devel.Tag != "devel" {
		t.Fatalf("tag=%s", devel.Tag)
	}
	if devel.CommitSHA != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Fatalf("commit from body: %q", devel.CommitSHA)
	}

	if _, err := c.ByTag(context.Background(), "v9.9.9"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCommitFromRelease(t *testing.T) {
	if got := commitFromRelease("Built from commit abcdef1234567890abcdef1234567890abcdef12.", "main"); got != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Fatalf("body commit: %q", got)
	}
	if got := commitFromRelease("", "abcdef1234567890abcdef1234567890abcdef12"); got != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Fatalf("target_commitish: %q", got)
	}
	if got := commitFromRelease("no sha here", "main"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestAssetNameAndFind(t *testing.T) {
	name, err := AssetName("linux", "amd64")
	if err != nil || name != "containerctl-linux-amd64" {
		t.Fatalf("linux/amd64: %s %v", name, err)
	}
	name, err = AssetName("linux", "arm")
	if err != nil || name != "containerctl-linux-armv7" {
		t.Fatalf("linux/arm: %s %v", name, err)
	}
	name, err = AssetName("windows", "amd64")
	if err != nil || name != "containerctl-windows-amd64.exe" {
		t.Fatalf("windows: %s %v", name, err)
	}
	if _, err := AssetName("plan9", "amd64"); err == nil {
		t.Fatal("expected unsupported")
	}

	rel := &Release{Tag: "v1", Assets: []Asset{{Name: "containerctl-linux-amd64"}}}
	a, err := FindAsset(rel, "linux", "amd64")
	if err != nil || a.Name != "containerctl-linux-amd64" {
		t.Fatalf("find: %v %v", a, err)
	}
	if _, err := FindAsset(rel, "darwin", "arm64"); err == nil {
		t.Fatal("expected missing asset")
	}
}
