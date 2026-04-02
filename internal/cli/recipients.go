package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/keys"
	"github.com/spf13/cobra"
)

func newRecipientsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "recipients",
		Aliases: []string{"r"},
		Short:   "Manage project recipients for secret sharing (writes to bento.yaml)",
	}
	cmd.AddCommand(
		newRecipientsAddCmd(),
		newRecipientsRemoveCmd(),
		newRecipientsListCmd(),
	)
	return cmd
}

func newRecipientsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <bento-pk-...>",
		Short: "Add a recipient to this project's bento.yaml",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			pubKeyStr := args[1]

			if !strings.HasPrefix(pubKeyStr, keys.PrefixPublicKey) {
				return fmt.Errorf("invalid public key — must start with %q", keys.PrefixPublicKey)
			}

			// Validate the key parses correctly.
			if _, err := keys.ParsePublicKey(pubKeyStr); err != nil {
				return fmt.Errorf("invalid public key: %w", err)
			}

			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found — run 'bento init' first")
			}

			// Check for duplicate name.
			for _, r := range cfg.Recipients {
				if r.Name == name {
					return fmt.Errorf("recipient %q already exists — remove first with: bento recipients remove %s", name, name)
				}
			}

			cfg.Recipients = append(cfg.Recipients, config.RecipientConfig{
				Name: name,
				Key:  pubKeyStr,
			})

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving bento.yaml: %w", err)
			}

			fmt.Printf("Added recipient %q to bento.yaml\n", name)
			return nil
		},
	}
}

func newRecipientsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a recipient from this project's bento.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found — run 'bento init' first")
			}

			found := false
			var updated []config.RecipientConfig
			for _, r := range cfg.Recipients {
				if r.Name == name {
					found = true
					continue
				}
				updated = append(updated, r)
			}

			if !found {
				return fmt.Errorf("recipient %q not found in bento.yaml", name)
			}

			cfg.Recipients = updated
			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving bento.yaml: %w", err)
			}

			fmt.Printf("Removed recipient %q from bento.yaml\n", name)
			return nil
		},
	}
}

func newRecipientsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recipients in this project's bento.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found — run 'bento init' first")
			}

			if len(cfg.Recipients) == 0 {
				fmt.Println("No recipients configured. Add one with:")
				fmt.Println("  bento recipients add <name> <bento-pk-...>")
				return nil
			}

			fmt.Println("Recipients (from bento.yaml):")
			for _, r := range cfg.Recipients {
				fmt.Printf("  %-12s %s\n", r.Name, r.Key)
			}
			return nil
		},
	}
}
