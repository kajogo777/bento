package cli

import (
	"fmt"
	"path/filepath"

	"github.com/kajogo777/bento/internal/policy"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newGCCmd() *cobra.Command {
	var (
		flagKeepLast   int
		flagKeepTagged bool
	)

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Clean up old checkpoints and orphaned blobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, store, err := loadConfigAndStore(dir)
			if err != nil {
				return err
			}

			// Resolve keep_last: flag overrides config (which already has
			// defaults from BackfillDefaults).
			keepLast := cfg.Retention.KeepLast
			if flagKeepLast > 0 {
				keepLast = flagKeepLast
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

			// Secrets are stored as OCI layers in the manifest, so they're
			// cleaned up automatically by blob GC below. No separate cleanup needed.

			if len(deleted) == 0 {
				fmt.Println("Nothing to clean up.")
			} else {
				fmt.Printf("Deleted %d checkpoint(s).\n", len(deleted))
			}

			// Phase 2: Prune orphaned blobs from the shared pool.
			result, err := registry.BlobGC(cfg.Store)
			if err != nil {
				return fmt.Errorf("blob garbage collection: %w", err)
			}
			if len(result.Deleted) > 0 {
				fmt.Printf("Pruned %d blob(s), freed %s.\n", len(result.Deleted), formatSize(int(result.BytesFreed)))
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&flagKeepLast, "keep-last", 0, "number of recent checkpoints to keep")
	cmd.Flags().BoolVar(&flagKeepTagged, "keep-tagged", false, "keep all tagged checkpoints")

	return cmd
}
