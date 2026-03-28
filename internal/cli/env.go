package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables and secrets",
	}

	envCmd.AddCommand(newEnvSetCmd(), newEnvUnsetCmd(), newEnvShowCmd(), newEnvExportCmd())
	return envCmd
}

// newEnvSetCmd creates the "bento env set" command.
// Plain env var:    bento env set KEY value
// Secret ref:       bento env set KEY --source env --var VAR_NAME
func newEnvSetCmd() *cobra.Command {
	var (
		flagSource  string
		flagVar     string
		flagPath    string
		flagKey     string
		flagRole    string
		flagCommand string
	)

	cmd := &cobra.Command{
		Use:   "set <name> [value]",
		Short: "Set an env var or secret reference",
		Long: `Set a plain environment variable or a secret reference.

Plain env var (value as positional argument):
  bento env set NODE_ENV development

Secret reference (use --source flag):
  bento env set DATABASE_URL --source env --var DATABASE_URL
  bento env set API_KEY --source file --path /run/secrets/api-key
  bento env set TOKEN --source exec --command "vault read -field=token secret/app"

When --source is provided, the name is stored as a secret reference (only the
reference is saved, never the value). Without --source, it is stored as a plain
environment variable.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			isSecret := flagSource != ""

			// Validate argument combinations.
			if !isSecret && len(args) < 2 {
				return fmt.Errorf("provide a value or use --source to set a secret reference")
			}
			if isSecret && len(args) == 2 {
				return fmt.Errorf("cannot combine a literal value with --source; use either a positional value or --source flags")
			}

			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			if cfg.Env == nil {
				cfg.Env = make(map[string]config.EnvEntry)
			}

			if isSecret {
				// Build fields map from flags.
				fields := make(map[string]string)
				if flagVar != "" {
					fields["var"] = flagVar
				}
				if flagPath != "" {
					fields["path"] = flagPath
				}
				if flagKey != "" {
					fields["key"] = flagKey
				}
				if flagRole != "" {
					fields["role"] = flagRole
				}
				if flagCommand != "" {
					fields["command"] = flagCommand
				}
				if len(fields) == 0 {
					return fmt.Errorf("secret source %q requires at least one field flag (--var, --path, --key, --role, or --command)", flagSource)
				}

				cfg.Env[name] = config.NewSecretEnv(flagSource, fields)
			} else {
				cfg.Env[name] = config.NewLiteralEnv(args[1])
			}

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if isSecret {
				fmt.Printf("Set secret %s (source: %s)\n", name, flagSource)
			} else {
				fmt.Printf("Set %s=%s\n", name, args[1])
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagSource, "source", "", "secret provider (env, file, exec, vault, aws-sts, 1password, gcloud, azure)")
	cmd.Flags().StringVar(&flagVar, "var", "", "environment variable name (source=env)")
	cmd.Flags().StringVar(&flagPath, "path", "", "file path (source=file) or vault path (source=vault)")
	cmd.Flags().StringVar(&flagKey, "key", "", "field key within the secret")
	cmd.Flags().StringVar(&flagRole, "role", "", "IAM role ARN (source=aws-sts)")
	cmd.Flags().StringVar(&flagCommand, "command", "", "shell command (source=exec)")

	return cmd
}

// newEnvUnsetCmd creates the "bento env unset" command.
func newEnvUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <name>",
		Short: "Remove an env var or secret reference",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			entry, exists := cfg.Env[name]
			if !exists {
				return fmt.Errorf("%s is not configured", name)
			}

			delete(cfg.Env, name)

			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if entry.IsRef {
				fmt.Printf("Removed secret %s\n", name)
			} else {
				fmt.Printf("Removed %s\n", name)
			}
			return nil
		},
	}
}

// newEnvShowCmd creates the "bento env show" command.
func newEnvShowCmd() *cobra.Command {
	var (
		flagResolve bool
		flagReveal  bool
	)

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show configured env vars and secret references",
		Long: `Display all configured environment variables and secret references.

By default, secrets are shown as references (source + fields) without resolving.
Use --resolve to attempt resolving all entries and display masked values.
Use --reveal to show resolved values in cleartext.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			if len(cfg.Env) == 0 {
				fmt.Println("No environment variables configured.")
				return nil
			}

			// --reveal implies --resolve.
			if flagReveal {
				flagResolve = true
			}

			// Resolve if requested.
			var resolved map[string]string
			var resolveErrs []error
			if flagResolve {
				ctx := context.Background()
				resolved, resolveErrs = secrets.HydrateEnv(ctx, cfg.Env)
			}

			fmt.Println("Environment:")
			for _, name := range sortedEnvKeys(cfg.Env) {
				entry := cfg.Env[name]
				if entry.IsRef {
					ref := formatSecretRef(entry)
					if flagResolve {
						if val, ok := resolved[name]; ok {
							if flagReveal {
								fmt.Printf("  %s (%s) = %s\n", name, ref, val)
							} else {
								fmt.Printf("  %s (%s) = %s\n", name, ref, maskValue(val))
							}
						} else {
							fmt.Printf("  %s (%s) = <failed to resolve>\n", name, ref)
						}
					} else {
						fmt.Printf("  %s (%s)\n", name, ref)
					}
				} else {
					fmt.Printf("  %s=%s\n", name, entry.Value)
				}
			}

			// Print resolve errors at the end.
			for _, e := range resolveErrs {
				fmt.Printf("\nWarning: %v\n", e)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&flagResolve, "resolve", false, "resolve secret references and display masked values")
	cmd.Flags().BoolVar(&flagReveal, "reveal", false, "resolve and display values in cleartext (implies --resolve)")

	return cmd
}

// newEnvExportCmd creates the "bento env export" command.
func newEnvExportCmd() *cobra.Command {
	var (
		flagOutput   string
		flagTemplate string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Resolve all env vars and secrets and export as a .env file",
		Long: `Resolve all configured environment variables and secrets, then output
them in .env format.

By default, output goes to stdout. Use -o to write to a file (created with
0600 permissions). Use --template to base the output on an existing .env
template file.

Examples:
  bento env export                       # print to stdout
  bento env export -o .env               # write to .env file
  bento env export -o .env --template .env.example`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			if len(cfg.Env) == 0 {
				fmt.Fprintln(os.Stderr, "No environment variables to export.")
				return nil
			}

			ctx := context.Background()
			allVars, errs := secrets.HydrateEnv(ctx, cfg.Env)
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
			}

			if len(allVars) == 0 {
				fmt.Fprintln(os.Stderr, "No variables resolved successfully.")
				return nil
			}

			// If writing to a file, use PopulateEnvFile (handles templates).
			if flagOutput != "" {
				outputPath := filepath.Join(dir, flagOutput)
				if !isInsideDir(dir, outputPath) {
					return fmt.Errorf("output path %q escapes workspace directory", flagOutput)
				}

				templatePath := ""
				if flagTemplate != "" {
					templatePath = filepath.Join(dir, flagTemplate)
					if !isInsideDir(dir, templatePath) {
						return fmt.Errorf("template path %q escapes workspace directory", flagTemplate)
					}
				}

				if err := secrets.PopulateEnvFile(templatePath, outputPath, allVars); err != nil {
					return fmt.Errorf("writing %s: %w", flagOutput, err)
				}
				fmt.Fprintf(os.Stderr, "Exported %d variables to %s\n", len(allVars), flagOutput)
				return nil
			}

			// Stdout mode: generate sorted KEY=VALUE lines.
			keys := sortedKeys(allVars)
			for _, k := range keys {
				fmt.Printf("%s=%s\n", k, allVars[k])
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "output file path (default: stdout)")
	cmd.Flags().StringVar(&flagTemplate, "template", "", "template .env file to use as base")

	return cmd
}

// --- helpers ---

// formatSecretRef returns a human-readable description of a secret reference.
func formatSecretRef(e config.EnvEntry) string {
	parts := []string{e.Source}
	if v, ok := e.Fields["var"]; ok {
		parts = append(parts, fmt.Sprintf("var=%s", v))
	}
	if v, ok := e.Fields["path"]; ok {
		parts = append(parts, fmt.Sprintf("path=%s", v))
	}
	if v, ok := e.Fields["key"]; ok {
		parts = append(parts, fmt.Sprintf("key=%s", v))
	}
	if v, ok := e.Fields["role"]; ok {
		parts = append(parts, fmt.Sprintf("role=%s", v))
	}
	if v, ok := e.Fields["command"]; ok {
		parts = append(parts, fmt.Sprintf("command=%q", v))
	}
	return strings.Join(parts, ", ")
}

// maskValue masks a secret value, showing only the first and last characters.
func maskValue(v string) string {
	runes := []rune(v)
	if len(runes) <= 4 {
		return "****"
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1])
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedEnvKeys returns env entry map keys in sorted order.
func sortedEnvKeys(m map[string]config.EnvEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isInsideDir checks that resolved is a path inside base (no directory traversal).
func isInsideDir(base, resolved string) bool {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) || absResolved == absBase
}
