package docker

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	registrytypes "github.com/docker/docker/api/types/registry"
)

type dockerConfig struct {
	Auths       map[string]authEntry `json:"auths"`
	CredsStore  string               `json:"credsStore"`
	CredHelpers map[string]string    `json:"credHelpers"`
}

type authEntry struct {
	Auth string `json:"auth"`
}

// registryAuth returns a base64-encoded JSON AuthConfig for the given image.
// It searches credential files in this order:
//  1. authFile argument (explicit path from stack.yaml auth_file)
//  2. $REGISTRY_AUTH_FILE (Podman env override)
//  3. $DOCKER_CONFIG/config.json (Docker env override)
//  4. ~/.docker/config.json (Docker default)
//  5. $XDG_RUNTIME_DIR/containers/auth.json (Podman rootless)
//  6. ~/.config/containers/auth.json (Podman rootless fallback)
//  7. /etc/containers/auth.json (Podman root)
//
// Returns "" if no credentials are found (public image or not logged in).
func registryAuth(authFile, img string) string {
	ref, err := reference.ParseNormalizedNamed(img)
	if err != nil {
		return ""
	}
	serverAddr := reference.Domain(ref)
	canonicalAddr := serverAddr
	if serverAddr == "docker.io" {
		canonicalAddr = "https://index.docker.io/v1/"
	}

	candidates := credFileCandidates()
	if authFile != "" {
		candidates = append([]string{authFile}, candidates...)
	}

	for _, path := range candidates {
		cfg, err := loadAuthFile(path)
		if err != nil {
			continue
		}
		authCfg, err := resolveCredentials(cfg, serverAddr, canonicalAddr)
		if err != nil || (authCfg.Username == "" && authCfg.Password == "") {
			continue
		}
		encoded, err := json.Marshal(authCfg)
		if err != nil {
			continue
		}
		return base64.URLEncoding.EncodeToString(encoded)
	}
	return ""
}

func credFileCandidates() []string {
	var paths []string

	// Podman explicit override
	if v := os.Getenv("REGISTRY_AUTH_FILE"); v != "" {
		paths = append(paths, v)
	}

	// Docker explicit override
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		paths = append(paths, filepath.Join(v, "config.json"))
	}

	home, _ := os.UserHomeDir()

	// Docker default
	if home != "" {
		paths = append(paths, filepath.Join(home, ".docker", "config.json"))
	}

	// Podman rootless — XDG_RUNTIME_DIR
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		paths = append(paths, filepath.Join(v, "containers", "auth.json"))
	}

	// Podman rootless fallback
	if home != "" {
		paths = append(paths, filepath.Join(home, ".config", "containers", "auth.json"))
	}

	// Podman root
	paths = append(paths, "/etc/containers/auth.json")

	return paths
}

func loadAuthFile(path string) (*dockerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func resolveCredentials(cfg *dockerConfig, serverAddr, canonicalAddr string) (registrytypes.AuthConfig, error) {
	// per-registry credential helper takes priority
	helper := cfg.CredHelpers[serverAddr]
	if helper == "" {
		helper = cfg.CredHelpers[canonicalAddr]
	}
	if helper == "" && cfg.CredsStore != "" {
		helper = cfg.CredsStore
	}

	if helper != "" {
		return credHelperGet(helper, canonicalAddr)
	}

	// fall back to inline base64 auth
	entry, ok := cfg.Auths[canonicalAddr]
	if !ok {
		entry = cfg.Auths[serverAddr]
	}
	if entry.Auth == "" {
		return registrytypes.AuthConfig{}, nil
	}
	return decodeAuthEntry(entry.Auth, canonicalAddr)
}

func credHelperGet(helper, serverAddr string) (registrytypes.AuthConfig, error) {
	cmd := exec.Command("docker-credential-"+helper, "get")
	cmd.Stdin = bytes.NewBufferString(serverAddr)
	raw, err := cmd.Output()
	if err != nil {
		return registrytypes.AuthConfig{}, fmt.Errorf("credential helper %s: %w", helper, err)
	}
	var creds struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return registrytypes.AuthConfig{}, err
	}
	return registrytypes.AuthConfig{
		Username:      creds.Username,
		Password:      creds.Secret,
		ServerAddress: serverAddr,
	}, nil
}

func decodeAuthEntry(auth, serverAddr string) (registrytypes.AuthConfig, error) {
	decoded, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return registrytypes.AuthConfig{}, err
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return registrytypes.AuthConfig{}, fmt.Errorf("invalid auth entry for %s", serverAddr)
	}
	return registrytypes.AuthConfig{
		Username:      parts[0],
		Password:      parts[1],
		ServerAddress: serverAddr,
	}, nil
}
