package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/secrets"
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
				fmt.Println("\nEnv file templates:")
				for path, ef := range cfg.EnvFiles {
					fmt.Printf("  %s -> %s\n", path, ef.Template)
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

	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Print resolved env vars and secrets as shell exports",
		Long: `Resolve all environment variables and secrets, then print them as
shell export statements. Use with eval to load into your shell:

  eval $(bento env export)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			exports := make(map[string]string)

			// Plain env vars
			for k, v := range cfg.Env {
				exports[k] = v
			}

			// Resolve secrets
			if len(cfg.Secrets) > 0 {
				ctx := context.Background()
				resolved, errs := secrets.HydrateSecrets(ctx, cfg.Secrets)
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "# warning: %v\n", e)
				}
				for k, v := range resolved {
					exports[k] = v
				}
			}

			// Print sorted for deterministic output
			keys := make([]string, 0, len(exports))
			for k := range exports {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				// Shell-escape single quotes in values
				v := strings.ReplaceAll(exports[k], "'", "'\\''")
				fmt.Printf("export %s='%s'\n", k, v)
			}

			return nil
		},
	}

	envCmd.AddCommand(showCmd, setCmd, exportCmd)
	return envCmd
}
