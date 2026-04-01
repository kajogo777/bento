package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newSaveCmd() *cobra.Command {
	var (
		flagMessage              string
		flagTag                  string
		flagSkipSecretScan       bool
		flagAllowMissingExternal bool
	)

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a checkpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			result, err := ExecuteSave(SaveOptions{
				Dir:                  dir,
				Message:              flagMessage,
				Tag:                  flagTag,
				SkipSecretScan:       flagSkipSecretScan,
				AllowMissingExternal: flagAllowMissingExternal,
			})
			if err != nil {
				return err
			}

			if result.Skipped {
				fmt.Printf("No changes detected, skipping checkpoint.\n")
				return nil
			}

			fmt.Printf("Tagged: %s, latest\n", result.Tag)

			if result.Hint != "" {
				fmt.Printf("\nHint: %s\n", result.Hint)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "checkpoint message")
	cmd.Flags().StringVar(&flagTag, "tag", "", "custom tag for this checkpoint")
	cmd.Flags().BoolVar(&flagSkipSecretScan, "skip-secret-scan", false, "skip secret scanning")
	cmd.Flags().BoolVar(&flagAllowMissingExternal, "allow-missing-external", false, "warn instead of error when external agent session files are inaccessible")

	return cmd
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
