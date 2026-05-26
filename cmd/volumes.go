package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var (
	flagVolumesUnused bool
	flagVolumesSize   bool
)

func init() {
	cmd := &cobra.Command{
		Use:   "volumes",
		Short: "List local container volumes",
		RunE:  runVolumes,
	}
	cmd.Flags().BoolVar(&flagVolumesUnused, "unused", false, "show only volumes not mounted by any container")
	cmd.Flags().BoolVar(&flagVolumesSize, "size", false, "show disk usage — triggers a daemon-side scan and may be slow")
	rootCmd.AddCommand(cmd)
}

func runVolumes(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	r, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := pingRuntime(ctx, r); err != nil {
		return err
	}

	f := rt.Filters{}
	if flagVolumesUnused {
		t := true
		f.Dangling = &t
	}

	vols, err := r.ListVolumes(ctx, f)
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	sort.Slice(vols, func(i, j int) bool {
		return vols[i].Name < vols[j].Name
	})

	// Optionally fetch volume sizes from the daemon.
	if flagVolumesSize {
		sizes, err := r.VolumeSizes(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not fetch volume sizes: %v\n", err)
		} else {
			for i := range vols {
				if sz, ok := sizes[vols[i].Name]; ok {
					vols[i].Size = &sz
				}
			}
		}
	}

	// Build volume → container info map.
	ctrs, _ := r.ListContainers(ctx, rt.Filters{})
	type mountRef struct {
		name        string
		state       string
		source      string
		destination string
		readOnly    bool
	}
	volCtrMap := make(map[string][]string)
	volMountMap := make(map[string][]mountRef)
	for _, c := range ctrs {
		for _, m := range c.Mounts {
			if m.Type != "volume" || m.Name == "" {
				continue
			}
			volCtrMap[m.Name] = append(volCtrMap[m.Name], c.Name)
			volMountMap[m.Name] = append(volMountMap[m.Name], mountRef{
				name:        c.Name,
				state:       c.State,
				source:      m.Source,
				destination: m.Destination,
				readOnly:    m.ReadOnly,
			})
		}
	}

	switch flagOutput {
	case "json", "yaml":
		type containerRef struct {
			Name        string `json:"name"`
			State       string `json:"state"`
			Source      string `json:"source"`
			Destination string `json:"destination"`
			ReadOnly    bool   `json:"read_only,omitempty"`
		}
		type volumeOut struct {
			rt.VolumeInfo `json:",inline" yaml:",inline"`
			Containers    []containerRef `json:"containers,omitempty" yaml:"containers,omitempty"`
		}
		out := make([]volumeOut, 0, len(vols))
		for _, v := range vols {
			var refs []containerRef
			for _, mr := range volMountMap[v.Name] {
				refs = append(refs, containerRef{
					Name:        mr.name,
					State:       mr.state,
					Source:      mr.source,
					Destination: mr.destination,
					ReadOnly:    mr.readOnly,
				})
			}
			out = append(out, volumeOut{VolumeInfo: v, Containers: refs})
		}
		if flagOutput == "yaml" {
			enc := yaml.NewEncoder(os.Stdout)
			enc.SetIndent(2)
			return enc.Encode(out)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	default:
		if len(vols) == 0 {
			fmt.Println("No volumes found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if flagVolumesSize {
			fmt.Fprintln(w, "NAME\tDRIVER\tSIZE\tCONTAINERS")
		} else {
			fmt.Fprintln(w, "NAME\tDRIVER\tCONTAINERS")
		}
		for _, v := range vols {
			ctrsStr := "-"
			if names := volCtrMap[v.Name]; len(names) > 0 {
				ctrsStr = strings.Join(names, ", ")
			}
			if flagVolumesSize {
				sizeStr := "n/a"
				if v.Size != nil && *v.Size >= 0 {
					sizeStr = formatImageSize(*v.Size)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.Name, v.Driver, sizeStr, ctrsStr)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\n", v.Name, v.Driver, ctrsStr)
			}
		}
		return w.Flush()
	}
}
