package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
)

var flagStackUnset bool

var stackCmd = &cobra.Command{
	Use:   "stack [file]",
	Short: "Set or show the default stack file",
	Long: `Set the default stack YAML used by subsequent containerctl commands
(similar to "oc project").

  containerctl stack /path/to/stack1.yaml   # set default (stored as an absolute path)
  containerctl stack                        # print the current default
  containerctl stack --unset                # clear the saved default (back to stack.yaml)

An explicit -f/--file on any command always overrides the saved default.
The path is stored in $XDG_CONFIG_HOME/containerctl/config.json
(or ~/.config/containerctl/config.json).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStack,
}

func init() {
	stackCmd.Flags().BoolVar(&flagStackUnset, "unset", false, "clear the saved default stack file")
	rootCmd.AddCommand(stackCmd)
}

func runStack(_ *cobra.Command, args []string) error {
	if flagStackUnset {
		if len(args) > 0 {
			return fmt.Errorf("stack --unset does not take a file path")
		}
		if err := config.ClearCurrentStackPath(); err != nil {
			return err
		}
		fmt.Printf("Cleared default stack file (now %s)\n", config.DefaultStackFile)
		return nil
	}

	if len(args) == 1 {
		abs, err := config.SetCurrentStackPath(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Now using stack file %q\n", abs)
		return nil
	}

	// Print current default: saved path if set, else built-in default.
	saved, err := config.CurrentStackPath()
	if err != nil {
		return err
	}
	if saved != "" {
		fmt.Printf("%s\n", saved)
		// Warn if the saved file has disappeared.
		if _, err := os.Stat(saved); err != nil {
			fmt.Fprintf(os.Stderr, "warning: file not found: %s\n", saved)
		}
		return nil
	}
	fmt.Printf("%s\n", config.DefaultStackFile)
	return nil
}
