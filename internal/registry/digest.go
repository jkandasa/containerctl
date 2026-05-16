// Package registry queries OCI-compatible registries for image manifest digests
// without relying on the Docker or Podman daemon APIs.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpClient is shared across registry calls with a conservative timeout so
// that a slow or unresponsive registry never hangs the caller indefinitely.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Credentials holds optional username/password for private registry access.
type Credentials struct {
	Username string
	Password string
}

// RemoteDigest returns the content digest for image as reported by the registry.
// It handles Docker Hub token auth and generic Bearer auth (quay.io, ghcr.io, etc.)
// without requiring daemon-specific API support.
// Pass nil creds for public images.
func RemoteDigest(ctx context.Context, image string, creds *Credentials) (string, error) {
	reg, repo, tag := parseRef(image)
	return fetchDigest(ctx, httpClient, reg, repo, tag, "", creds)
}

// parseRef splits an image reference into registry host, repository, and tag.
//
//	"nginx:latest"                   → "registry-1.docker.io", "library/nginx",    "latest"
//	"myuser/app:v1"                  → "registry-1.docker.io", "myuser/app",        "v1"
//	"docker.io/library/nginx:1.27"   → "registry-1.docker.io", "library/nginx",    "1.27"
//	"quay.io/foo/bar:v1"             → "quay.io",               "foo/bar",          "v1"
func parseRef(image string) (reg, repo, tag string) {
	tag = "latest"
	// split tag — last colon that doesn't appear after the last slash
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		tag = image[i+1:]
		image = image[:i]
	}

	parts := strings.SplitN(image, "/", 2)
	if len(parts) == 1 {
		// bare name → official Docker Hub library image
		return "registry-1.docker.io", "library/" + parts[0], tag
	}

	first := parts[0]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		// first component is a registry hostname
		reg = first
		repo = parts[1]
		if reg == "docker.io" {
			reg = "registry-1.docker.io"
			// docker.io/nginx → library/nginx
			if !strings.Contains(repo, "/") {
				repo = "library/" + repo
			}
		}
		return reg, repo, tag
	}

	// two components, no registry hostname → Docker Hub user/image
	return "registry-1.docker.io", image, tag
}

func fetchDigest(ctx context.Context, client *http.Client, reg, repo, tag, token string, creds *Credentials) (string, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", reg, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}, ","))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// parse WWW-Authenticate and retry with a token
		wwwAuth := resp.Header.Get("Www-Authenticate")
		if wwwAuth == "" {
			return "", fmt.Errorf("unauthorized and no WWW-Authenticate header")
		}
		tok, err := bearerToken(ctx, client, wwwAuth, repo, creds)
		if err != nil {
			return "", fmt.Errorf("auth: %w", err)
		}
		return fetchDigest(ctx, client, reg, repo, tag, tok, creds)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("image not found on registry (check image name and tag)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry did not return a Docker-Content-Digest header")
	}
	return digest, nil
}

func bearerToken(ctx context.Context, client *http.Client, wwwAuth, repo string, creds *Credentials) (string, error) {
	// WWW-Authenticate: Bearer realm="https://auth.example.com/token",service="...",scope="..."
	params := parseBearer(wwwAuth)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("no realm in WWW-Authenticate: %s", wwwAuth)
	}

	url := realm + "?service=" + params["service"]
	if scope := params["scope"]; scope != "" {
		url += "&scope=" + scope
	} else {
		url += "&scope=repository:" + repo + ":pull"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if creds != nil && creds.Username != "" {
		req.SetBasicAuth(creds.Username, creds.Password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	return body.AccessToken, nil
}

// parseBearer parses a Bearer WWW-Authenticate header into key/value pairs.
func parseBearer(header string) map[string]string {
	out := make(map[string]string)
	s := strings.TrimPrefix(header, "Bearer ")
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return out
}
