package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kajogo777/bento/internal/config"
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
		flagMessage             string
		flagTag                 string
		flagSkipSecretScan      bool
		flagAllowMissingExternal bool
	)

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a checkpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			// Resolve agent harness (uses config layers if defined, otherwise auto-detect)
			h := resolveHarness(dir, cfg)
			layers := h.Layers(dir)

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

			// Scan workspace (scanner handles both workspace and external patterns)
			scanner := workspace.NewScanner(dir, layers, ignorePatterns)
			scanResults, err := scanner.Scan()
			if err != nil {
				return fmt.Errorf("scanning workspace: %w", err)
			}

			// Secret scan (only workspace files, not external)
			if !flagSkipSecretScan {
				secretPatterns := h.SecretPatterns()
				if len(secretPatterns) > 0 {
					secretScanner, err := secrets.NewSecretScanner(secretPatterns)
					if err != nil {
						return fmt.Errorf("initializing secret scanner: %w", err)
					}
					var allFiles []string
					for _, sr := range scanResults {
						for _, f := range sr.WorkspaceFiles {
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

			sc, _ := h.SessionConfig(dir)

			// Open store
			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Determine checkpoint sequence
			existing, _ := store.ListCheckpoints()
			seen := make(map[string]bool)
			for _, e := range existing {
				seen[e.Digest] = true
			}
			seq := len(seen) + 1

			// Find parent digest
			parentDigest := ""
			if len(existing) > 0 {
				if d, err := store.ResolveTag("latest"); err == nil {
					parentDigest = d
				}
			}

			// Build map of previous layer digests for change detection.
			// Use LoadManifest (not LoadCheckpoint) — we only need the manifest,
			// not the layer blobs, so there's no need to load gigabytes of data.
			prevLayerDigests := make(map[string]string)
			if parentDigest != "" {
				if prevManifestBytes, _, loadErr := store.LoadManifest(parentDigest); loadErr == nil {
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
			for _, ld := range layers {
				sr := scanResults[ld.Name]
				wsFiles := sr.WorkspaceFiles
				extFiles := sr.ExternalFiles

				data, err := workspace.PackLayerWithExternal(dir, wsFiles, extFiles, flagAllowMissingExternal)
				if err != nil {
					return fmt.Errorf("packing layer %s: %w", ld.Name, err)
				}

				mediaType := ld.MediaType
				if mediaType == "" {
					mediaType = manifest.MediaTypeForLayer(ld.Name)
				}

				totalFiles := len(wsFiles) + len(extFiles)
				status := "changed"
				if totalFiles == 0 {
					status = "empty"
				} else if parentDigest != "" {
					newDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
					if prevDigest, ok := prevLayerDigests[ld.Name]; ok && prevDigest == newDigest {
						status = "unchanged, reusing"
					}
				}


				layerInfos = append(layerInfos, manifest.LayerInfo{
					Name:        ld.Name,
					MediaType:   mediaType,
					Data:        data,
					FileCount:   totalFiles,
					
				})

				extInfo := ""
				if len(extFiles) > 0 {
					extInfo = fmt.Sprintf(" (+%d external)", len(extFiles))
				}
				sizeStr := formatSize(len(data))
				fmt.Printf("  %-10s %d files%s, %s (%s)\n", ld.Name+":", totalFiles, extInfo, sizeStr, status)
			}

			// Build config object — include env vars and secret refs so the
			// checkpoint is self-describing when pushed to a remote registry.
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
			cfgObj.Message = flagMessage

			// Embed env vars (non-secret) into the manifest for portability
			if len(cfg.Env) > 0 {
				cfgObj.Env = cfg.Env
			}

			// Embed secret references (not values) into the manifest for portability
			if len(cfg.Secrets) > 0 {
				cfgObj.Secrets = make(map[string]manifest.SecretRef, len(cfg.Secrets))
				for name, s := range cfg.Secrets {
					cfgObj.Secrets[name] = manifest.SecretRef{
						Source:  s.Source,
						Path:    s.Fields["path"],
						Key:     s.Fields["key"],
						Var:     s.Fields["var"],
						Role:    s.Fields["role"],
						Command: s.Fields["command"],
					}
				}
			}

			// Embed env file mappings for portability
			if len(cfg.EnvFiles) > 0 {
				cfgObj.EnvFiles = make(map[string]manifest.EnvFileRef, len(cfg.EnvFiles))
				for path, ef := range cfg.EnvFiles {
					cfgObj.EnvFiles[path] = manifest.EnvFileRef{
						Template: ef.Template,
						Secrets:  ef.Secrets,
					}
				}
			}

			manifestBytes, configBytes, err := manifest.BuildManifest(cfgObj, layerInfos)
			if err != nil {
				return fmt.Errorf("building manifest: %w", err)
			}

			var storeLayerData []registry.LayerData
			for _, li := range layerInfos {
				layerDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(li.Data))
				storeLayerData = append(storeLayerData, registry.LayerData{
					MediaType: li.MediaType,
					Data:      li.Data,
					Digest:    layerDigest,
				})
			}

			tag := fmt.Sprintf("cp-%d", seq)
			if flagTag != "" {
				tag = flagTag
			}
			manifestDigest, err := store.SaveCheckpoint(tag, manifestBytes, configBytes, storeLayerData)
			if err != nil {
				return fmt.Errorf("saving checkpoint: %w", err)
			}

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

			return nil
		},
	}

	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "checkpoint message")
	cmd.Flags().StringVar(&flagTag, "tag", "", "custom tag for this checkpoint")
	cmd.Flags().BoolVar(&flagSkipSecretScan, "skip-secret-scan", false, "skip secret scanning")
	cmd.Flags().BoolVar(&flagAllowMissingExternal, "allow-missing-external", false, "warn instead of error when external agent session files are inaccessible")

	return cmd
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
