package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

// Global flags
var (
	flagVerbose bool
	flagDir     string
)

// loadConfigAndStore is a helper that loads bento.yaml from the workspace
// directory and opens the local OCI store for the project.
func loadConfigAndStore(dir string) (*config.BentoConfig, registry.Store, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("no bento.yaml found. Run `bento init` first")
	}
	projectName := filepath.Base(dir)
	storePath := filepath.Join(cfg.Store, projectName)
	store, err := registry.NewStore(storePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening store: %w", err)
	}
	return cfg, store, nil
}

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
