package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [remote]",
		Short: "Push local checkpoints to registry",
		Args:  cobra.MaximumNArgs(1),
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

			// TODO: implement remote push via oras-go
			fmt.Printf("Pushing to %s...\n", remote)
			fmt.Println("Remote push is not yet implemented. Checkpoints are saved locally.")

			return nil
		},
	}

	return cmd
}
