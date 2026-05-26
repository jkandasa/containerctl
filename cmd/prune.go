package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var (
	flagPruneImages   bool
	flagPruneVolumes  bool
	flagPruneNetworks bool
	flagPruneAll      bool
	flagPruneDryRun   bool
	flagPruneForce    bool
)

func init() {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove unused host-wide images, volumes, and/or networks",
		Long: `Remove unused local resources from the host (not project-scoped).

Flags select which resource types to prune:
  --images    images not used by any container or stack declaration
  --volumes   volumes not mounted by any container
  --networks  user-defined networks not connected to any container
  --all       equivalent to --images --volumes --networks

Use --dry-run to preview without removing. Use --force to skip confirmation.`,
		RunE: runPrune,
	}
	cmd.Flags().BoolVar(&flagPruneImages, "images", false, "remove unused images")
	cmd.Flags().BoolVar(&flagPruneVolumes, "volumes", false, "remove unused volumes")
	cmd.Flags().BoolVar(&flagPruneNetworks, "networks", false, "remove unused networks")
	cmd.Flags().BoolVar(&flagPruneAll, "all", false, "remove all unused resources (images, volumes, networks)")
	cmd.Flags().BoolVar(&flagPruneDryRun, "dry-run", false, "show what would be removed without removing")
	cmd.Flags().BoolVar(&flagPruneForce, "force", false, "skip confirmation prompt")
	rootCmd.AddCommand(cmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	doImages := flagPruneImages || flagPruneAll
	doVolumes := flagPruneVolumes || flagPruneAll
	doNetworks := flagPruneNetworks || flagPruneAll

	if !doImages && !doVolumes && !doNetworks {
		return fmt.Errorf("specify at least one of --images, --volumes, --networks, or --all")
	}

	ctx := context.Background()

	stack, _ := config.Load(flagFile)

	r, err := runtimeFromOptional(stack)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := pingRuntime(ctx, r); err != nil {
		return err
	}

	var toRemoveImages []rt.ImageInfo
	var toRemoveVolumes []rt.VolumeInfo
	var toRemoveNetworks []rt.NetworkInfo

	allCtrs, err := r.ListContainers(ctx, rt.Filters{})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	if doImages {
		imgs, err := r.ListImages(ctx)
		if err != nil {
			return fmt.Errorf("list images: %w", err)
		}
		toRemoveImages = unusedImages(allCtrs, stack, imgs)
	}

	if doVolumes {
		t := true
		vols, err := r.ListVolumes(ctx, rt.Filters{Dangling: &t})
		if err != nil {
			return fmt.Errorf("list volumes: %w", err)
		}
		toRemoveVolumes = vols
	}

	if doNetworks {
		t := true
		nets, err := r.ListNetworks(ctx, rt.Filters{Dangling: &t})
		if err != nil {
			return fmt.Errorf("list networks: %w", err)
		}
		for _, n := range nets {
			if !systemNetworks[n.Name] {
				toRemoveNetworks = append(toRemoveNetworks, n)
			}
		}
	}

	total := len(toRemoveImages) + len(toRemoveVolumes) + len(toRemoveNetworks)
	if total == 0 {
		fmt.Println("Nothing to prune.")
		return nil
	}

	if len(toRemoveImages) > 0 {
		fmt.Printf("Images (%d):\n", len(toRemoveImages))
		for _, img := range toRemoveImages {
			tag := "<none>"
			if len(img.Tags) > 0 {
				tag = strings.Join(img.Tags, ", ")
			}
			fmt.Printf("  %s  %s  %s\n", img.ID, tag, formatImageSize(img.Size))
		}
	}
	if len(toRemoveVolumes) > 0 {
		fmt.Printf("Volumes (%d):\n", len(toRemoveVolumes))
		for _, v := range toRemoveVolumes {
			fmt.Printf("  %s\n", v.Name)
		}
	}
	if len(toRemoveNetworks) > 0 {
		fmt.Printf("Networks (%d):\n", len(toRemoveNetworks))
		for _, n := range toRemoveNetworks {
			fmt.Printf("  %s  (%s)\n", n.Name, n.Driver)
		}
	}

	if flagPruneDryRun {
		fmt.Println("\nDry run — nothing removed.")
		return nil
	}

	if !flagPruneForce {
		if !stdinIsTerminal() {
			return fmt.Errorf("stdin is not a terminal; use --force to skip confirmation")
		}
		fmt.Printf("\nRemove %d resource(s)? [y/N] ", total)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var errCount int
	for _, img := range toRemoveImages {
		id := img.ID
		if len(img.Tags) > 0 {
			id = img.Tags[0]
		}
		if err := r.RemoveImage(ctx, id, false); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing image %s: %v\n", id, err)
			errCount++
		} else {
			fmt.Printf("  removed image    %s\n", id)
		}
	}
	for _, v := range toRemoveVolumes {
		if err := r.RemoveVolume(ctx, v.Name, false); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing volume %s: %v\n", v.Name, err)
			errCount++
		} else {
			fmt.Printf("  removed volume   %s\n", v.Name)
		}
	}
	for _, n := range toRemoveNetworks {
		if err := r.RemoveNetwork(ctx, n.Name); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing network %s: %v\n", n.Name, err)
			errCount++
		} else {
			fmt.Printf("  removed network  %s\n", n.Name)
		}
	}

	if errCount > 0 {
		return fmt.Errorf("%d removal(s) failed", errCount)
	}
	return nil
}
