package cli

import (
	"fmt"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables and secrets",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show tracked env vars and secret refs",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			if len(cfg.Env) > 0 {
				fmt.Println("Environment variables:")
				for k, v := range cfg.Env {
					fmt.Printf("  %s=%s\n", k, v)
				}
			}

			if len(cfg.Secrets) > 0 {
				fmt.Println("\nSecret references:")
				for k, s := range cfg.Secrets {
					fmt.Printf("  %s (source: %s)\n", k, s.Source)
				}
			}

			if len(cfg.EnvFiles) > 0 {
				fmt.Println("\nEnv files:")
				for path, ef := range cfg.EnvFiles {
					if ef.Template != "" {
						fmt.Printf("  %s (from %s)\n", path, ef.Template)
					} else {
						fmt.Printf("  %s\n", path)
					}
				}
			}

			if len(cfg.Env) == 0 && len(cfg.Secrets) == 0 && len(cfg.EnvFiles) == 0 {
				fmt.Println("No environment variables, secrets, or env files configured.")
			}

			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set an env var",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			if cfg.Env == nil {
				cfg.Env = make(map[string]string)
			}
			cfg.Env[args[0]] = args[1]

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Set %s=%s\n", args[0], args[1])
			return nil
		},
	}

	envCmd.AddCommand(showCmd, setCmd)
	return envCmd
}
