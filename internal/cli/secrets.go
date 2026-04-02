package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
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
// Exports the multi-recipient envelope for a checkpoint to stdout.
func newSecretsExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <ref>",
		Short: "Export multi-recipient envelope for a checkpoint",
		Long: `Export the multi-recipient envelope for a checkpoint to stdout.
The output is already encrypted — share it via any channel.
Recipients can decrypt using their private key.

  bento secrets export cp-3 > envelope.json`,
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

			// Read the pre-encrypted envelope.
			be := backend.DefaultBackend()
			envKey := cfg.ID + "/" + tag + ".enc"
			ctx := context.Background()
			envelope, err := be.Get(ctx, envKey, nil)
			if err != nil {
				return fmt.Errorf("no encrypted envelope for %s — was this checkpoint saved with secrets?", tag)
			}

			envelopeJSON := envelope["envelope"]
			if envelopeJSON == "" {
				return fmt.Errorf("encrypted envelope is empty for %s", tag)
			}

			fmt.Fprint(os.Stdout, envelopeJSON)
			return nil
		},
	}
}
