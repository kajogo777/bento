package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	var flagTag string

	cmd := &cobra.Command{
		Use:   "pull [remote]",
		Short: "Pull checkpoints from a remote registry",
		Long: `Sync checkpoints from a remote OCI registry to the local store.
Does not restore any files — use 'bento open' after pulling.

Uses the configured remote from bento.yaml by default, or pass
a registry URL to pull from a different remote.

Examples:
  bento pull                              # pull all tags from configured remote
  bento pull --tag cp-3                   # pull a specific tag
  bento pull ghcr.io/myorg/project        # pull from explicit remote`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			remote := cfg.Remote
			if len(args) > 0 {
				remote = args[0]
			}
			if remote == "" {
				return fmt.Errorf("no remote configured. Set 'remote' in bento.yaml or pass a registry URL")
			}

			// Persist remote to bento.yaml when passed as a CLI argument.
			if len(args) > 0 {
				if updated, updateErr := config.UpdateRemote(dir, remote); updateErr != nil {
					fmt.Printf("Warning: saving remote to bento.yaml: %v\n", updateErr)
				} else if updated {
					fmt.Printf("Remote: %s\n", remote)
				}
			}

			// Derive store name from remote (last path segment).
			parts := strings.Split(remote, "/")
			storeName := parts[len(parts)-1]

			storePath := filepath.Join(cfg.Store, storeName)
			// If the workspace ID matches the store name pattern, use the config store path.
			if cfg.ID != "" {
				storePath = cfg.StorePath()
			}

			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			localStore := store.(*registry.LocalStore)

			ctx := context.Background()

			if flagTag != "" {
				fmt.Printf("Pulling %s from %s...\n", flagTag, remote)
				if err := registry.PullFromRemote(ctx, localStore, remote, flagTag); err != nil {
					return fmt.Errorf("pull failed: %w", err)
				}
				fmt.Printf("  pulled %s\n", flagTag)
			} else {
				fmt.Printf("Pulling from %s...\n", remote)
				pulled, err := registry.PullAllFromRemote(ctx, localStore, remote)
				if err != nil {
					return fmt.Errorf("pull failed: %w", err)
				}
				if len(pulled) == 0 {
					fmt.Println("No checkpoints found on remote.")
					return nil
				}
				for _, tag := range pulled {
					fmt.Printf("  pulled %s\n", tag)
				}
			}

			fmt.Println("Done.")
			fmt.Println("\nUse `bento list` to see checkpoints, `bento open <ref>` to restore.")

			return nil
		},
	}

	cmd.Flags().StringVar(&flagTag, "tag", "", "pull only this tag (default: pull all)")

	return cmd
}
