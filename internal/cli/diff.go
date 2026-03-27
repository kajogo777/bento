package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/harness"
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
		Use:   "diff [ref1] [ref2]",
		Short: "Show changes since last checkpoint, or compare two checkpoints",
		Long: `With no arguments, shows what changed in the workspace since the last save.
With one argument, compares that checkpoint to the current workspace.
With two arguments, compares two checkpoints.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			if len(args) <= 1 {
				return diffWorkspace(dir, args)
			}
			return diffCheckpoints(dir, args)
		},
	}

	return cmd
}

// diffWorkspace compares the current workspace against a checkpoint (default: latest).
func diffWorkspace(dir string, args []string) error {
	cfg, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

	// Resolve which checkpoint to compare against
	ref := "latest"
	if len(args) == 1 {
		ref = args[0]
	}
	_, tag, _ := registry.ParseRef(ref)

	manifestBytes, _, layers, err := store.LoadCheckpoint(tag)
	if err != nil {
		return fmt.Errorf("no checkpoints found. Run `bento save` first")
	}

	var m ocispec.Manifest
	_ = json.Unmarshal(manifestBytes, &m)

	// Scan current workspace using same logic as save
	h := harness.ResolveHarness(dir, cfg.Harness)
	ignorePatterns := append(config.DefaultIgnorePatterns, h.Ignore()...)
	ignorePatterns = append(ignorePatterns, cfg.Ignore...)
	if bentoIgnore, err := workspace.LoadBentoIgnore(dir); err == nil {
		ignorePatterns = append(ignorePatterns, bentoIgnore...)
	}

	scanner := workspace.NewScanner(dir, h.Layers(), ignorePatterns)
	currentFiles, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("scanning workspace: %w", err)
	}

	fmt.Printf("Comparing workspace → %s\n\n", tag)

	hasChanges := false
	for i, ld := range h.Layers() {
		name := ld.Name
		if i >= len(layers) {
			continue
		}

		// Get saved files from checkpoint layer
		savedSizes, _ := workspace.ListLayerFilesWithSizes(layers[i].Data)

		// Get current files for this layer
		currentLayerFiles := currentFiles[name]
		currentSizes := make(map[string]int64)
		for _, f := range currentLayerFiles {
			info, err := os.Stat(filepath.Join(dir, f))
			if err == nil {
				currentSizes[f] = info.Size()
			}
		}

		var added, removed, modified []string
		for f, size := range currentSizes {
			if _, ok := savedSizes[f]; !ok {
				added = append(added, f)
			} else if savedSizes[f] != size {
				modified = append(modified, f)
			}
		}
		for f := range savedSizes {
			if _, ok := currentSizes[f]; !ok {
				removed = append(removed, f)
			}
		}
		sort.Strings(added)
		sort.Strings(removed)
		sort.Strings(modified)

		if len(added) == 0 && len(removed) == 0 && len(modified) == 0 {
			fmt.Printf("  %s%s: unchanged%s\n", colorDim, name, colorReset)
			continue
		}

		hasChanges = true
		total := len(added) + len(removed) + len(modified)
		fmt.Printf("  %s: %d change(s)\n", name, total)

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

	// Check external layer (agent session data outside workspace)
	// The external layer is appended after the regular layers in the manifest
	externalIdx := len(h.Layers())
	if externalIdx < len(layers) {
		// Check if this layer is the external layer via annotation
		if externalIdx < len(m.Layers) {
			if _, ok := m.Layers[externalIdx].Annotations[manifest.AnnotationExternalPaths]; ok {
				savedSizes, _ := workspace.ListLayerFilesWithSizes(layers[externalIdx].Data)

				// Scan current external paths
				externalDefs := collectExternalDefs(h, cfg, dir)
				currentExtSizes := make(map[string]int64)
				if len(externalDefs) > 0 {
					wsDefs := make([]workspace.ExternalPathDef, len(externalDefs))
					for i, d := range externalDefs {
						wsDefs[i] = workspace.ExternalPathDef{Source: harness.ExpandHome(d.Source), ArchivePrefix: d.ArchivePrefix}
					}
					extFiles, err := workspace.ScanExternalPaths(wsDefs, ignorePatterns)
					if err == nil {
						for _, ef := range extFiles {
							info, err := os.Stat(ef.AbsPath)
							if err == nil {
								currentExtSizes[ef.ArchivePath] = info.Size()
							}
						}
					}
				}

				var added, removed, modified []string
				for f, size := range currentExtSizes {
					if _, ok := savedSizes[f]; !ok {
						added = append(added, f)
					} else if savedSizes[f] != size {
						modified = append(modified, f)
					}
				}
				for f := range savedSizes {
					if _, ok := currentExtSizes[f]; !ok {
						removed = append(removed, f)
					}
				}
				sort.Strings(added)
				sort.Strings(removed)
				sort.Strings(modified)

				if len(added) > 0 || len(removed) > 0 || len(modified) > 0 {
					hasChanges = true
					total := len(added) + len(removed) + len(modified)
					fmt.Printf("  external: %d change(s)\n", total)
					for _, f := range added {
						fmt.Printf("    %s+ %s%s\n", colorGreen, f, colorReset)
					}
					for _, f := range removed {
						fmt.Printf("    %s- %s%s\n", colorRed, f, colorReset)
					}
					for _, f := range modified {
						fmt.Printf("    %s~ %s%s\n", colorYellow, f, colorReset)
					}
				} else {
					fmt.Printf("  %sexternal: unchanged%s\n", colorDim, colorReset)
				}
			}
		}
	}

	if !hasChanges {
		fmt.Println("No changes since last checkpoint.")
	}

	return nil
}

// diffCheckpoints compares two saved checkpoints.
func diffCheckpoints(dir string, args []string) error {
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
}

func truncateDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}
