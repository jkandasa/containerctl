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
// Credentials are merged from all auto-detected files plus the explicit authfile:
//   - Auto-detected files (first-wins among them):
//     $REGISTRY_AUTH_FILE, $DOCKER_CONFIG/config.json, ~/.docker/config.json,
//     $XDG_RUNTIME_DIR/containers/auth.json, ~/.config/containers/auth.json,
//     /etc/containers/auth.json
//   - authfile from stack.yaml overlaid last (highest precedence, overrides auto-detected)
//
// Returns "" if no credentials are found (public image or not logged in).
// A non-empty authFile that cannot be read or parsed is an error. Missing
// auto-detected files are ignored.
func registryAuth(authFile, img string) (string, error) {
	authCfg, err := resolveAuth(authFile, img)
	if err != nil {
		return "", err
	}
	if authCfg.Username == "" && authCfg.Password == "" {
		return "", nil
	}
	encoded, err := json.Marshal(authCfg)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(encoded), nil
}

// credentialsFor returns username and password for the given image by searching
// the same credential sources as registryAuth.
func credentialsFor(authFile, img string) (username, password string, err error) {
	authCfg, err := resolveAuth(authFile, img)
	if err != nil {
		return "", "", err
	}
	return authCfg.Username, authCfg.Password, nil
}

// resolveAuth is the shared credential resolution used by registryAuth and credentialsFor.
func resolveAuth(authFile, img string) (registrytypes.AuthConfig, error) {
	ref, err := reference.ParseNormalizedNamed(img)
	if err != nil {
		return registrytypes.AuthConfig{}, nil
	}
	serverAddr := reference.Domain(ref)
	canonicalAddr := serverAddr
	if serverAddr == "docker.io" {
		canonicalAddr = "https://index.docker.io/v1/"
	}

	merged := &dockerConfig{
		Auths:       make(map[string]authEntry),
		CredHelpers: make(map[string]string),
	}

	// Merge all auto-detected files; first-wins among them.
	for _, path := range credFileCandidates() {
		if cfg, err := loadAuthFile(path); err == nil {
			mergeConfig(merged, cfg, false)
		}
	}

	// authfile from stack.yaml overrides anything auto-detected.
	// Load already resolved a relative path against the stack directory.
	if authFile != "" {
		cfg, err := loadAuthFile(authFile)
		if err != nil {
			return registrytypes.AuthConfig{}, fmt.Errorf("auth file %s: %w", authFile, err)
		}
		mergeConfig(merged, cfg, true)
	}

	authCfg, err := resolveCredentials(merged, serverAddr, canonicalAddr)
	if err != nil && authFile != "" {
		// Auto-detected files still fall back to an anonymous pull. An
		// explicit authfile must surface a bad blob or credential helper.
		return registrytypes.AuthConfig{}, err
	}
	if err != nil {
		return registrytypes.AuthConfig{}, nil
	}
	return authCfg, nil
}

// mergeConfig copies entries from src into dst.
// If override is false, existing dst entries are kept (first-wins).
// If override is true, src entries replace dst entries (last-wins / higher precedence).
func mergeConfig(dst, src *dockerConfig, override bool) {
	for k, v := range src.Auths {
		if override || dst.Auths[k].Auth == "" {
			dst.Auths[k] = v
		}
	}
	for k, v := range src.CredHelpers {
		if override || dst.CredHelpers[k] == "" {
			dst.CredHelpers[k] = v
		}
	}
	if src.CredsStore != "" && (override || dst.CredsStore == "") {
		dst.CredsStore = src.CredsStore
	}
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
