package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var flagTag string

	cmd := &cobra.Command{
		Use:   "push [remote]",
		Short: "Push checkpoints to a remote registry",
		Long: `Push local checkpoints to an OCI registry. Uses Docker credential
helpers for authentication (docker login, crane auth, etc).

Examples:
  bento push ghcr.io/myorg/workspaces/myproject
  bento push --tag cp-3
  bento push                    # uses remote from bento.yaml`,
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

			// Open local store
			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}

			// Determine which tags to push
			var tags []string
			if flagTag != "" {
				tags = []string{flagTag}
			}

			remoteRef := remote + "/" + projectName
			fmt.Printf("Pushing to %s...\n", remoteRef)

			ctx := context.Background()
			if err := registry.PushToRemote(ctx, store, remoteRef, tags); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}

			fmt.Println("Done.")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagTag, "tag", "", "push only this tag (default: push all)")

	return cmd
}
