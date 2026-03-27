package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/harness"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		flagTask    string
		flagHarness string
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

			// Detect harness
			var h harness.Harness
			if flagHarness != "" && flagHarness != "auto" {
				h = harness.DetectSingle(dir, flagHarness)
				fmt.Printf("Using agent: %s\n", h.Name())
			} else {
				h = harness.Detect(dir)
				fmt.Printf("Detected agent: %s\n", h.Name())
			}

			// Build config
			cfg := &config.BentoConfig{
				Store:   config.DefaultStorePath(),
				Harness: h.Name(),
				Task:    flagTask,
				Ignore:  []string{"*.log", "tmp/", ".DS_Store"},
			}

			// Write bento.yaml
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
	cmd.Flags().StringVar(&flagHarness, "harness", "auto", "harness name (auto-detect if not set)")

	return cmd
}
