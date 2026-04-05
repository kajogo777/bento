package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/hooks"
	"github.com/kajogo777/bento/internal/keys"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/kajogo777/bento/internal/secrets/backend"
	"github.com/kajogo777/bento/internal/workspace"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newOpenCmd() *cobra.Command {
	var (
		flagLayers             string
		flagSkipLayers         string
		flagForce              bool
		flagPrivateKey         string
		flagAllowMissingSecrets bool
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
			}

			storePath := ""
			cfg, cfgErr := config.Load(sourceDir)
			if cfgErr == nil {
				if storeName == "" {
					storeName = cfg.ID
				}
				storePath = filepath.Join(cfg.Store, storeName)
			} else {
				if storeName == "" {
					storeName = filepath.Base(sourceDir)
				}
				storePath = filepath.Join(config.DefaultStorePath(), storeName)
			}

			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Load checkpoint (try local first, fall back to remote pull).
			// Layers are backed by temp files; we defer cleanup for all of them.
			manifestBytes, configBytes, layers, err := store.LoadCheckpoint(tag)
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
					manifestBytes, configBytes, layers, err = store.LoadCheckpoint(tag)
					if err != nil {
						return fmt.Errorf("loading checkpoint %s after pull: %w", ref, err)
					}
				} else {
					return fmt.Errorf("loading checkpoint %s: %w", ref, err)
				}
			}
			// Clean up temp files when done, regardless of error path below.
			defer func() {
				for i := range layers {
					layers[i].Cleanup()
				}
			}()

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

			// Build set of files in the checkpoint for cleanup (stream to avoid
			// loading entire layers into memory). List files from all layers concurrently.
			perLayerFiles := make([][]string, len(layersToRestore))

			kg := new(errgroup.Group)
			kg.SetLimit(runtime.NumCPU())

			for i := range layersToRestore {
				i := i // capture loop variable
				kg.Go(func() error {
					r, err := layersToRestore[i].NewReader()
					if err != nil {
						return nil // skip unreadable layers
					}
					files, err := workspace.ListLayerFilesFromReader(r)
					_ = r.Close()
					if err == nil {
						perLayerFiles[i] = files
					}
					return nil
				})
			}
			_ = kg.Wait() // errors are silently skipped (same as original behavior)

			// Merge all per-layer file lists into keepFiles (single goroutine, no mutex needed).
			keepFiles := make(map[string]bool)
			for _, files := range perLayerFiles {
				for _, f := range files {
					keepFiles[f] = true
				}
			}

			// Pre-check: if checkpoint has scrubbed secrets, verify we can resolve
			// them BEFORE writing any files. This prevents leaving broken files
			// on disk when secrets are unavailable.
			var secretValues map[string]string
			var hasScrubRecords bool
			var scrubRecords []manifest.ScrubFileRecord
			bentoCfg, parseErr := manifest.UnmarshalConfig(configBytes)
			if parseErr == nil && len(bentoCfg.ScrubRecords) > 0 {
				hasScrubRecords = true
				scrubRecords = bentoCfg.ScrubRecords

				backendKey := bentoCfg.WorkspaceID + "/" + tag
				ctx := context.Background()

				// Try 1: local encrypted envelope (same-machine opens).
				localBe := backend.DefaultBackend()
				envKey := backendKey + ".enc"
				if envData, envErr := localBe.Get(ctx, envKey, nil); envErr == nil {
					if envJSON := envData["envelope"]; envJSON != "" {
						if recipPub, recipPriv, kpErr := keys.LoadDefaultKeypair(); kpErr == nil {
							if sv, unwrapErr := backend.TryUnwrapEnvelope([]byte(envJSON), recipPub, recipPriv); unwrapErr == nil {
								secretValues = sv
							}
						}
					}
				}

				// Try 2: key wrapping auto-discovery from OCI layer.
				if secretValues == nil {
					var ociManifest ocispec.Manifest
					if jsonErr := json.Unmarshal(manifestBytes, &ociManifest); jsonErr == nil {
						for li, ld := range ociManifest.Layers {
							if ld.Annotations[manifest.AnnotationSecretsEncrypted] == "true" && li < len(layers) {
								r, rErr := layers[li].NewReader()
								if rErr == nil {
									cipherBytes, exErr := workspace.ExtractFileContentFromLayer(r, "secrets.enc", 10*1024*1024)
									_ = r.Close()
									if exErr == nil {
										// Check if this is a multi-recipient envelope (has wrappedKeys).
										var env backend.MultiRecipientEnvelope
										if json.Unmarshal(cipherBytes, &env) == nil && len(env.WrappedKeys) > 0 {
											// Try to unwrap with user's private key.
											var recipPub, recipPriv [32]byte
											var kpErr error
											if flagPrivateKey != "" {
												absPath, _ := filepath.Abs(flagPrivateKey)
												recipPub, recipPriv, kpErr = keys.LoadKeypairFrom(filepath.Dir(absPath), strings.TrimSuffix(filepath.Base(absPath), ".json"))
												if kpErr != nil {
													fmt.Printf("Warning: loading private key from %s: %v\n", flagPrivateKey, kpErr)
												}
											} else {
												recipPub, recipPriv, kpErr = keys.LoadDefaultKeypair()
											}
											if kpErr == nil {
												if sv, unwrapErr := backend.TryUnwrapEnvelope(cipherBytes, recipPub, recipPriv); unwrapErr == nil {
													secretValues = sv
													fmt.Println("  Decrypted secrets with keypair auto-discovery")
												}
											}
										}
									}
								}
								break
							}
						}
					}
				}

				// If secrets unavailable and not allowed to proceed, fail NOW before writing files.
				if secretValues == nil && !flagAllowMissingSecrets {
					totalSecrets := 0
					for _, rec := range scrubRecords {
						totalSecrets += len(rec.Replacements)
					}
					openArgs := ref
					if len(args) > 1 {
						openArgs = ref + " " + args[1]
					}
					fmt.Printf("\nError: %d scrubbed secret(s) in %d file(s) cannot be resolved.\n", totalSecrets, len(scrubRecords))
					fmt.Println("\nNo matching private key found. To restore secrets:")
					fmt.Println("  1. Import your private key:")
					fmt.Println("     bento keys import <bento-sk-...>")
					fmt.Println("  2. Or ask the sender to re-push with your public key:")
					fmt.Println("     bento keys list   # show your public key")
					fmt.Printf("     # sender runs: bento push --include-secrets --recipient <your-key> %s\n", openArgs)
					fmt.Println("\nTo open anyway with placeholders:")
					fmt.Printf("  bento open --allow-missing-secrets %s\n", openArgs)
					return fmt.Errorf("secrets not available — no matching private key")
				}
			}

			// Unpack layers (stream each layer directly to disk, no full load into memory)
			fmt.Printf("Restoring checkpoint %s (sequence %d)...\n", tag, info.Sequence)
			for i := range layersToRestore {
				r, err := layersToRestore[i].NewReader()
				if err != nil {
					return fmt.Errorf("opening layer %d: %w", i, err)
				}
				unpackErr := workspace.UnpackLayerWithExternalFromReader(r, targetDir)
				_ = r.Close()
				if unpackErr != nil {
					return fmt.Errorf("unpacking layer: %w", unpackErr)
				}
			}

			// Remove files not in the checkpoint (stale files from later saves)
			if len(keepFiles) > 0 {
				if err := workspace.CleanStaleFiles(targetDir, keepFiles); err != nil {
					fmt.Printf("Warning: cleaning stale files: %v\n", err)
				}
			}

			// Regenerate bento.yaml if the target directory doesn't have one.
			// This enables the "open anywhere" workflow: pull from a registry,
			// get a fully functional workspace that can save/push/diff immediately.
			// See specs/portable-config.md for the full specification.
			if _, statErr := os.Stat(filepath.Join(targetDir, "bento.yaml")); os.IsNotExist(statErr) {
				if parseErr == nil {
					newCfg := configFromArtifact(bentoCfg, storePath)
					if err := config.Save(targetDir, newCfg); err != nil {
						fmt.Printf("Warning: generating bento.yaml: %v\n", err)
					} else {
						fmt.Println("Generated bento.yaml from artifact metadata")
						// Also use the regenerated config for env hydration and hooks below
						cfg = newCfg
						cfgErr = nil
					}

					// Regenerate .bentoignore from embedded ignore patterns
					if len(bentoCfg.Ignore) > 0 {
						ignoreContent := "# Bento ignore patterns (restored from checkpoint)\n"
						for _, p := range bentoCfg.Ignore {
							ignoreContent += p + "\n"
						}
						ignorePath := filepath.Join(targetDir, ".bentoignore")
						if err := os.WriteFile(ignorePath, []byte(ignoreContent), 0644); err != nil {
							fmt.Printf("Warning: generating .bentoignore: %v\n", err)
						}
					}
				}
			}

			// Hydrate scrubbed secrets (already resolved in pre-check above).
			if hasScrubRecords && secretValues != nil {
				hydrated := 0
				for _, rec := range scrubRecords {
					filePath := filepath.Join(targetDir, filepath.FromSlash(rec.Path))
					fi, statErr := os.Stat(filePath)
					if statErr != nil {
						continue
					}
					content, readErr := os.ReadFile(filePath)
					if readErr != nil {
						continue
					}
					content = secrets.HydrateFile(content, secretValues)
					if writeErr := os.WriteFile(filePath, content, fi.Mode()); writeErr != nil {
						continue
					}
					hydrated += len(rec.Replacements)
				}
				if hydrated > 0 {
					fmt.Printf("Hydrated %d secret(s)\n", hydrated)
				}
			} else if hasScrubRecords && flagAllowMissingSecrets {
				fmt.Println("Warning: secrets not available. Files contain placeholders.")
			}

			// Validate secret references can resolve (dry-run hydration)
			if cfgErr == nil && cfg != nil && len(cfg.Env) > 0 {
				ctx := context.Background()
				_, errs := secrets.HydrateEnv(ctx, cfg.Env)
				for _, e := range errs {
					fmt.Printf("Warning: %v\n", e)
				}
			}

			// Update head in bento.yaml to track this directory's position.
			// Use config.UpdateHead to avoid triggering BackfillDefaults.
			manifestDigest := digest.FromBytes(manifestBytes).String()
			_ = config.UpdateHead(targetDir, manifestDigest)

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
	cmd.Flags().StringVar(&flagPrivateKey, "private-key", "", "explicit path to private key file for key wrapping")
	cmd.Flags().BoolVar(&flagAllowMissingSecrets, "allow-missing-secrets", false, "open even if secrets cannot be hydrated (files will contain placeholders)")

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

