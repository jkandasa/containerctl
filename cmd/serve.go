package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/web"
)

var (
	flagServeAddress    string
	flagServeToken      string
	flagServeTLS        string
	flagServeTLSDomain  string
	flagServeTLSCert    string
	flagServeTLSKey     string
	flagServeTLSCache   string
	flagServeSessionTTL time.Duration
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web terminal for browser-based management",
	Long: `Start an HTTP/HTTPS server exposing a browser terminal.

After authenticating with the configured token, users land in a
terminal where they can run the same containerctl commands as the CLI.

Authentication token is read from --token or CONTAINERCTL_TOKEN env var.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&flagServeAddress, "address", ":8080", "address to listen on (e.g. :8080 or 127.0.0.1:9090)")
	serveCmd.Flags().StringVar(&flagServeToken, "token", "", "auth token (or set CONTAINERCTL_TOKEN env var)")
	serveCmd.Flags().StringVar(&flagServeTLS, "tls", "none", "TLS mode: none | self-signed | letsencrypt | custom")
	serveCmd.Flags().StringVar(&flagServeTLSDomain, "tls-domain", "", "public domain for Let's Encrypt")
	serveCmd.Flags().StringVar(&flagServeTLSCert, "tls-cert", "", "TLS certificate file (custom mode)")
	serveCmd.Flags().StringVar(&flagServeTLSKey, "tls-key", "", "TLS key file (custom mode)")
	serveCmd.Flags().StringVar(&flagServeTLSCache, "tls-cache-dir", defaultTLSCacheDir(), "Let's Encrypt cert cache directory")
	serveCmd.Flags().DurationVar(&flagServeSessionTTL, "session-ttl", 24*time.Hour, "session validity duration")
}

func runServe(_ *cobra.Command, _ []string) error {
	token := flagServeToken
	if token == "" {
		token = os.Getenv("CONTAINERCTL_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("auth token is required; set --token or CONTAINERCTL_TOKEN env var")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	// Dereference symlinks so the subprocess resolves correctly.
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable symlink: %w", err)
	}

	absFile, err := filepath.Abs(flagFile)
	if err != nil {
		return fmt.Errorf("resolve stack file path: %w", err)
	}

	// Read serve-specific settings from the stack file.
	// Fail-safe: if the stack can't be parsed, all opt-in features stay disabled.
	var execEnabled, editEnabled, useEnabled bool
	var execAllowed []string
	if stack, err := config.Load(absFile); err == nil {
		execEnabled = stack.Serve.Exec.Enabled
		execAllowed = stack.Serve.Exec.Allowed
		editEnabled = stack.Serve.Edit.Enabled
		useEnabled = stack.Serve.Use.Enabled
	}

	srv := web.New(web.Config{
		Listen:      flagServeAddress,
		Token:       token,
		TLSMode:     flagServeTLS,
		TLSDomain:   flagServeTLSDomain,
		TLSCert:     flagServeTLSCert,
		TLSKey:      flagServeTLSKey,
		TLSCacheDir: flagServeTLSCache,
		SessionTTL:  flagServeSessionTTL,

		Executable:  executable,
		StackFile:   absFile,
		Project:     flagProject,
		RuntimeName: flagRuntime,
		Socket:      flagSocket,
		ExecEnabled: execEnabled,
		ExecAllowed: execAllowed,
		EditEnabled: editEnabled,
		UseEnabled:  useEnabled,
	})

	return srv.Start()
}

func defaultTLSCacheDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "containerctl", "certs")
}
