package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Config holds all settings for the web server.
type Config struct {
	Listen      string
	Token       string
	TLSMode     string // none | self-signed | letsencrypt | custom
	TLSDomain   string
	TLSCert     string
	TLSKey      string
	TLSCacheDir string
	SessionTTL  time.Duration

	// containerctl invocation
	Executable  string
	StackFile   string
	Project     string
	RuntimeName string
	Socket      string

	// ExecEnabled must be true for any exec command (interactive or not).
	ExecEnabled bool
	// ExecAllowed is an optional allowlist of container names. Empty means all
	// containers are permitted when ExecEnabled is true.
	ExecAllowed []string

	// EditEnabled must be true for the browser editor ("edit" command) to work.
	EditEnabled bool
	// UseEnabled must be true for the "use" stack-switch command to work.
	UseEnabled bool

	// NoColor indicates that the server was started with --no-color.
	// When true, subcommands executed from the web terminal will also
	// have --no-color injected (unless the user explicitly overrides it).
	NoColor bool
}

// Server is the containerctl web interface.
type Server struct {
	cfg  Config
	auth *sessionStore
}

// New creates a Server from the given config.
func New(cfg Config) *Server {
	return &Server{
		cfg:  cfg,
		auth: newSessionStore(cfg.Token, cfg.SessionTTL),
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.protected(s.handleLogout))
	mux.HandleFunc("/terminal", s.protected(s.handleTerminalPage))
	mux.HandleFunc("/ws/terminal", s.protected(s.handleTerminalWS))
	mux.HandleFunc("/ws/exec", s.protected(s.handleExecWS))
	mux.HandleFunc("/ws/logs", s.protected(s.handleLogsWS))
	mux.HandleFunc("/api/v1/status", s.protected(s.handleStatusAPI))
	mux.HandleFunc("/api/v1/file", s.protected(s.handleFile))

	return mux
}

func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.validRequest(r) {
			if r.Header.Get("Upgrade") == "websocket" || strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

// validateSession checks whether the session associated with the request is still valid.
// This is used for periodic re-validation on long-lived WebSocket connections.
func (s *Server) validateSession(r *http.Request) bool {
	return s.auth.validRequest(r)
}

// Start runs the server until interrupted.
func (s *Server) Start() error {
	srv := &http.Server{
		Addr:        s.cfg.Listen,
		Handler:     s.routes(),
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
		// WriteTimeout intentionally not set — streaming responses need no deadline.
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)

	switch s.cfg.TLSMode {
	case "", "none":
		fmt.Fprintln(os.Stderr, "WARNING: serving without TLS — token is sent in plaintext; use behind a TLS reverse proxy or set --tls")
		fmt.Fprintf(os.Stderr, "Listening on http://%s\n", s.cfg.Listen)
		go func() { errCh <- srv.ListenAndServe() }()

	case "self-signed":
		cert, err := generateSelfSigned()
		if err != nil {
			return fmt.Errorf("generate self-signed cert: %w", err)
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		fmt.Fprintf(os.Stderr, "Listening on https://%s (self-signed — browser will show a security warning)\n", s.cfg.Listen)
		go func() { errCh <- srv.ListenAndServeTLS("", "") }()

	case "letsencrypt":
		if s.cfg.TLSDomain == "" {
			return fmt.Errorf("--tls-domain is required for letsencrypt mode")
		}
		m := &autocert.Manager{
			Cache:      autocert.DirCache(s.cfg.TLSCacheDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.cfg.TLSDomain),
		}
		srv.TLSConfig = m.TLSConfig()
		go func() {
			if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
				fmt.Fprintf(os.Stderr, "WARN: HTTP-01 challenge listener on :80: %v\n", err)
			}
		}()
		fmt.Fprintf(os.Stderr, "Listening on https://%s (Let's Encrypt)\n", s.cfg.Listen)
		go func() { errCh <- srv.ListenAndServeTLS("", "") }()

	case "custom":
		if s.cfg.TLSCert == "" || s.cfg.TLSKey == "" {
			return fmt.Errorf("--tls-cert and --tls-key are required for custom TLS mode")
		}
		fmt.Fprintf(os.Stderr, "Listening on https://%s (custom cert)\n", s.cfg.Listen)
		go func() { errCh <- srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey) }()

	default:
		return fmt.Errorf("unknown TLS mode %q; use none, self-signed, letsencrypt, or custom", s.cfg.TLSMode)
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
