package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/keys"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets/backend"
	"github.com/spf13/cobra"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage scrubbed secrets",
	}

	cmd.AddCommand(newSecretsExportCmd())
	return cmd
}

// newSecretsExportCmd creates "bento secrets export <ref>".
// Exports the multi-recipient envelope for a checkpoint to stdout,
// optionally re-wrapped with a different sender and/or additional recipients.
func newSecretsExportCmd() *cobra.Command {
	var (
		flagSender     string
		flagRecipients []string
	)

	cmd := &cobra.Command{
		Use:   "export <ref>",
		Short: "Export multi-recipient envelope for a checkpoint",
		Long: `Export the multi-recipient envelope for a checkpoint to stdout.
The output is encrypted — share it via any channel.
Recipients can decrypt using their private key.

Use --sender and --recipient to re-wrap for specific recipients.

  bento secrets export cp-3 > envelope.json
  bento secrets export cp-3 --sender work --recipient alice > envelope.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]

			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			_, tag, err := registry.ParseRef(ref)
			if err != nil {
				return err
			}

			// Read the encrypted envelope from local storage.
			be := backend.DefaultBackend()
			envKey := cfg.ID + "/" + tag + ".enc"
			ctx := context.Background()
			envelope, err := be.Get(ctx, envKey, nil)
			if err != nil {
				return fmt.Errorf("no encrypted envelope for %s — was this checkpoint saved with secrets?", tag)
			}

			envJSON := envelope["envelope"]
			if envJSON == "" {
				return fmt.Errorf("encrypted envelope is empty for %s", tag)
			}

			// If --sender or --recipient specified, re-wrap the envelope.
			needsRewrap := flagSender != "" || cfg.Sender != "" || len(flagRecipients) > 0 || len(cfg.Recipients) > 0
			if needsRewrap {
				// Determine sender keypair.
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
				// If no sender was specified, the sender IS the default keypair — reuse it.
				defaultPub, defaultPriv := senderPub, senderPriv
				if senderName != "" {
					var defaultErr error
					defaultPub, defaultPriv, defaultErr = keys.LoadDefaultKeypair()
					if defaultErr != nil {
						return fmt.Errorf("loading default keypair for unwrap: %w", defaultErr)
					}
				}

				// Resolve recipients.
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

				newEnvelope, rewrapErr := backend.RewrapEnvelope(
					[]byte(envJSON),
					defaultPub, defaultPriv,
					senderPub, senderPriv,
					recipientPubs,
				)
				if rewrapErr != nil {
					return fmt.Errorf("re-wrapping envelope: %w", rewrapErr)
				}

				rewrappedJSON, marshalErr := json.Marshal(newEnvelope)
				if marshalErr != nil {
					return fmt.Errorf("marshaling re-wrapped envelope: %w", marshalErr)
				}

				fmt.Fprintf(os.Stderr, "Re-wrapped for %d recipient(s), sealed by: %s\n", len(recipientPubs), newEnvelope.Sender)
				_, _ = fmt.Fprint(os.Stdout, string(rewrappedJSON))
			} else {
				_, _ = fmt.Fprint(os.Stdout, envJSON)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagSender, "sender", "", "keypair name to use as sender (overrides bento.yaml sender)")
	cmd.Flags().StringArrayVar(&flagRecipients, "recipient", nil, "additional recipient public key or name (repeatable)")
	return cmd
}
