package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ANSI color codes
var (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[2m"
)

func init() {
	// Disable colors if stdout is not a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		colorReset = ""
		colorRed = ""
		colorGreen = ""
		colorYellow = ""
		colorDim = ""
	}
}

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

			_, store, err := loadConfigAndStore(dir)
			if err != nil {
				return err
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

			// Parse manifests for layer names
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
					fmt.Printf("  %s%s: unchanged%s (%s)\n",
						colorDim, name, colorReset, truncateDigest(layers1[i].Digest))
					continue
				}

				// Layer changed: show file-level diff
				sizes1, _ := workspace.ListLayerFilesWithSizes(layers1[i].Data)
				sizes2, _ := workspace.ListLayerFilesWithSizes(layers2[i].Data)

				var added, removed, modified []string
				for f := range sizes2 {
					if _, ok := sizes1[f]; !ok {
						added = append(added, f)
					} else if sizes1[f] != sizes2[f] {
						modified = append(modified, f)
					}
				}
				for f := range sizes1 {
					if _, ok := sizes2[f]; !ok {
						removed = append(removed, f)
					}
				}
				sort.Strings(added)
				sort.Strings(removed)
				sort.Strings(modified)

				fmt.Printf("  %s: changed (%s → %s)\n",
					name, formatSize(len(layers1[i].Data)), formatSize(len(layers2[i].Data)))

				for _, f := range added {
					fmt.Printf("    %s+ %s%s\n", colorGreen, f, colorReset)
				}
				for _, f := range removed {
					fmt.Printf("    %s- %s%s\n", colorRed, f, colorReset)
				}
				for _, f := range modified {
					fmt.Printf("    %s~ %s%s\n", colorYellow, f, colorReset)
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
