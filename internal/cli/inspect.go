package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

// printFileTree groups a sorted list of file paths by their top-level directory
// and prints them with the given indent prefix. Files in the root are printed
// directly; files in subdirectories are grouped under a "dir/ (N files)" header.
func printFileTree(files []string, indent string) {
	// Group by top-level directory (first path component)
	type dirGroup struct {
		dir   string
		files []string
	}
	groups := []dirGroup{}
	groupIdx := map[string]int{}

	for _, f := range files {
		slash := strings.Index(f, "/")
		var dir string
		if slash < 0 {
			dir = "" // root-level file
		} else {
			dir = f[:slash]
		}
		if idx, ok := groupIdx[dir]; ok {
			groups[idx].files = append(groups[idx].files, f)
		} else {
			groupIdx[dir] = len(groups)
			groups = append(groups, dirGroup{dir: dir, files: []string{f}})
		}
	}

	for _, g := range groups {
		if g.dir == "" {
			// Root-level files — print directly
			for _, f := range g.files {
				fmt.Printf("%s%s\n", indent, f)
			}
		} else if len(g.files) == 1 {
			fmt.Printf("%s%s\n", indent, g.files[0])
		} else {
			fileWord := "files"
			if len(g.files) == 1 {
				fileWord = "file"
			}
			fmt.Printf("%s%s/ (%d %s)\n", indent, g.dir, len(g.files), fileWord)
			for _, f := range g.files {
				fmt.Printf("%s  %s\n", indent, f)
			}
		}
	}
}

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [ref]",
		Short: "Show checkpoint metadata and layers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			ref := "latest"
			if len(args) > 0 {
				ref = args[0]
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			storeName, tag, err := registry.ParseRef(ref)
			if err != nil {
				return err
			}
			if storeName == "" {
				storeName = filepath.Base(dir)
			}

			storePath := filepath.Join(cfg.Store, storeName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			manifestBytes, configBytes, layers, err := store.LoadCheckpoint(tag)
			if err != nil {
				return fmt.Errorf("loading checkpoint %s: %w", ref, err)
			}

			// Parse checkpoint info
			info, err := manifest.ParseCheckpointInfo(manifestBytes)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			// Parse manifest for layer metadata
			var m ocispec.Manifest
			_ = json.Unmarshal(manifestBytes, &m)

			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes))

			fmt.Printf("Checkpoint: %s (sequence %d)\n", tag, info.Sequence)
			fmt.Printf("Digest:     %s\n", digest)
			if info.Parent != "" {
				fmt.Printf("Parent:     %s\n", info.Parent)
			}
			fmt.Printf("Created:    %s\n", info.Created)
			if info.Agent != "" {
				fmt.Printf("Agent:      %s\n", info.Agent)
			}
			if info.Message != "" {
				fmt.Printf("Message:    %s\n", info.Message)
			}

			// Display config
			if len(configBytes) > 0 {
				cfgObj, err := manifest.UnmarshalConfig(configBytes)
				if err == nil {
					fmt.Println("\nConfig:")
					if cfgObj.Task != "" {
						fmt.Printf("  Task:      %s\n", cfgObj.Task)
					}
					if cfgObj.Harness != "" {
						fmt.Printf("  Agent:     %s\n", cfgObj.Harness)
					}
					if cfgObj.GitBranch != "" {
						fmt.Printf("  Git:       %s (%s)\n", cfgObj.GitBranch, cfgObj.GitSha)
					}
					if cfgObj.Environment != nil {
						fmt.Printf("  Platform:  %s/%s\n", cfgObj.Environment.OS, cfgObj.Environment.Arch)
					}
				}
			}

			// Cleanup temp files when done.
			defer func() {
				for i := range layers {
					layers[i].Cleanup()
				}
			}()

			// Display layer file trees
			fmt.Println("\nLayers:")
			for i, ld := range layers {
				layerName := fmt.Sprintf("layer-%d", i)
				layerDigest := ld.Digest
				if i < len(m.Layers) {
					if name, ok := m.Layers[i].Annotations[manifest.AnnotationTitle]; ok {
						layerName = name
					}
				}

				r, err := ld.NewReader()
				var files []string
				var layerSize int64
				if err == nil {
					files, _ = workspace.ListLayerFilesFromReader(r)
					_ = r.Close()
				}
				sort.Strings(files)

				// Get size from the temp file stat, or Data len for in-memory layers.
				if ld.Path != "" {
					if fi, err := os.Stat(ld.Path); err == nil {
						layerSize = fi.Size()
					}
				} else {
					layerSize = int64(len(ld.Data))
				}

				fileWord := "files"
				if len(files) == 1 {
					fileWord = "file"
				}
				fmt.Printf("\n  [%d/%d] %s — %d %s, %s\n",
					i+1, len(layers), layerName, len(files), fileWord, formatSize(int(layerSize)))
				fmt.Printf("  %s digest: %s%s\n", colorDim, truncateDigest(layerDigest), colorReset)

				if len(files) == 0 {
					fmt.Printf("    (empty)\n")
				} else {
					printFileTree(files, "    ")
				}
			}

			return nil
		},
	}

	return cmd
}
