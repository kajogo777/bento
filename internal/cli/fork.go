package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/registry"
	"github.com/bentoci/bento/internal/workspace"
	"github.com/spf13/cobra"
)

func newForkCmd() *cobra.Command {
	var flagMessage string

	cmd := &cobra.Command{
		Use:   "fork <ref>",
		Short: "Branch from a checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			_, tag, err := registry.ParseRef(args[0])
			if err != nil {
				return err
			}

			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Load the fork point
			_, _, layers, err := store.LoadCheckpoint(tag)
			if err != nil {
				return fmt.Errorf("checkpoint '%s' not found. Run `bento list` to see available checkpoints", tag)
			}

			// Build set of files in the checkpoint for cleanup
			keepFiles := make(map[string]bool)
			for _, ld := range layers {
				files, listErr := workspace.ListLayerFiles(ld.Data)
				if listErr == nil {
					for _, f := range files {
						keepFiles[f] = true
					}
				}
			}

			// Restore to workspace
			fmt.Printf("Forking from %s...\n", tag)
			for _, ld := range layers {
				if err := workspace.UnpackLayer(ld.Data, dir); err != nil {
					return fmt.Errorf("unpacking layer: %w", err)
				}
			}

			// Remove stale files not in the fork point
			if len(keepFiles) > 0 {
				if err := workspace.CleanStaleFiles(dir, keepFiles); err != nil {
					fmt.Printf("Warning: cleaning stale files: %v\n", err)
				}
			}

			msg := flagMessage
			if msg == "" {
				msg = fmt.Sprintf("forked from %s", tag)
			}
			fmt.Printf("Workspace restored to %s. %s\n", tag, msg)

			return nil
		},
	}

	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "fork message")

	return cmd
}
