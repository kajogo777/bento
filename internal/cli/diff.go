package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <ref1> <ref2>",
		Short: "Compare two checkpoints",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			_, tag1, _ := registry.ParseRef(args[0])
			_, tag2, _ := registry.ParseRef(args[1])

			_, _, layers1, err := store.LoadCheckpoint(tag1)
			if err != nil {
				return fmt.Errorf("loading %s: %w", args[0], err)
			}

			_, _, layers2, err := store.LoadCheckpoint(tag2)
			if err != nil {
				return fmt.Errorf("loading %s: %w", args[1], err)
			}

			fmt.Printf("Comparing %s → %s\n\n", args[0], args[1])

			for i := 0; i < len(layers1) && i < len(layers2); i++ {
				if layers1[i].Digest == layers2[i].Digest {
					fmt.Printf("  Layer %d: unchanged (digest %s)\n", i, truncateDigest(layers1[i].Digest))
				} else {
					fmt.Printf("  Layer %d: changed\n", i)
					fmt.Printf("    from: %s (%d bytes)\n", truncateDigest(layers1[i].Digest), len(layers1[i].Data))
					fmt.Printf("    to:   %s (%d bytes)\n", truncateDigest(layers2[i].Digest), len(layers2[i].Data))
				}
			}

			return nil
		},
	}

	return cmd
}

func truncateDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}
