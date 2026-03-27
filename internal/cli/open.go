package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/hooks"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var (
		flagLayers     string
		flagSkipLayers string
		flagForce      bool
	)

	cmd := &cobra.Command{
		Use:   "open <ref> [target-dir]",
		Short: "Restore a checkpoint",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			targetDir := flagDir
			if len(args) > 1 {
				targetDir = args[1]
			}
			targetDir, err := filepath.Abs(targetDir)
			if err != nil {
				return err
			}

			// Determine if ref is a full registry URL (contains /) or a local ref
			sourceDir, _ := filepath.Abs(flagDir)
			isRemoteRef := strings.Contains(ref, "/")

			var storeName, tag string
			var remoteRef string

			if isRemoteRef {
				// Full registry ref like "ghcr.io/myorg/ws/project:cp-1"
				if idx := strings.LastIndex(ref, ":"); idx > 0 && !strings.Contains(ref[idx:], "/") {
					remoteRef = ref[:idx]
					tag = ref[idx+1:]
				} else {
					remoteRef = ref
					tag = "latest"
				}
				// Use last path segment as local cache name
				parts := strings.Split(remoteRef, "/")
				storeName = parts[len(parts)-1]
			} else {
				var parseErr error
				storeName, tag, parseErr = registry.ParseRef(ref)
				if parseErr != nil {
					return parseErr
				}
				if storeName == "" {
					storeName = filepath.Base(sourceDir)
				}
			}

			storePath := ""
			cfg, cfgErr := config.Load(sourceDir)
			if cfgErr == nil {
				storePath = filepath.Join(cfg.Store, storeName)
			} else {
				storePath = filepath.Join(config.DefaultStorePath(), storeName)
			}

			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Load checkpoint (try local first, fall back to remote pull)
			manifestBytes, _, layers, err := store.LoadCheckpoint(tag)
			if err != nil {
				// Determine remote to pull from
				pullRef := remoteRef
				if pullRef == "" && cfgErr == nil && cfg.Remote != "" {
					pullRef = cfg.Remote + "/" + storeName
				}
				if pullRef != "" {
					fmt.Printf("Pulling from %s:%s...\n", pullRef, tag)
					localStore := store.(*registry.LocalStore)
					ctx := context.Background()
					if pullErr := registry.PullFromRemote(ctx, localStore, pullRef, tag); pullErr != nil {
						return fmt.Errorf("checkpoint %s not found locally and pull failed: %w", ref, pullErr)
					}
					// Retry local load
					manifestBytes, _, layers, err = store.LoadCheckpoint(tag)
					if err != nil {
						return fmt.Errorf("loading checkpoint %s after pull: %w", ref, err)
					}
				} else {
					return fmt.Errorf("loading checkpoint %s: %w", ref, err)
				}
			}

			// Parse manifest to get layer info
			info, err := manifest.ParseCheckpointInfo(manifestBytes)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			// Filter layers if requested
			layersToRestore := layers
			if flagLayers != "" {
				wanted := strings.Split(flagLayers, ",")
				layersToRestore = filterLayers(layers, manifestBytes, wanted, false)
			} else if flagSkipLayers != "" {
				skipped := strings.Split(flagSkipLayers, ",")
				layersToRestore = filterLayers(layers, manifestBytes, skipped, true)
			}

			// Build set of files in the checkpoint for cleanup
			keepFiles := make(map[string]bool)
			for _, ld := range layersToRestore {
				files, err := workspace.ListLayerFiles(ld.Data)
				if err == nil {
					for _, f := range files {
						keepFiles[f] = true
					}
				}
			}

			// Unpack layers
			fmt.Printf("Restoring checkpoint %s (sequence %d)...\n", tag, info.Sequence)
			for _, ld := range layersToRestore {
				if err := workspace.UnpackLayer(ld.Data, targetDir); err != nil {
					return fmt.Errorf("unpacking layer: %w", err)
				}
			}

			// Remove files not in the checkpoint (stale files from later saves)
			if len(keepFiles) > 0 {
				if err := workspace.CleanStaleFiles(targetDir, keepFiles); err != nil {
					fmt.Printf("Warning: cleaning stale files: %v\n", err)
				}
			}

			// Hydrate env vars and secrets
			if cfgErr == nil && cfg != nil {
				allVars := make(map[string]string)

				// Plain env vars from bento.yaml
				for k, v := range cfg.Env {
					allVars[k] = v
				}

				// Resolved secrets
				if len(cfg.Secrets) > 0 {
					ctx := context.Background()
					resolved, errs := secrets.HydrateSecrets(ctx, cfg.Secrets)
					for _, e := range errs {
						fmt.Printf("Warning: secret hydration: %v\n", e)
					}
					for k, v := range resolved {
						allVars[k] = v
					}
				}

				// Populate env files
				for envPath, envFile := range cfg.EnvFiles {
					templatePath := ""
					if envFile.Template != "" {
						templatePath = filepath.Join(targetDir, envFile.Template)
					}
					outputPath := filepath.Join(targetDir, envPath)

					// Filter to only the secrets listed in the env file config
					fileVars := make(map[string]string)
					for k, v := range allVars {
						fileVars[k] = v
					}

					if err := secrets.PopulateEnvFile(templatePath, outputPath, fileVars); err != nil {
						fmt.Printf("Warning: populating %s: %v\n", envPath, err)
					}
				}
			}

			fmt.Printf("Restored to %s\n", targetDir)

			// Run post_restore hook
			if cfgErr == nil && cfg != nil {
				hookCmd := cfg.Hooks.PostRestore
				if hookCmd != "" {
					runner := hooks.NewRunner(targetDir, cfg.Hooks.Timeout)
					if err := runner.Run("post_restore", hookCmd); err != nil {
						fmt.Printf("Warning: post_restore hook failed: %v\n", err)
					}
				}
			}

			_ = flagForce
			return nil
		},
	}

	cmd.Flags().StringVar(&flagLayers, "layers", "", "comma-separated layer names to restore")
	cmd.Flags().StringVar(&flagSkipLayers, "skip-layers", "", "comma-separated layer names to skip")
	cmd.Flags().BoolVar(&flagForce, "force", false, "overwrite existing files without confirmation")

	return cmd
}

// filterLayers filters layer data based on names parsed from the manifest.
func filterLayers(layers []registry.LayerData, manifestBytes []byte, names []string, exclude bool) []registry.LayerData {
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[strings.TrimSpace(n)] = true
	}

	// Parse the manifest to get layer descriptors with title annotations.
	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		// If we can't parse the manifest, return all layers unchanged.
		return layers
	}

	var result []registry.LayerData
	for i, ld := range layers {
		layerName := ""
		if i < len(m.Layers) {
			layerName = m.Layers[i].Annotations[manifest.AnnotationTitle]
		}

		matched := nameSet[layerName]
		if exclude {
			// Keep layers that are NOT in the excluded set.
			if !matched {
				result = append(result, ld)
			}
		} else {
			// Keep layers that ARE in the wanted set.
			if matched {
				result = append(result, ld)
			}
		}
	}
	return result
}
