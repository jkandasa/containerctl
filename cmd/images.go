package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var flagImagesUnused bool

func init() {
	cmd := &cobra.Command{
		Use:   "images [name...]",
		Short: "List local container images",
		RunE:  runImages,
	}
	cmd.Flags().BoolVar(&flagImagesUnused, "unused", false, "show only images not referenced by any container or stack declaration")
	rootCmd.AddCommand(cmd)
}

func runImages(cmd *cobra.Command, args []string) error {
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

	imgs, err := r.ListImages(ctx)
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}

	sort.Slice(imgs, func(i, j int) bool {
		return imgs[i].Created.After(imgs[j].Created)
	})

	// Load all containers once to build the image→containers maps.
	ctrs, _ := r.ListContainers(ctx, rt.Filters{})
	imageCtrMap := buildImageContainerMap(ctrs)
	imageCtrRefMap := buildImageContainerRefMap(ctrs)

	if flagImagesUnused {
		imgs = unusedImages(ctrs, stack, imgs)
	}

	// Filter by name/tag substring when positional args are given.
	if len(args) > 0 {
		imgs = filterImagesByName(imgs, args)
	}

	return printImages(imgs, imageCtrMap, imageCtrRefMap)
}

type imageContainerRef struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// buildImageContainerMap returns a map of short image ID → container names using it.
func buildImageContainerMap(ctrs []rt.ContainerInfo) map[string][]string {
	m := make(map[string][]string)
	for _, c := range ctrs {
		id := strings.TrimPrefix(c.ImageID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		if id != "" {
			m[id] = append(m[id], c.Name)
		}
	}
	return m
}

// buildImageContainerRefMap returns a map of short image ID → container refs (name + state).
func buildImageContainerRefMap(ctrs []rt.ContainerInfo) map[string][]imageContainerRef {
	m := make(map[string][]imageContainerRef)
	for _, c := range ctrs {
		id := strings.TrimPrefix(c.ImageID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		if id != "" {
			m[id] = append(m[id], imageContainerRef{Name: c.Name, State: c.State})
		}
	}
	return m
}

// unusedImages filters to images not referenced by any container or stack declaration.
// Matching is done by image ID (reliable) with tag name as a secondary check, because
// Docker may return different name formats (short vs. fully-qualified) across API calls.
// stack may be nil when no stack file is available; in that case only running containers
// are considered.
func unusedImages(ctrs []rt.ContainerInfo, stack *config.Stack, imgs []rt.ImageInfo) []rt.ImageInfo {
	inUseIDs   := make(map[string]bool)
	inUseNames := make(map[string]bool)
	for _, c := range ctrs {
		inUseNames[c.Image] = true
		id := strings.TrimPrefix(c.ImageID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		if id != "" {
			inUseIDs[id] = true
		}
	}
	if stack != nil {
		for _, c := range stack.Containers {
			inUseNames[c.Image] = true
		}
	}

	out := imgs[:0]
	for _, img := range imgs {
		if inUseIDs[img.ID] {
			continue
		}
		used := false
		for _, tag := range img.Tags {
			if inUseNames[tag] {
				used = true
				break
			}
		}
		if !used {
			out = append(out, img)
		}
	}
	return out
}

// filterImagesByName returns images whose ID or any tag contains any of the given terms.
func filterImagesByName(imgs []rt.ImageInfo, terms []string) []rt.ImageInfo {
	out := imgs[:0]
	for _, img := range imgs {
		for _, term := range terms {
			if strings.Contains(img.ID, term) {
				out = append(out, img)
				break
			}
			matched := false
			for _, tag := range img.Tags {
				if strings.Contains(tag, term) {
					matched = true
					break
				}
			}
			if matched {
				out = append(out, img)
				break
			}
		}
	}
	return out
}

func printImages(imgs []rt.ImageInfo, imageCtrMap map[string][]string, imageCtrRefMap map[string][]imageContainerRef) error {
	switch flagOutput {
	case "json", "yaml":
		type imageOut struct {
			rt.ImageInfo `json:",inline" yaml:",inline"`
			Containers   []imageContainerRef `json:"containers,omitempty" yaml:"containers,omitempty"`
		}
		out := make([]imageOut, 0, len(imgs))
		for _, img := range imgs {
			out = append(out, imageOut{ImageInfo: img, Containers: imageCtrRefMap[img.ID]})
		}
		cols := colors()
		if flagOutput == "yaml" {
			return render.YAML(os.Stdout, out, cols)
		}
		return render.JSON(os.Stdout, out, cols)
	default:
		if len(imgs) == 0 {
			fmt.Println("No images found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "IMAGE ID\tTAGS\tSIZE\tCREATED\tCONTAINERS")
		for _, img := range imgs {
			tags := "<none>"
			if len(img.Tags) > 0 {
				tags = strings.Join(img.Tags, ", ")
			}
			ctrs := "-"
			if names := imageCtrMap[img.ID]; len(names) > 0 {
				ctrs = strings.Join(names, ", ")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				img.ID, tags, formatImageSize(img.Size), formatAge(img.Created), ctrs)
		}
		return w.Flush()
	}
}
