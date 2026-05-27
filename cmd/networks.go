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

var flagNetworksUnused bool

var systemNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func init() {
	cmd := &cobra.Command{
		Use:   "networks",
		Short: "List container networks",
		RunE:  runNetworks,
	}
	cmd.Flags().BoolVar(&flagNetworksUnused, "unused", false, "show only networks not connected to any container")
	rootCmd.AddCommand(cmd)
}

func runNetworks(cmd *cobra.Command, args []string) error {
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

	f := rt.Filters{}
	if flagNetworksUnused {
		t := true
		f.Dangling = &t
	}

	nets, err := r.ListNetworks(ctx, f)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}

	// Exclude system networks.
	filtered := nets[:0]
	for _, n := range nets {
		if !systemNetworks[n.Name] {
			filtered = append(filtered, n)
		}
	}
	nets = filtered

	sort.Slice(nets, func(i, j int) bool {
		return nets[i].Name < nets[j].Name
	})

	// Build network → container info map.
	ctrs, _ := r.ListContainers(ctx, rt.Filters{})
	type netRef struct {
		name      string
		state     string
		ipAddress string
		gateway   string
	}
	netCtrMap := make(map[string][]string)
	netInfoMap := make(map[string][]netRef)
	for _, c := range ctrs {
		for _, ni := range c.NetworkInfos {
			netCtrMap[ni.Name] = append(netCtrMap[ni.Name], c.Name)
			netInfoMap[ni.Name] = append(netInfoMap[ni.Name], netRef{
				name:      c.Name,
				state:     c.State,
				ipAddress: ni.IPAddress,
				gateway:   ni.Gateway,
			})
		}
	}

	switch flagOutput {
	case "json", "yaml":
		type containerRef struct {
			Name      string `json:"name"`
			State     string `json:"state"`
			IPAddress string `json:"ip_address,omitempty"`
			Gateway   string `json:"gateway,omitempty"`
		}
		type networkOut struct {
			rt.NetworkInfo `json:",inline" yaml:",inline"`
			Containers     []containerRef `json:"containers,omitempty" yaml:"containers,omitempty"`
		}
		out := make([]networkOut, 0, len(nets))
		for _, n := range nets {
			var refs []containerRef
			for _, nr := range netInfoMap[n.Name] {
				refs = append(refs, containerRef{
					Name:      nr.name,
					State:     nr.state,
					IPAddress: nr.ipAddress,
					Gateway:   nr.gateway,
				})
			}
			out = append(out, networkOut{NetworkInfo: n, Containers: refs})
		}
		cols := colors()
		if flagOutput == "yaml" {
			return render.YAML(os.Stdout, out, cols)
		}
		return render.JSON(os.Stdout, out, cols)
	default:
		if len(nets) == 0 {
			fmt.Println("No networks found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDRIVER\tMANAGED\tCONTAINERS")
		for _, n := range nets {
			managed := "no"
			if n.Labels["containerctl.managed"] == "true" {
				managed = "yes"
			}
			ctrsStr := "-"
			if names := netCtrMap[n.Name]; len(names) > 0 {
				ctrsStr = strings.Join(names, ", ")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.Name, n.Driver, managed, ctrsStr)
		}
		return w.Flush()
	}
}
