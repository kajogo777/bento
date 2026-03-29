package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/workspace"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <layer> <path>",
		Short: "Add a file to a layer's tracking patterns",
		Long: `Adds a file path as a pattern to the specified layer in bento.yaml.

If bento.yaml has no custom layers section, the detected extension layers are
written to bento.yaml first (so you can customize them), then the pattern is
added to the target layer.

Since layers are matched in order (first match wins), adding a file to a layer
that appears before the catch-all will move it out of the catch-all.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			layerName := args[0]
			filePath := workspace.NormalizePath(args[1])

			// Validate the file exists
			absPath := filepath.Join(dir, filepath.FromSlash(filePath))
			if _, err := os.Stat(absPath); err != nil {
				return fmt.Errorf("file not found: %s", filePath)
			}

			// If no custom layers in config, populate from extensions
			if len(cfg.Layers) == 0 {
				resolved := resolveExtensions(dir, cfg)
				cfg.Layers = layerDefsToConfig(resolved.Layers)
				fmt.Println("Initialized layers in bento.yaml from detected extensions.")
			}

			// Find target layer
			targetIdx := -1
			for i, l := range cfg.Layers {
				if l.Name == layerName {
					targetIdx = i
					break
				}
			}
			if targetIdx == -1 {
				return fmt.Errorf("layer %q not found. Available layers: %s", layerName, layerNames(cfg.Layers))
			}

			// Check if already explicitly matched in the target layer
			for _, p := range cfg.Layers[targetIdx].Patterns {
				if p == filePath {
					fmt.Printf("Already in layer %q: pattern %q exists\n", layerName, filePath)
					return nil
				}
				if workspace.MatchesPattern(p, filePath) {
					fmt.Printf("Already in layer %q: %s matches pattern %q\n", layerName, filePath, p)
					return nil
				}
			}

			cfg.Layers[targetIdx].Patterns = append(cfg.Layers[targetIdx].Patterns, filePath)

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Added %s to layer %q\n", filePath, cfg.Layers[targetIdx].Name)
			return nil
		},
	}

	return cmd
}

// layerDefsToConfig converts extension LayerDefs to config LayerConfigs,
// filtering out external patterns (~/... or /...) since those are
// machine-specific and shouldn't be persisted in bento.yaml.
func layerDefsToConfig(defs []extension.LayerDef) []config.LayerConfig {
	layers := make([]config.LayerConfig, 0, len(defs))
	for _, ld := range defs {
		var patterns []string
		for _, p := range ld.Patterns {
			if !extension.IsExternalPattern(p) {
				patterns = append(patterns, p)
			}
		}
		layers = append(layers, config.LayerConfig{
			Name:     ld.Name,
			Patterns: patterns,
			CatchAll: ld.CatchAll,
		})
	}
	return layers
}

// layerNames returns a comma-separated list of layer names for error messages.
func layerNames(layers []config.LayerConfig) string {
	result := ""
	for i, l := range layers {
		if i > 0 {
			result += ", "
		}
		result += l.Name
	}
	return result
}
