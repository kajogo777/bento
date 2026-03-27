package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/manifest"
	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [ref]",
		Short: "Show checkpoint metadata and layers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			ref := "latest"
			if len(args) > 0 {
				ref = args[0]
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			storeName, tag, err := registry.ParseRef(ref)
			if err != nil {
				return err
			}
			if storeName == "" {
				storeName = filepath.Base(dir)
			}

			storePath := filepath.Join(cfg.Store, storeName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			manifestBytes, configBytes, _, err := store.LoadCheckpoint(tag)
			if err != nil {
				return fmt.Errorf("loading checkpoint %s: %w", ref, err)
			}

			// Parse and display checkpoint info
			info, err := manifest.ParseCheckpointInfo(manifestBytes)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			fmt.Printf("Checkpoint: %s (sequence %d)\n", tag, info.Sequence)
			fmt.Printf("Digest:     %s\n", info.Digest)
			if info.Parent != "" {
				fmt.Printf("Parent:     %s\n", info.Parent)
			}
			fmt.Printf("Created:    %s\n", info.Created)
			if info.Agent != "" {
				fmt.Printf("Agent:      %s\n", info.Agent)
			}
			if info.Message != "" {
				fmt.Printf("Message:    %s\n", info.Message)
			}

			// Display config object
			if len(configBytes) > 0 {
				var cfgObj manifest.BentoConfigObj
				if err := json.Unmarshal(configBytes, &cfgObj); err == nil {
					fmt.Println("\nConfig:")
					if cfgObj.Task != "" {
						fmt.Printf("  Task:      %s\n", cfgObj.Task)
					}
					if cfgObj.Harness != "" {
						fmt.Printf("  Harness:   %s\n", cfgObj.Harness)
					}
					if cfgObj.GitBranch != "" {
						fmt.Printf("  Git:       %s (%s)\n", cfgObj.GitBranch, cfgObj.GitSha)
					}
					if cfgObj.Environment != nil {
						fmt.Printf("  Platform:  %s/%s\n", cfgObj.Environment.OS, cfgObj.Environment.Arch)
					}
				}
			}

			return nil
		},
	}

	return cmd
}
