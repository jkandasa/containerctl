package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/reconcile"
	"github.com/jkandasa/containerctl/internal/registry"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var flagCheckUpdateApply bool

var checkUpdateCmd = &cobra.Command{
	Use:   "check-update [name...]",
	Short: "Check registry for updates; --apply applies patch/minor updates and digest changes",
	RunE:  runCheckUpdate,
}

func init() {
	rootCmd.AddCommand(checkUpdateCmd)
	checkUpdateCmd.Flags().BoolVar(&flagCheckUpdateApply, "apply", false, "pull and recreate containers with patch/minor updates or digest changes")
}

type imageUpdateStatus struct {
	name     string
	image    string
	// status: "up-to-date" | "patch update" | "major update" | "patch+major" |
	//         "digest changed" | "not pulled" | "error"
	status   string
	note     string
	newerTag string // for --apply: the target tag (empty means re-pull same tag)
}

func runCheckUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	runtime, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer runtime.Close()

	if err := pingRuntime(ctx, runtime); err != nil {
		return err
	}
	applyAuthFile(runtime, stack.AuthFile)

	st, err := state.Load(stack.Project)
	if err != nil {
		return err
	}

	filterSet := make(map[string]bool, len(args))
	for _, a := range args {
		filterSet[a] = true
	}

	var containers []config.Container
	for _, c := range stack.Containers {
		if c.Disabled {
			continue
		}
		if len(filterSet) > 0 && !filterSet[c.Name] {
			continue
		}
		containers = append(containers, c)
	}

	if len(containers) == 0 {
		fmt.Println("No containers to check.")
		return nil
	}

	results := make([]imageUpdateStatus, len(containers))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for i, c := range containers {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = checkImage(ctx, runtime, c)
		}()
	}
	wg.Wait()

	nameW, imageW, statusW := len("NAME"), len("IMAGE"), len("STATUS")
	for _, r := range results {
		if len(r.name) > nameW {
			nameW = len(r.name)
		}
		if len(r.image) > imageW {
			imageW = len(r.image)
		}
		if len(r.status) > statusW {
			statusW = len(r.status)
		}
	}
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %s", nameW, "NAME", imageW, "IMAGE", statusW, "STATUS", "NOTE")
	fmt.Fprintln(os.Stdout, header)
	fmt.Fprintln(os.Stdout, strings.Repeat("-", len(header)))
	for _, r := range results {
		fmt.Fprintf(os.Stdout, "%-*s  %-*s  %-*s  %s\n", nameW, r.name, imageW, r.image, statusW, r.status, r.note)
	}

	if !flagCheckUpdateApply {
		return nil
	}

	var toApply []imageUpdateStatus
	for _, r := range results {
		switch r.status {
		case "digest changed", "patch update", "patch+major":
			toApply = append(toApply, r)
		}
	}

	if len(toApply) == 0 {
		fmt.Println("\nNothing to apply. Major version updates require manual tag changes in stack.yaml.")
		return nil
	}

	fmt.Println()
	for _, r := range toApply {
		c := stack.ContainerByName(r.name)
		if c == nil || st.IsDisabled(r.name) {
			continue
		}

		targetImage := c.Image
		if r.newerTag != "" {
			targetImage = replaceImageTag(c.Image, r.newerTag)
			fmt.Printf("  %-20s %s → %s\n", r.name, c.Image, targetImage)
			if err := config.UpdateContainerImage(flagFile, r.name, targetImage); err != nil {
				fmt.Fprintf(os.Stderr, "  %-20s config update error: %v\n", r.name, err)
				continue
			}
			c.Image = targetImage
		} else {
			fmt.Printf("  %-20s pulling...\n", r.name)
		}

		if err := runtime.Pull(ctx, targetImage); err != nil {
			fmt.Fprintf(os.Stderr, "  %-20s pull error: %v\n", r.name, err)
			continue
		}

		fullName := config.ContainerFullName(stack.Project, r.name)
		existing, err := runtime.InspectContainer(ctx, fullName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-20s inspect error: %v\n", r.name, err)
			continue
		}
		if existing != nil {
			if existing.Labels[rt.LabelManaged] != "true" {
				fmt.Fprintf(os.Stderr, "  %-20s skipped: not managed by containerctl\n", r.name)
				continue
			}
			if err := runtime.StopContainer(ctx, existing.ID, 10*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "  %-20s stop error: %v\n", r.name, err)
				continue
			}
			if err := runtime.RemoveContainer(ctx, existing.ID, false); err != nil {
				fmt.Fprintf(os.Stderr, "  %-20s remove error: %v\n", r.name, err)
				continue
			}
		}

		spec, err := reconcile.ContainerSpecFrom(stack.Project, c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-20s spec error: %v\n", r.name, err)
			continue
		}
		id, err := runtime.CreateContainer(ctx, spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-20s create error: %v\n", r.name, err)
			continue
		}
		if err := runtime.StartContainer(ctx, id); err != nil {
			fmt.Fprintf(os.Stderr, "  %-20s start error: %v\n", r.name, err)
			continue
		}
		fmt.Printf("  %-20s updated → running\n", r.name)
	}
	return nil
}

