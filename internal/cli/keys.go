package cli

import (
	"fmt"

	"github.com/kajogo777/bento/internal/keys"
	"github.com/spf13/cobra"
)

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage keypairs (your identity)",
	}
	cmd.AddCommand(
		newKeysGenerateCmd(),
		newKeysListCmd(),
		newKeysImportCmd(),
	)
	return cmd
}

func newKeysGenerateCmd() *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new keypair",
		RunE: func(cmd *cobra.Command, args []string) error {
			name := flagName
			if name == "" {
				name = "default"
			}

			// Check if keypair already exists.
			if _, _, err := keys.LoadKeypair(name); err == nil {
				return fmt.Errorf("keypair %q already exists — use a different --name or delete %s/%s.json first",
					name, keys.DefaultKeysDir(), name)
			}

			pub, priv, err := keys.GenerateKeypair()
			if err != nil {
				return err
			}

			if err := keys.SaveKeypair(name, pub, priv); err != nil {
				return err
			}

			fmt.Printf("Generated keypair %q:\n", name)
			fmt.Printf("  Public key:  %s\n", keys.FormatPublicKey(pub))
			fmt.Printf("  Private key: saved to %s/%s.json\n", keys.DefaultKeysDir(), name)
			fmt.Println()
			fmt.Println("Share your public key with teammates:")
			fmt.Printf("  %s\n", keys.FormatPublicKey(pub))

			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "name for the keypair (default: \"default\")")
	return cmd
}

func newKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your keypairs",
		RunE: func(cmd *cobra.Command, args []string) error {
			kps, err := keys.ListKeypairs()
			if err != nil {
				return err
			}

			if len(kps) == 0 {
				fmt.Println("No keypairs found. One will be auto-generated on first save,")
				fmt.Println("or generate now with:")
				fmt.Println("  bento keys generate")
			} else {
				fmt.Println("Keypairs:")
				for _, kp := range kps {
					created := kp.Created
					if len(created) >= 10 {
						created = created[:10]
					}
					fmt.Printf("  %-12s %s    (created %s)\n", kp.Name, kp.PublicKey, created)
				}
			}

			return nil
		},
	}
}

func newKeysImportCmd() *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "import <bento-sk-...>",
		Short: "Import a private key from its bento-sk-... string",
		Long: `Import a private key by pasting the bento-sk-... string.
The public key is derived automatically from the private key.

Get the private key string from the source machine:
  bento keys list --show-private
  # or: cat ~/.bento/keys/default.json | jq -r .privateKey`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			privKeyStr := args[0]

			// Parse the private key.
			priv, err := keys.ParsePrivateKey(privKeyStr)
			if err != nil {
				return fmt.Errorf("invalid private key: %w", err)
			}

			// Derive public key from private key.
			pub, err := keys.DerivePublicKey(priv)
			if err != nil {
				return fmt.Errorf("deriving public key: %w", err)
			}

			name := flagName
			if name == "" {
				name = "default"
			}

			// Check if keypair already exists.
			if _, _, loadErr := keys.LoadKeypair(name); loadErr == nil {
				return fmt.Errorf("keypair %q already exists — use a different --name or delete %s/%s.json first",
					name, keys.DefaultKeysDir(), name)
			}

			if err := keys.SaveKeypair(name, pub, priv); err != nil {
				return err
			}

			fmt.Printf("Imported keypair %q:\n", name)
			fmt.Printf("  Public key:  %s\n", keys.FormatPublicKey(pub))
			fmt.Printf("  Saved to:    %s/%s.json\n", keys.DefaultKeysDir(), name)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "name for the imported keypair (default: \"default\")")
	return cmd
}