// configFromArtifact reconstructs a BentoConfig from the OCI config metadata
// embedded in a checkpoint. Local fields (id, store) get fresh values;
// portable fields are carried over from the artifact.
// See specs/portable-config.md for the full specification.
func configFromArtifact(obj *manifest.BentoConfigObj, sourceStorePath string) *config.BentoConfig {
	// Preserve the source workspace ID so the new directory shares
	// the same store and checkpoint history (like git worktrees).
	newID := obj.WorkspaceID
	if newID == "" {
		var err error
		newID, err = config.GenerateWorkspaceID()
		if err != nil {
			newID = "ws-restored"
		}
	}

	// Derive the store root from the source store path. The source store
	// path is <store-root>/<workspace-id>, so strip the workspace-id suffix
	// to get the root. This ensures the new directory uses the same store.
	storeRoot := config.DefaultStorePath()
	if sourceStorePath != "" {
		storeRoot = filepath.Dir(sourceStorePath)
	}

	cfg := &config.BentoConfig{
		ID:     newID,
		Store:  storeRoot,
		Task:   obj.Task,
		Remote: obj.Remote,
	}

	// Env vars and secret references
	if len(obj.Env) > 0 {
		cfg.Env = make(map[string]config.EnvEntry, len(obj.Env))
		for name, entry := range obj.Env {
			if entry.IsRef {
				fields := make(map[string]string)
				if entry.Path != "" {
					fields["path"] = entry.Path
				}
				if entry.Key != "" {
					fields["key"] = entry.Key
				}
				if entry.Var != "" {
					fields["var"] = entry.Var
				}
				if entry.Role != "" {
					fields["role"] = entry.Role
				}
				if entry.Command != "" {
					fields["command"] = entry.Command
				}
				cfg.Env[name] = config.NewSecretEnv(entry.Source, fields)
			} else {
				cfg.Env[name] = config.NewLiteralEnv(entry.Value)
			}
		}
	}

	// Custom layer definitions
	if len(obj.Layers) > 0 {
		cfg.Layers = make([]config.LayerConfig, len(obj.Layers))
		for i, l := range obj.Layers {
			cfg.Layers[i] = config.LayerConfig{
				Name:     l.Name,
				Patterns: l.Patterns,
				CatchAll: l.CatchAll,
			}
		}
	}

	// Hooks
	if obj.Hooks != nil {
		cfg.Hooks = config.HooksConfig{
			PreSave:     obj.Hooks.PreSave,
			PostSave:    obj.Hooks.PostSave,
			PostRestore: obj.Hooks.PostRestore,
			PrePush:     obj.Hooks.PrePush,
			PostPush:    obj.Hooks.PostPush,
			PostFork:    obj.Hooks.PostFork,
			Timeout:     obj.Hooks.Timeout,
		}
	}

	// Ignore patterns (stored in bento.yaml ignore field)
	if len(obj.Ignore) > 0 {
		cfg.Ignore = obj.Ignore
	}

	// Retention policy
	if obj.Retention != nil {
		cfg.Retention = config.RetentionConfig{
			KeepLast:   obj.Retention.KeepLast,
			KeepTagged: obj.Retention.KeepTagged,
		}
	}

	return cfg
}
