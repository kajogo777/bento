package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		flagTask string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace tracking",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			// Check if already initialized
			if _, err := os.Stat(filepath.Join(dir, "bento.yaml")); err == nil {
				return fmt.Errorf("bento.yaml already exists in %s", dir)
			}

			// Detect active extensions
			exts := extension.DetectAll(dir)
			names := make([]string, len(exts))
			for i, e := range exts {
				names[i] = e.Name()
			}
			fmt.Printf("Detected extensions: %s\n", strings.Join(names, ", "))

			cfg := &config.BentoConfig{
				Store: config.DefaultStorePath(),
				Task:  flagTask,
			}

			id, err := config.GenerateWorkspaceID()
			if err != nil {
				return err
			}
			cfg.ID = id

			// Populate all default values so they are visible in bento.yaml
			// from the start. Users can then see and tune retention, watch,
			// etc. without guessing what the hidden defaults are.
			cfg.BackfillDefaults()
			cfg.Retention.KeepTagged = true

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("writing bento.yaml: %w", err)
			}

			fmt.Println("Created bento.yaml")
			fmt.Printf("Store: %s (local)\n", cfg.Store)

			// Create .bentoignore if it doesn't exist
			ignorePath := filepath.Join(dir, ".bentoignore")
			if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
				defaultIgnore := "# Bento ignore patterns\n# Files matching these patterns are excluded from all layers.\n\n"
				for _, p := range config.DefaultIgnorePatterns {
					defaultIgnore += p + "\n"
				}
				if err := os.WriteFile(ignorePath, []byte(defaultIgnore), 0644); err != nil {
					return fmt.Errorf("writing .bentoignore: %w", err)
				}
				fmt.Println("Created .bentoignore")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagTask, "task", "", "task description")

	return cmd
}
