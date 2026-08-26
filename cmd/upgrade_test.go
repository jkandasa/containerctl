package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jkandasa/containerctl/internal/upgrade"
)

func TestFetchTargetLatestAndTag(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","assets":[]}`))
	})
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/tags/devel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"devel","body":"Built from commit abcdef1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","assets":[]}`))
	})
	mux.HandleFunc("/repos/jkandasa/containerctl/releases/tags/v1.2.5", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.5","assets":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &upgrade.Client{HTTP: srv.Client(), BaseURL: srv.URL, Repo: upgrade.DefaultRepo}

	rel, err := fetchTarget(context.Background(), c, upgrade.ParseVersion("v1.2.3"), "")
	if err != nil || rel.Tag != "v1.2.4" {
		t.Fatalf("latest: %+v %v", rel, err)
	}

	rel, err = fetchTarget(context.Background(), c, upgrade.ParseVersion("v1.2.3"), "v1.2.5")
	if err != nil || rel.Tag != "v1.2.5" {
		t.Fatalf("explicit: %+v %v", rel, err)
	}

	rel, err = fetchTarget(context.Background(), c, upgrade.ParseVersion("devel-abc1234"), "")
	if err != nil || rel.Tag != "devel" || rel.CommitSHA == "" {
		t.Fatalf("auto devel: %+v %v", rel, err)
	}
}

func TestRemoteLineIncludesCommit(t *testing.T) {
	s := remoteLine(&upgrade.Release{
		Tag:       "devel",
		CommitSHA: "abcdef1234567890",
	})
	if s == "devel" || s == "-" {
		t.Fatalf("expected commit in %q", s)
	}
}
