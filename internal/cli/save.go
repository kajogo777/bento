package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/harness"
	"github.com/kajogo777/bento/internal/hooks"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

func newSaveCmd() *cobra.Command {
	var (
		flagMessage        string
		flagTag            string
		flagSkipSecretScan bool
	)

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a checkpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			// Load config
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			// Detect harness
			h := harness.ResolveHarness(dir, cfg.Harness)

			// Run pre_save hook
			hookCmd := cfg.Hooks.PreSave
			if hookCmd == "" {
				if defaults := h.DefaultHooks(); defaults["pre_save"] != "" {
					hookCmd = defaults["pre_save"]
				}
			}
			if hookCmd != "" {
				runner := hooks.NewRunner(dir, cfg.Hooks.Timeout)
				if err := runner.Run("pre_save", hookCmd); err != nil {
					return fmt.Errorf("pre_save hook failed: %w", err)
				}
			}

			// Collect ignore patterns
			ignorePatterns := append(config.DefaultIgnorePatterns, h.Ignore()...)
			ignorePatterns = append(ignorePatterns, cfg.Ignore...)
			if bentoIgnore, err := workspace.LoadBentoIgnore(dir); err == nil {
				ignorePatterns = append(ignorePatterns, bentoIgnore...)
			}

			// Scan workspace
			scanner := workspace.NewScanner(dir, h.Layers(), ignorePatterns)
			layerFiles, err := scanner.Scan()
			if err != nil {
				return fmt.Errorf("scanning workspace: %w", err)
			}

			// Secret scan
			if !flagSkipSecretScan {
				secretPatterns := h.SecretPatterns()
				if len(secretPatterns) > 0 {
					secretScanner, err := secrets.NewSecretScanner(secretPatterns)
					if err != nil {
						return fmt.Errorf("initializing secret scanner: %w", err)
					}
					var allFiles []string
					for _, files := range layerFiles {
						for _, f := range files {
							allFiles = append(allFiles, filepath.Join(dir, f))
						}
					}
					results, err := secretScanner.ScanFiles(allFiles)
					if err != nil {
						return fmt.Errorf("secret scan error: %w", err)
					}
					if len(results) > 0 {
						fmt.Println("Secret scan found potential secrets:")
						for _, r := range results {
							fmt.Printf("  %s:%d matched pattern: %s\n", r.File, r.Line, r.Pattern)
						}
						return fmt.Errorf("aborting save due to potential secrets. Use --skip-secret-scan to bypass")
					}
				}
			}

			// Get session config from harness
			sc, _ := h.SessionConfig(dir)

			// Open store
			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Determine checkpoint sequence (count unique digests)
			existing, _ := store.ListCheckpoints()
			seen := make(map[string]bool)
			for _, e := range existing {
				seen[e.Digest] = true
			}
			seq := len(seen) + 1

			// Find parent
			parentDigest := ""
			if len(existing) > 0 {
				if d, err := store.ResolveTag("latest"); err == nil {
					parentDigest = d
				}
			}

			// Build map of previous layer digests for change detection
			prevLayerDigests := make(map[string]string) // layer name -> digest
			if parentDigest != "" {
				if prevManifestBytes, _, _, loadErr := store.LoadCheckpoint(parentDigest); loadErr == nil {
					var prevManifest ocispec.Manifest
					if jsonErr := json.Unmarshal(prevManifestBytes, &prevManifest); jsonErr == nil {
						for _, ld := range prevManifest.Layers {
							if name, ok := ld.Annotations["org.opencontainers.image.title"]; ok {
								prevLayerDigests[name] = string(ld.Digest)
							}
						}
					}
				}
			}

			// Pack layers
			fmt.Println("Scanning workspace...")
			var layerInfos []manifest.LayerInfo
			for _, ld := range h.Layers() {
				files := layerFiles[ld.Name]
				data, err := workspace.PackLayer(dir, files)
				if err != nil {
					return fmt.Errorf("packing layer %s: %w", ld.Name, err)
				}

				mediaType := ld.MediaType
				if mediaType == "" {
					mediaType = manifest.MediaTypeForLayer(ld.Name)
				}

				status := "changed"
				if len(files) == 0 {
					status = "empty"
				} else if parentDigest != "" {
					newDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
					if prevDigest, ok := prevLayerDigests[ld.Name]; ok && prevDigest == newDigest {
						status = "unchanged, reusing"
					}
				}

				layerInfos = append(layerInfos, manifest.LayerInfo{
					Name:      ld.Name,
					MediaType: mediaType,
					Data:      data,
					FileCount: len(files),
					Frequency: string(ld.Frequency),
				})

				sizeStr := formatSize(len(data))
				fmt.Printf("  %-10s %d files, %s (%s)\n", ld.Name+":", len(files), sizeStr, status)
			}

			// Pack external paths (agent session data outside workspace)
			externalDefs := collectExternalDefs(h, cfg, dir)
			if len(externalDefs) > 0 {
				wsDefs := make([]workspace.ExternalPathDef, len(externalDefs))
				for i, d := range externalDefs {
					wsDefs[i] = workspace.ExternalPathDef{Source: d.Source, ArchivePrefix: d.ArchivePrefix}
				}
				extFiles, err := workspace.ScanExternalPaths(wsDefs, ignorePatterns)
				if err != nil {
					fmt.Printf("Warning: scanning external paths: %v\n", err)
				}
				if len(extFiles) > 0 {
					extData, err := workspace.PackExternalFiles(extFiles)
					if err != nil {
						return fmt.Errorf("packing external files: %w", err)
					}

					// Build path map annotation (archive prefix -> source with ~ for portability)
					pathMap := make(map[string]string)
					for _, d := range externalDefs {
						pathMap[d.ArchivePrefix] = d.Source
					}
					pathMapJSON, _ := json.Marshal(pathMap)

					status := "changed"
					if parentDigest != "" {
						newDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(extData))
						if prevDigest, ok := prevLayerDigests["external"]; ok && prevDigest == newDigest {
							status = "unchanged, reusing"
						}
					}

					layerInfos = append(layerInfos, manifest.LayerInfo{
						Name:        "external",
						MediaType:   manifest.LayerMediaType,
						Data:        extData,
						FileCount:   len(extFiles),
						Frequency:   string(harness.ChangesOften),
						Annotations: map[string]string{manifest.AnnotationExternalPaths: string(pathMapJSON)},
					})

					fmt.Printf("  %-10s %d files, %s (%s)\n", "external:", len(extFiles), formatSize(len(extData)), status)
				}
			}

			// Build config object
			cfgObj := &manifest.BentoConfigObj{
				SchemaVersion:    "1.0.0",
				Checkpoint:       seq,
				Created:          time.Now().UTC().Format(time.RFC3339),
				Status:           "paused",
				Harness:          h.Name(),
				ParentCheckpoint: parentDigest,
				Task:             cfg.Task,
				Environment: &manifest.Environment{
					OS:   runtime.GOOS,
					Arch: runtime.GOARCH,
				},
			}
			if sc != nil {
				cfgObj.Agent = sc.Agent
				cfgObj.AgentVersion = sc.AgentVersion
				cfgObj.GitSha = sc.GitSha
				cfgObj.GitBranch = sc.GitBranch
			}

			// Build manifest
			cfgObj.Message = flagMessage
			manifestBytes, configBytes, err := manifest.BuildManifest(cfgObj, layerInfos)
			if err != nil {
				return fmt.Errorf("building manifest: %w", err)
			}

			// Prepare layer data for store
			var storeLayerData []registry.LayerData
			for _, li := range layerInfos {
				digest := fmt.Sprintf("sha256:%x", sha256.Sum256(li.Data))
				storeLayerData = append(storeLayerData, registry.LayerData{
					MediaType: li.MediaType,
					Data:      li.Data,
					Digest:    digest,
				})
			}

			// Save to store
			tag := fmt.Sprintf("cp-%d", seq)
			if flagTag != "" {
				tag = flagTag
			}
			manifestDigest, err := store.SaveCheckpoint(tag, manifestBytes, configBytes, storeLayerData)
			if err != nil {
				return fmt.Errorf("saving checkpoint: %w", err)
			}

			// Also tag as latest
			if err := store.Tag(manifestDigest, "latest"); err != nil {
				return fmt.Errorf("tagging latest: %w", err)
			}

			if flagSkipSecretScan {
				fmt.Printf("Secret scan: skipped\n")
			} else {
				fmt.Printf("Secret scan: clean\n")
			}
			fmt.Printf("Tagged: %s, latest\n", tag)

			// Run post_save hook
			postHookCmd := cfg.Hooks.PostSave
			if postHookCmd == "" {
				if defaults := h.DefaultHooks(); defaults["post_save"] != "" {
					postHookCmd = defaults["post_save"]
				}
			}
			if postHookCmd != "" {
				runner := hooks.NewRunner(dir, cfg.Hooks.Timeout)
				if err := runner.Run("post_save", postHookCmd); err != nil {
					fmt.Printf("Warning: post_save hook failed: %v\n", err)
				}
			}

			_ = manifestDigest
			_ = strconv.Itoa(seq)

			return nil
		},
	}

	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "checkpoint message")
	cmd.Flags().StringVar(&flagTag, "tag", "", "custom tag for this checkpoint")
	cmd.Flags().BoolVar(&flagSkipSecretScan, "skip-secret-scan", false, "skip secret scanning")

	return cmd
}

// collectExternalDefs merges harness and config external path definitions.
func collectExternalDefs(h harness.Harness, cfg *config.BentoConfig, workDir string) []harness.ExternalPathDef {
	var defs []harness.ExternalPathDef
	defs = append(defs, h.ExternalPaths(workDir)...)
	for _, ep := range cfg.ExternalPaths {
		defs = append(defs, harness.ExternalPathDef{
			Source:        ep.Path,
			ArchivePrefix: "__external__/" + ep.Prefix + "/",
		})
	}
	return defs
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
