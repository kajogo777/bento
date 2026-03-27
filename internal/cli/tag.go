package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag <ref> <new-tag>",
		Short: "Tag a checkpoint",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			_, store, err := loadConfigAndStore(dir)
			if err != nil {
				return err
			}

			_, tag, err := registry.ParseRef(args[0])
			if err != nil {
				return err
			}

			digest, err := store.ResolveTag(tag)
			if err != nil {
				return fmt.Errorf("checkpoint '%s' not found", tag)
			}

			if err := store.Tag(digest, args[1]); err != nil {
				return fmt.Errorf("tagging: %w", err)
			}

			fmt.Printf("Tagged %s as %s\n", tag, args[1])
			return nil
		},
	}

	return cmd
}
