package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/policy"
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

			cfg, store, err := loadConfigAndStore(dir)
			if err != nil {
				return err
			}

			keepLast := flagKeepLast
			if keepLast == 0 {
				keepLast = cfg.Retention.KeepLast
			}
			if keepLast == 0 {
				keepLast = 10
			}

			keepTagged := flagKeepTagged || cfg.Retention.KeepTagged

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
