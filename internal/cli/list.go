package cli

import (
	"fmt"
	"path/filepath"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List checkpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			projectName := filepath.Base(dir)
			storePath := filepath.Join(cfg.Store, projectName)
			store, err := registry.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			entries, err := store.ListCheckpoints()
			if err != nil {
				return fmt.Errorf("listing checkpoints: %w", err)
			}

			if len(entries) == 0 {
				fmt.Println("No checkpoints found. Run `bento save` to create one.")
				return nil
			}

			// Group tags by digest
			type group struct {
				tags    []string
				created string
				digest  string
				message string
			}
			digestOrder := []string{}
			groups := map[string]*group{}
			for _, e := range entries {
				if g, ok := groups[e.Digest]; ok {
					g.tags = append(g.tags, e.Tag)
				} else {
					digestOrder = append(digestOrder, e.Digest)
					groups[e.Digest] = &group{
						tags:    []string{e.Tag},
						created: e.Created,
						digest:  e.Digest,
						message: e.Message,
					}
				}
			}

			fmt.Printf("%-20s %-20s %-22s %s\n", "TAG", "CREATED", "DIGEST", "MESSAGE")
			for _, d := range digestOrder {
				g := groups[d]
				tags := ""
				for i, t := range g.tags {
					if i > 0 {
						tags += ", "
					}
					tags += t
				}
				digest := g.digest
				if len(digest) > 19 {
					digest = digest[:19] + "..."
				}
				fmt.Printf("%-20s %-20s %-22s %s\n", tags, g.created, digest, g.message)
			}

			return nil
		},
	}

	return cmd
}
