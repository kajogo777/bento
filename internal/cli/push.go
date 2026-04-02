package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets/backend"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var (
		flagTag            string
		flagIncludeSecrets bool
		flagRecipients     []string
	)

	cmd := &cobra.Command{
		Use:   "push [remote]",
		Short: "Push checkpoints to a remote registry",
		Long: `Push local checkpoints to an OCI registry. Uses Docker credential
helpers for authentication (docker login, crane auth, etc).

If the checkpoint has scrubbed secrets, use --include-secrets to pack them
(encrypted) into the OCI artifact. Without it, secrets are omitted and the
recipient must obtain them separately.

Included secrets are always encrypted — they can only be read by recipients
whose public keys were used during save (via bento.yaml or --recipient flags).
The sender is always included as a recipient automatically.

Examples:
  bento push
  bento push --include-secrets
  bento push --tag cp-3`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			remote := cfg.Remote
			if len(args) > 0 {
				remote = args[0]
			}
			if remote == "" {
				return fmt.Errorf("no remote configured. Set 'remote' in bento.yaml or pass a registry URL")
			}

			// Open local store
			store, err := registry.NewStore(cfg.StorePath())
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}

			// Check if checkpoint has scrubbed secrets.
			var pushCheckpoint int
			var hasScrubRecords bool

			{
				checkTag := flagTag
				if checkTag == "" {
					checkTag = "latest"
				}
				if _, configBytes, _, loadErr := store.LoadCheckpoint(checkTag); loadErr == nil {
					if bentoCfg, parseErr := manifest.UnmarshalConfig(configBytes); parseErr == nil {
						pushCheckpoint = bentoCfg.Checkpoint
						hasScrubRecords = len(bentoCfg.ScrubRecords) > 0
					}
				}
			}

			// If --include-secrets, inject the encrypted envelope as an OCI layer.
			if flagIncludeSecrets && hasScrubRecords {
				tag := flagTag
				if tag == "" {
					tag = "latest"
				}

				manifestBytes, _, _, loadErr := store.LoadCheckpoint(tag)
				if loadErr != nil {
					return fmt.Errorf("loading checkpoint %s: %w", tag, loadErr)
				}

				// Check if OCI layer already exists.
				var ociManifest ocispec.Manifest
				hasSecretsLayer := false
				if jsonErr := json.Unmarshal(manifestBytes, &ociManifest); jsonErr == nil {
					for _, ld := range ociManifest.Layers {
						if ld.Annotations[manifest.AnnotationSecretsEncrypted] == "true" {
							hasSecretsLayer = true
							break
						}
					}
				}

				if !hasSecretsLayer {
					// Read the encrypted envelope from local storage.
					cpTag := fmt.Sprintf("cp-%d", pushCheckpoint)
					envKey := cfg.ID + "/" + cpTag + ".enc"
					localBe := backend.DefaultBackend()
					ctx := context.Background()
					envelope, getErr := localBe.Get(ctx, envKey, nil)
					if getErr != nil {
						return fmt.Errorf("no encrypted envelope found for %s — save with secrets first", cpTag)
					}

					// Determine what to pack: multi-recipient envelope or old-format ciphertext.
					var secretsContent []byte
					layerAnnotations := map[string]string{
						manifest.AnnotationTitle:            "secrets",
						manifest.AnnotationSecretsEncrypted: "true",
					}

					if envJSON := envelope["envelope"]; envJSON != "" {
						// Multi-recipient envelope — pack the full JSON.
						secretsContent = []byte(envJSON)
						layerAnnotations[manifest.AnnotationSecretsKeyWrapping] = "curve25519"
						// Extract sender from envelope for annotation.
						var env backend.MultiRecipientEnvelope
						if json.Unmarshal([]byte(envJSON), &env) == nil {
							layerAnnotations[manifest.AnnotationSecretsSender] = env.Sender
						}
					} else {
						return fmt.Errorf("no wrapped envelope found for %s — re-save to generate one", cpTag)
					}

					// Pack as OCI layer and inject into the checkpoint.
					packed, packErr := workspace.PackBytesToTempLayer("secrets.enc", secretsContent)
					if packErr != nil {
						return fmt.Errorf("packing secrets layer: %w", packErr)
					}
					defer func() { _ = os.Remove(packed.Path) }()

					secretsLayer := registry.LayerData{
						MediaType: manifest.LayerMediaType,
						Path:      packed.Path,
						Digest:    packed.GzipDigest,
						Size:      packed.Size,
						DiffID:    packed.DiffID,
					}

					localStore := store.(*registry.LocalStore)
					if injectErr := localStore.InjectLayer(tag, secretsLayer, layerAnnotations); injectErr != nil {
						return fmt.Errorf("injecting secrets layer: %w", injectErr)
					}

					// Also update the cp-N tag to point to the new manifest.
					if cpTag != tag {
						if newDigest, resolveErr := store.ResolveTag(tag); resolveErr == nil {
							_ = store.Tag(newDigest, cpTag)
						}
					}
				} else {
					fmt.Println("Encrypted secrets layer already present.")
				}
			}

			// Determine which tags to push
			var tags []string
			if flagTag != "" {
				tags = []string{flagTag}
			}

			fmt.Printf("Pushing to %s...\n", remote)

			ctx := context.Background()
			if err := registry.PushToRemote(ctx, store, remote, tags); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}

			// Build the full remote ref for hints.
			pushTag := flagTag
			if pushTag == "" {
				pushTag = fmt.Sprintf("cp-%d", pushCheckpoint)
			}
			remoteRef := remote + ":" + pushTag

			fmt.Println("Done.")

			if flagIncludeSecrets {
				fmt.Printf("\nRecipients can open with:\n  bento open %s ./workspace\n  (auto-decrypts if their private key is in ~/.bento/keys/)\n", remoteRef)
			} else if !flagIncludeSecrets && hasScrubRecords {
				fmt.Println("\nWarning: this checkpoint has scrubbed secrets that are NOT included in the push.")
				fmt.Println("The recipient will not be able to restore secrets without additional steps.")
				fmt.Println("\nTo include encrypted secrets in the artifact:")
				fmt.Printf("  bento push --include-secrets %s\n", remote)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagTag, "tag", "", "push only this tag (default: push all)")
	cmd.Flags().BoolVar(&flagIncludeSecrets, "include-secrets", false, "include encrypted secret envelope in the OCI artifact")
	cmd.Flags().StringArrayVar(&flagRecipients, "recipient", nil, "recipient public key or name for key wrapping (repeatable)")

	return cmd
}
