package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/manifest"
	"github.com/bentoci/bento/internal/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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

			manifestBytes1, _, layers1, err := store.LoadCheckpoint(tag1)
			if err != nil {
				return fmt.Errorf("loading %s: %w", args[0], err)
			}

			manifestBytes2, _, layers2, err := store.LoadCheckpoint(tag2)
			if err != nil {
				return fmt.Errorf("loading %s: %w", args[1], err)
			}

			// Parse manifests to get layer names from annotations
			var m1, m2 ocispec.Manifest
			_ = json.Unmarshal(manifestBytes1, &m1)
			_ = json.Unmarshal(manifestBytes2, &m2)

			layerName := func(descs []ocispec.Descriptor, idx int) string {
				if idx < len(descs) {
					if name, ok := descs[idx].Annotations[manifest.AnnotationTitle]; ok {
						return name
					}
				}
				return fmt.Sprintf("layer-%d", idx)
			}

			fmt.Printf("Comparing %s → %s\n\n", args[0], args[1])

			for i := 0; i < len(layers1) && i < len(layers2); i++ {
				name := layerName(m1.Layers, i)
				if layers1[i].Digest == layers2[i].Digest {
					fmt.Printf("  %s: unchanged (digest %s)\n", name, truncateDigest(layers1[i].Digest))
				} else {
					fmt.Printf("  %s: changed\n", name)
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
