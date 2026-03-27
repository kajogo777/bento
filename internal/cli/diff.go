package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[2m"
)

func init() {
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

func diffWorkspace(dir string, args []string) error {
	cfg, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

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

	h := resolveHarness(dir, cfg)
	layerDefs := h.Layers(dir)

	ignorePatterns := append(config.DefaultIgnorePatterns, h.Ignore()...)
	ignorePatterns = append(ignorePatterns, cfg.Ignore...)
	if bentoIgnore, err := workspace.LoadBentoIgnore(dir); err == nil {
		ignorePatterns = append(ignorePatterns, bentoIgnore...)
	}

	scanner := workspace.NewScanner(dir, layerDefs, ignorePatterns)
	scanResults, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("scanning workspace: %w", err)
	}

	fmt.Printf("Comparing workspace → %s\n\n", tag)

	hasChanges := false
	for i, ld := range layerDefs {
		if i >= len(layers) {
			continue
		}

		savedHashes, _ := workspace.ListLayerFilesWithHashes(layers[i].Data)

		sr := scanResults[ld.Name]
		currentHashes := make(map[string]string)
		for _, f := range sr.WorkspaceFiles {
			if data, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
				currentHashes[f] = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}
		for _, ef := range sr.ExternalFiles {
			if data, err := os.ReadFile(ef.AbsPath); err == nil {
				currentHashes[ef.ArchivePath] = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}

		added, removed, modified := diffFileMaps(savedHashes, currentHashes)
		printLayerDiff(ld.Name, added, removed, modified, &hasChanges)
	}

	if !hasChanges {
		fmt.Println("No changes since last checkpoint.")
	}

	return nil
}

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

		hashes1, _ := workspace.ListLayerFilesWithHashes(layers1[i].Data)
		hashes2, _ := workspace.ListLayerFilesWithHashes(layers2[i].Data)

		added, removed, modified := diffFileMaps(hashes1, hashes2)

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
