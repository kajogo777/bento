package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/policy"
	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newGCCmd() *cobra.Command {
	var (
		flagKeepLast   int
		flagKeepTagged bool
	)

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Clean up old checkpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			keepLast := flagKeepLast
			if keepLast == 0 {
				keepLast = cfg.Retention.KeepLast
			}
			if keepLast == 0 {
				keepLast = 10
			}

			keepTagged := flagKeepTagged || cfg.Retention.KeepTagged

			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			opts := policy.GCOptions{
				KeepLast:   keepLast,
				KeepTagged: keepTagged,
			}

			deleted, err := policy.GarbageCollect(store, opts)
			if err != nil {
				return fmt.Errorf("garbage collection: %w", err)
			}

			if len(deleted) == 0 {
				fmt.Println("Nothing to clean up.")
			} else {
				fmt.Printf("Deleted %d checkpoint(s).\n", len(deleted))
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&flagKeepLast, "keep-last", 0, "number of recent checkpoints to keep")
	cmd.Flags().BoolVar(&flagKeepTagged, "keep-tagged", false, "keep all tagged checkpoints")

	return cmd
}
