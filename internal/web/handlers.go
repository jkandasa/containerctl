package web

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.auth.validRequest(r) {
		http.Redirect(w, r, "/terminal", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveAsset(w, "login.html", "text/html; charset=utf-8")
	case http.MethodPost:
		ip := clientIP(r)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ok, blocked, retryAfter := s.auth.validateLogin(ip, r.FormValue("token"))
		if ok {
			id := s.auth.create()
			ttl := s.auth.TTL()
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    id,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   s.cfg.TLSMode != "" && s.cfg.TLSMode != "none",
				MaxAge:   int(ttl.Seconds()),
				Expires:  time.Now().Add(ttl),
			})
			http.Redirect(w, r, "/terminal", http.StatusFound)
			return
		}
		if blocked {
			secs := int(retryAfter.Seconds()) + 1
			http.Redirect(w, r, fmt.Sprintf("/login?error=blocked&sec=%d", secs), http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := s.auth.sessionID(r)
	if id != "" {
		s.auth.delete(id)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleTerminalPage(w http.ResponseWriter, r *http.Request) {
	serveAsset(w, "terminal.html", "text/html; charset=utf-8")
}

func (s *Server) handleStatusAPI(w http.ResponseWriter, r *http.Request) {
	statusArgs := []string{"status", "--output", "json", "--no-color"}
	args := append(s.buildGlobalFlags(s.cfg.StackFile, statusArgs), statusArgs...)
	cmd := exec.CommandContext(r.Context(), s.cfg.Executable, args...)
	out, err := cmd.Output()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		http.Error(w, `{"error":"failed to get status"}`, http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(out)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	// The browser editor is the only client of this endpoint, so it carries the
	// same gate as the "edit" command. Without it, any authenticated session
	// could read or overwrite any file the server process can reach.
	if !s.cfg.EditEnabled {
		http.Error(w, `{"error":"edit is disabled; set serve.edit.enabled: true in your stack.yaml and restart"}`, http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleFileRead(w, r)
	case http.MethodPut:
		s.handleFileWrite(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	abs, err := resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}
	sum := sha256.Sum256(data)
	etag := fmt.Sprintf(`"%x"`, sum[:])
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	abs, err := resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	// Only allow overwriting existing files; never create new ones.
	if _, err := os.Stat(abs); err != nil {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}

	// Optimistic concurrency: if the client sent If-Match, verify the file
	// hasn't changed since the client last read it.
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
		current, err := os.ReadFile(abs)
		if err != nil {
			http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
			return
		}
		sum := sha256.Sum256(current)
		currentETag := fmt.Sprintf(`"%x"`, sum[:])
		if ifMatch != currentETag {
			w.Header().Set("ETag", currentETag)
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("conflict: file was modified by another client"))
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, `{"error":"read error"}`, http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(abs, body, 0644); err != nil {
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serveAsset(w http.ResponseWriter, name, contentType string) {
	b, err := fs.ReadFile(staticFS, name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}

// resolveFilePath validates and returns the absolute path from a raw path
// string. Returns an error if the path is empty or not already absolute.
func resolveFilePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("path required")
	}
	abs, err := filepath.Abs(raw)
	if err != nil || abs != raw {
		return "", fmt.Errorf("path must be absolute")
	}
	if !isStackFilePath(abs) {
		return "", fmt.Errorf("path must be a .yaml or .yml file")
	}
	return abs, nil
}

// isStackFilePath restricts the editor to stack files. "use" accepts any
// absolute path, so the target cannot be pinned to a single directory — but
// keeping it to YAML puts shell profiles, authorized_keys and unit files out of
// reach of a hijacked session.
func isStackFilePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	}
	return false
}

// clientIP extracts the originating IP from the request, honouring
// X-Forwarded-For for deployments behind a reverse proxy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
