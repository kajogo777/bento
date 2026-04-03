package cli

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/kajogo777/bento/internal/tui"
	"github.com/spf13/cobra"
)

func newExploreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explore [ref]",
		Short: "Interactively explore checkpoint contents",
		Long: `Launch a TUI to browse checkpoints, layers, and files.

Works with local refs:
  bento explore                    # explore all local checkpoints
  bento explore cp-3               # start at a specific checkpoint

Navigation:
  up/dn      Navigate tree
  enter      Expand/collapse or preview file
  /          Filter files by path
  pgup/pgdn  Scroll preview
  q          Quit`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}

			source, initialTag, err := tui.NewArtifactSource(ref, dir)
			if err != nil {
				return fmt.Errorf("opening artifact source: %w", err)
			}
			defer func() { _ = source.Close() }()

			_ = initialTag // TODO: use to pre-select checkpoint

			model := tui.NewModel(source, initialTag)
			p := tea.NewProgram(model)

			if _, err := p.Run(); err != nil {
				return fmt.Errorf("TUI error: %w", err)
			}

			return nil
		},
	}

	return cmd
}
