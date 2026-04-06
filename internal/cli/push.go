package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/keys"
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
		flagSender         string
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

Secrets are encrypted and wrapped for the configured sender and recipients.
The sender is always included as a recipient automatically.

Sender and recipients can be configured in bento.yaml or overridden with
--sender and --recipient flags. CLI flags take precedence.

Examples:
  bento push
  bento push --include-secrets
  bento push --include-secrets --sender work --recipient alice
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

			// Handle secrets layer for push:
			// - Without --include-secrets: strip secrets layer (don't share)
			// - With --include-secrets: keep layer, re-wrap if sender/recipients changed
			{
				tag := flagTag
				if tag == "" {
					tag = "latest"
				}

				manifestBytes, _, _, loadErr := store.LoadCheckpoint(tag)
				if loadErr != nil {
					return fmt.Errorf("loading checkpoint %s: %w", tag, loadErr)
				}

				// Check if OCI layer exists.
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

				if hasSecretsLayer && !flagIncludeSecrets {
					// Strip the secrets layer — user didn't opt into sharing secrets.
					localStore := store.(*registry.LocalStore)
					if removeErr := localStore.RemoveSecretsLayer(tag); removeErr != nil {
						return fmt.Errorf("removing secrets layer for push: %w", removeErr)
					}

					// Also update the cp-N tag to point to the stripped manifest.
					cpTag := fmt.Sprintf("cp-%d", pushCheckpoint)
					if cpTag != tag {
						if newDigest, resolveErr := store.ResolveTag(tag); resolveErr == nil {
							_ = store.Tag(newDigest, cpTag)
						}
					}

					fmt.Println("Secrets layer stripped (use --include-secrets to share)")
				} else if flagIncludeSecrets && hasScrubRecords {
					// Read the existing envelope from OCI layer.
					envBytes, envErr := extractSecretsEnvelope(store, manifestBytes)
					if envErr != nil {
						return fmt.Errorf("reading secrets layer: %w", envErr)
					}
					if envBytes == nil {
						return fmt.Errorf("no secrets layer found — re-save with secrets to generate one")
					}

					// Re-wrap if sender or recipients changed; otherwise keep as-is.
					if len(flagRecipients) > 0 || flagSender != "" {
						// Remove existing secrets layer before re-injecting.
						if hasSecretsLayer {
							localStore := store.(*registry.LocalStore)
							if removeErr := localStore.RemoveSecretsLayer(tag); removeErr != nil {
								return fmt.Errorf("removing existing secrets layer: %w", removeErr)
							}
						}

						// Determine sender keypair for push.
						var senderPub, senderPriv [32]byte
						senderName := flagSender
						if senderName == "" {
							senderName = cfg.Sender
						}
						if senderName != "" {
							var kpErr error
							senderPub, senderPriv, kpErr = keys.LoadKeypair(senderName)
							if kpErr != nil {
								return fmt.Errorf("loading sender keypair %q: %w", senderName, kpErr)
							}
						} else {
							var kpErr error
							senderPub, senderPriv, _, kpErr = keys.LoadOrCreateKeypair()
							if kpErr != nil {
								return fmt.Errorf("loading default keypair: %w", kpErr)
							}
						}

						// Unwrap DEK from save-time envelope using default keypair.
						defaultPub, defaultPriv := senderPub, senderPriv
						if senderName != "" {
							var defaultErr error
							defaultPub, defaultPriv, defaultErr = keys.LoadDefaultKeypair()
							if defaultErr != nil {
								return fmt.Errorf("loading default keypair for unwrap: %w", defaultErr)
							}
						}

						// Resolve recipients: bento.yaml + CLI flags + sender as self.
						var recipientSpecs []string
						for _, r := range cfg.Recipients {
							recipientSpecs = append(recipientSpecs, r.Name)
						}
						recipientSpecs = append(recipientSpecs, flagRecipients...)

						var configRecipients []keys.ConfigRecipient
						for _, r := range cfg.Recipients {
							configRecipients = append(configRecipients, keys.ConfigRecipient{
								Name: r.Name,
								Key:  r.Key,
							})
						}

						recipientPubs, resolveErr := keys.ResolveRecipients(recipientSpecs, configRecipients, senderPub, "")
						if resolveErr != nil {
							return fmt.Errorf("resolving recipients: %w", resolveErr)
						}

						// Re-wrap the envelope for the push sender and recipients.
						newEnvelope, rewrapErr := backend.RewrapEnvelope(
							envBytes,
							defaultPub, defaultPriv,
							senderPub, senderPriv,
							recipientPubs,
						)
						if rewrapErr != nil {
							return fmt.Errorf("re-wrapping envelope: %w", rewrapErr)
						}

						secretsContent, marshalErr := json.Marshal(newEnvelope)
						if marshalErr != nil {
							return fmt.Errorf("marshaling re-wrapped envelope: %w", marshalErr)
						}

						layerAnnotations := map[string]string{
							manifest.AnnotationTitle:              "secrets",
							manifest.AnnotationSecretsEncrypted:   "true",
							manifest.AnnotationSecretsKeyWrapping: "curve25519",
							manifest.AnnotationSecretsSender:      newEnvelope.Sender,
						}

						fmt.Printf("Re-wrapped secrets for %d recipient(s)\n", len(recipientPubs))
						fmt.Printf("Sealed by: %s\n", newEnvelope.Sender)

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
						cpTag := fmt.Sprintf("cp-%d", pushCheckpoint)
						if cpTag != tag {
							if newDigest, resolveErr := store.ResolveTag(tag); resolveErr == nil {
								_ = store.Tag(newDigest, cpTag)
							}
						}
					} else {
						fmt.Println("Encrypted secrets layer already present.")
					}
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
	cmd.Flags().StringVar(&flagSender, "sender", "", "keypair name to use as sender (overrides bento.yaml sender)")
	cmd.Flags().StringArrayVar(&flagRecipients, "recipient", nil, "additional recipient public key or name (repeatable)")

	return cmd
}
