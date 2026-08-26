package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/upgrade"
)

var (
	flagUpgradeVersion string
	flagUpgradeCheck   bool
)

var selfUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Replace this binary with a newer GitHub release",
	Long: `Download a containerctl release from GitHub and replace the running binary.

With no flags, checks the latest stable release (or the rolling devel release
when this binary is already a devel build) and installs it if it is newer.

  containerctl upgrade
  containerctl upgrade --check
  containerctl upgrade --version=v1.2.4
  containerctl upgrade --version=devel

A tagged version is installed only when it is greater than the current version
(v1.2.3 will not install v1.2.2 or another v1.2.3). The rolling devel tag is
rebuilt on every push to main; upgrade compares the remote commit (from the
release body) to this binary's devel-<sha> and skips the download when they
match.

Set GITHUB_TOKEN to raise the unauthenticated GitHub API rate limit. Set
CONTAINERCTL_GITHUB_REPO=owner/name to use a fork. Not available in the web
terminal.`,
	Args: cobra.NoArgs,
	RunE: runSelfUpgrade,
}

func init() {
	selfUpgradeCmd.Flags().StringVar(&flagUpgradeVersion, "version", "", "GitHub release tag to install (e.g. v1.2.4 or devel)")
	selfUpgradeCmd.Flags().BoolVar(&flagUpgradeCheck, "check", false, "report whether an update is available without replacing the binary (exits 3 if yes)")
	rootCmd.AddCommand(selfUpgradeCmd)
}

func runSelfUpgrade(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	current := upgrade.ParseVersion(Version)
	client := upgrade.NewClient()

	remote, err := fetchTarget(ctx, client, current, flagUpgradeVersion)
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	digest, _ := upgrade.FileSHA256(exe)
	dec := upgrade.Decide(current, flagUpgradeVersion, remote, digest)

	fmt.Fprintf(os.Stdout, "current:  %s\n", versionLine(current))
	fmt.Fprintf(os.Stdout, "remote:   %s\n", remoteLine(remote))

	switch dec.Action {
	case upgrade.ActionNone:
		fmt.Fprintf(os.Stdout, "%s\n", dec.Reason)
		return nil
	case upgrade.ActionRefuse:
		return fmt.Errorf("%s", dec.Reason)
	}

	fmt.Fprintf(os.Stdout, "%s\n", dec.Reason)
	if flagUpgradeCheck {
		os.Exit(3)
	}

	asset, err := upgrade.FindAsset(remote, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "downloading %s (%s/%s)...\n", asset.Name, runtime.GOOS, runtime.GOARCH)
	inst := &upgrade.Installer{}
	if err := inst.Replace(ctx, exe, asset); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "replaced %s\n", exe)
	fmt.Fprintf(os.Stdout, "restart any long-lived process (for example `containerctl serve`) to load the new binary\n")
	return nil
}

func fetchTarget(ctx context.Context, client *upgrade.Client, current upgrade.Version, requested string) (*upgrade.Release, error) {
	tag := upgrade.NormalizeTag(requested)
	if tag == "" && current.Track == upgrade.TrackDevel {
		tag = "devel"
	}
	var (
		rel *upgrade.Release
		err error
	)
	if tag == "" {
		rel, err = client.Latest(ctx)
	} else {
		rel, err = client.ByTag(ctx, tag)
	}
	if err != nil {
		if tag == "" {
			return nil, fmt.Errorf("fetch latest release: %w", err)
		}
		return nil, fmt.Errorf("fetch release %s: %w", tag, err)
	}
	return rel, nil
}

func versionLine(v upgrade.Version) string {
	if v.Raw == "" {
		return "dev"
	}
	if BuildDate != "" && BuildDate != "unknown" {
		return fmt.Sprintf("%s  (built %s)", v.Raw, BuildDate)
	}
	return v.Raw
}

func remoteLine(r *upgrade.Release) string {
	if r == nil {
		return "-"
	}
	s := r.Tag
	if r.CommitSHA != "" {
		sha := r.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		s += "  commit " + sha
	}
	if !r.PublishedAt.IsZero() {
		s += "  " + r.PublishedAt.UTC().Format(time.RFC3339)
	}
	return s
}