func checkImage(ctx context.Context, runtime rt.Runtime, c config.Container) imageUpdateStatus {
	s := imageUpdateStatus{name: c.Name, image: c.Image}

	if c.UpdatePolicy == "manual" {
		s.status = "manual"
		return s
	}

	if !isVersionTag(c.Image) {
		// floating tag (latest, master, …): digest comparison is the only meaningful signal
		localMeta, err := runtime.LocalImageMeta(ctx, c.Image)
		local := localMeta.Digest
		if err != nil {
			s.status = "error"
			s.note = err.Error()
			return s
		}
		if local == "" {
			s.status = "not pulled"
			return s
		}
		remote, err := runtime.RemoteImageDigest(ctx, c.Image)
		if err != nil {
			s.status = "error"
			s.note = err.Error()
			return s
		}
		if local != remote {
			s.status = "digest changed"
			s.note = fmt.Sprintf("%s → %s", shortDigest(local), shortDigest(remote))
		} else {
			s.status = "up-to-date"
		}
		return s
	}

	// semver tag: pure registry comparison — identical result for any runtime
	updates, err := registry.CheckTagUpdates(ctx, c.Image, 3)
	if err != nil {
		s.status = "error"
		s.note = err.Error()
		return s
	}

	hasPatch := len(updates.SameMajor) > 0
	hasMajor := len(updates.NewMajors) > 0

	if !hasPatch && !hasMajor {
		s.status = "up-to-date"
		return s
	}

	var noteParts []string
	if hasPatch {
		noteParts = append(noteParts, strings.Join(updates.SameMajor, ", "))
		s.newerTag = updates.SameMajor[len(updates.SameMajor)-1] // latest patch/minor
	}
	if hasMajor {
		noteParts = append(noteParts, "major: "+strings.Join(updates.NewMajors, ", "))
	}
	s.note = strings.Join(noteParts, "; ")

	switch {
	case hasPatch && hasMajor:
		s.status = "patch+major"
	case hasPatch:
		s.status = "patch update"
	default:
		s.status = "major update"
	}
	return s
}

// replaceImageTag swaps the tag component of a full image reference.
func replaceImageTag(image, newTag string) string {
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[:i+1] + newTag
	}
	return image + ":" + newTag
}

// isVersionTag returns true when the image tag looks like a pinned version
// (digit or 'v' + digit). Floating tags like "latest", "master", "edge" return false.
func isVersionTag(image string) bool {
	tag := "latest"
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		tag = image[i+1:]
	}
	s := strings.TrimPrefix(tag, "v")
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}

func shortDigest(d string) string {
	if after, ok := strings.CutPrefix(d, "sha256:"); ok && len(after) >= 12 {
		return "sha256:" + after[:12]
	}
	return d
}
