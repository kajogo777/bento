package cli

import (
	"fmt"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/keys"
	"github.com/spf13/cobra"
)

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage Curve25519 keypairs for secret sharing",
	}
	cmd.AddCommand(
		newKeysGenerateCmd(),
		newKeysListCmd(),
		newKeysPublicCmd(),
		newKeysImportCmd(),
		newKeysAddRecipientCmd(),
		newKeysRemoveRecipientCmd(),
	)
	return cmd
}

func newKeysGenerateCmd() *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new Curve25519 keypair",
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
		Short: "List keypairs and known recipients",
		RunE: func(cmd *cobra.Command, args []string) error {
			kps, err := keys.ListKeypairs()
			if err != nil {
				return err
			}

			if len(kps) == 0 {
				fmt.Println("No keypairs found. Generate one with:")
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

			// List recipients.
			var configRecipients []keys.ConfigRecipient
			dir, _ := cmd.Flags().GetString("dir")
			if dir == "" {
				dir = flagDir
			}
			if cfg, err := config.Load(dir); err == nil {
				for _, r := range cfg.Recipients {
					configRecipients = append(configRecipients, keys.ConfigRecipient{
						Name: r.Name,
						Key:  r.Key,
					})
				}
			}

			recipients := keys.ListRecipients(configRecipients, "")
			if len(recipients) > 0 {
				fmt.Println("\nRecipients:")
				for _, r := range recipients {
					fmt.Printf("  %-12s %s    (from %s)\n", r.Name, r.PublicKey, r.Source)
				}
			}

			return nil
		},
	}
}

func newKeysPublicCmd() *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "public",
		Short: "Show public key (for sharing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var pub [32]byte
			var err error

			if flagName != "" {
				pub, _, err = keys.LoadKeypair(flagName)
			} else {
				pub, _, err = keys.LoadDefaultKeypair()
			}
			if err != nil {
				return err
			}

			fmt.Println(keys.FormatPublicKey(pub))
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "keypair name (default: use default keypair)")
	return cmd
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

func newKeysAddRecipientCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-recipient <name> <bento-pk-...>",
		Short: "Import a recipient's public key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			pubKeyStr := args[1]

			if !strings.HasPrefix(pubKeyStr, keys.PrefixPublicKey) {
				return fmt.Errorf("invalid public key — must start with %q", keys.PrefixPublicKey)
			}

			if err := keys.AddRecipient(name, pubKeyStr); err != nil {
				return err
			}

			fmt.Printf("Added recipient %q\n", name)
			return nil
		},
	}
}

func newKeysRemoveRecipientCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-recipient <name>",
		Short: "Remove a recipient",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if err := keys.RemoveRecipient(name); err != nil {
				return err
			}

			fmt.Printf("Removed recipient %q\n", name)
			return nil
		},
	}
}
