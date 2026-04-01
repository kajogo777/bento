package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kajogo777/bento/internal/policy"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets/backend"
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

			// Capture checkpoint list before GC for backend cleanup.
			entriesBefore, _ := store.ListCheckpoints()

			deleted, err := policy.GarbageCollect(store, opts)
			if err != nil {
				return fmt.Errorf("garbage collection: %w", err)
			}

			// Clean up local secret entries for deleted checkpoints.
			if len(deleted) > 0 {
				be := backend.DefaultBackend()
				deletedSet := make(map[string]bool, len(deleted))
				for _, d := range deleted {
					deletedSet[d] = true
				}
				ctx := context.Background()
				cleaned := 0
				for _, e := range entriesBefore {
					if !deletedSet[e.Digest] {
						continue
					}
					if e.Tag == "" || e.Tag == "latest" {
						continue
					}
					key := cfg.ID + "/" + e.Tag
					_ = be.Delete(ctx, key)
					// Also clean the encrypted envelope.
					_ = be.Delete(ctx, key+".enc")
					cleaned++
				}
				if cleaned > 0 {
					fmt.Printf("Cleaned %d secret entry(ies).\n", cleaned)
				}
			}

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
