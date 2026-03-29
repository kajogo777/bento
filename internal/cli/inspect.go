package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	var flagFiles bool

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
				storeName = cfg.ID
			}

			storePath := filepath.Join(cfg.Store, storeName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Load manifest + config (lightweight, no layer blob downloads).
			manifestBytes, configBytes, err := store.LoadManifest(tag)
			if err != nil {
				return fmt.Errorf("loading checkpoint %s: %w", ref, err)
			}

			// Only load layer blobs when --files is requested.
			var layers []registry.LayerData
			if flagFiles {
				_, _, layers, err = store.LoadCheckpoint(tag)
				if err != nil {
					return fmt.Errorf("loading layer data for %s: %w", ref, err)
				}
				defer func() {
					for i := range layers {
						layers[i].Cleanup()
					}
				}()
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
			if info.Extensions != "" {
				fmt.Printf("Extensions: %s\n", info.Extensions)
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
					if len(cfgObj.Extensions) > 0 {
						fmt.Printf("  Extensions: %s\n", strings.Join(cfgObj.Extensions, ", "))
					}
					if len(cfgObj.Repos) > 0 {
						if len(cfgObj.Repos) == 1 && cfgObj.Repos[0].Path == "." {
							r := cfgObj.Repos[0]
							fmt.Printf("  Git:       %s (%s)\n", r.Branch, r.Sha[:12])
						} else {
							fmt.Println("  Repos:")
							for _, r := range cfgObj.Repos {
								sha := r.Sha
								if len(sha) > 12 {
									sha = sha[:12]
								}
								fmt.Printf("    %s: %s (%s)\n", r.Path, r.Branch, sha)
							}
						}
					}
					if cfgObj.Environment != nil {
						fmt.Printf("  Platform:  %s/%s\n", cfgObj.Environment.OS, cfgObj.Environment.Arch)
					}
				}
			}

			// Display layer summary and optional file trees
			fmt.Println("\nLayers:")
			var totalSize int64
			for i := range m.Layers {
				layerDesc := m.Layers[i]
				layerName := fmt.Sprintf("layer-%d", i)
				if name, ok := layerDesc.Annotations[manifest.AnnotationTitle]; ok {
					layerName = name
				}
				layerDigest := string(layerDesc.Digest)
				layerSize := layerDesc.Size
				totalSize += layerSize

				// Get file count from annotation.
				fileCount := 0
				if fc, ok := layerDesc.Annotations["dev.bento.layer.file-count"]; ok {
					_, _ = fmt.Sscanf(fc, "%d", &fileCount)
				}

				// If --files, read actual file list from layer blob.
				var files []string
				if flagFiles && i < len(layers) {
					r, err := layers[i].NewReader()
					if err == nil {
						files, _ = workspace.ListLayerFilesFromReader(r)
						_ = r.Close()
					}
					sort.Strings(files)
					fileCount = len(files)
				}

				fileWord := "files"
				if fileCount == 1 {
					fileWord = "file"
				}
				fmt.Printf("\n  [%d/%d] %s — %d %s, %s\n",
					i+1, len(m.Layers), layerName, fileCount, fileWord, formatSize(int(layerSize)))
				fmt.Printf("  %s digest: %s%s\n", colorDim, truncateDigest(layerDigest), colorReset)

				if flagFiles {
					if len(files) == 0 {
						fmt.Printf("    (empty)\n")
					} else {
						printFileTree(files, "    ")
					}
				}
			}

			fmt.Printf("\nTotal size: %s\n", formatSize(int(totalSize)))

			return nil
		},
	}

	cmd.Flags().BoolVar(&flagFiles, "files", false, "show file listing for each layer")

	return cmd
}
