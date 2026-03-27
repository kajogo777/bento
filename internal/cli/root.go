package cli

import (
	"github.com/spf13/cobra"
)

// Global flags
var (
	flagVerbose bool
	flagDir     string
)

// NewRootCmd creates the root bento command.
func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "bento",
		Short: "Portable agent workspaces. Pack, ship, resume.",
		Long:  "Bento packages AI agent workspace state into portable, layered OCI artifacts.",
		Version: version,
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&flagDir, "dir", ".", "workspace directory")

	rootCmd.AddCommand(
		newInitCmd(),
		newSaveCmd(),
		newOpenCmd(),
		newListCmd(),
		newInspectCmd(),
		newDiffCmd(),
		newForkCmd(),
		newTagCmd(),
		newPushCmd(),
		newGCCmd(),
		newEnvCmd(),
	)

	return rootCmd
}
