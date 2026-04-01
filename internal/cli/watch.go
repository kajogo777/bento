package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/policy"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/kajogo777/bento/internal/watcher"
	"github.com/kajogo777/bento/internal/workspace"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	var (
		flagDebounce             int
		flagMessage              string
		flagSkipSecretScan       bool
		flagAllowMissingExternal bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch workspace and auto-save checkpoints on changes",
		Long: `Starts a file-system watcher that creates checkpoints when files change.
Uses the same logic as 'bento save'. Runs until Ctrl-C.

Layers with watch: realtime are monitored instantly via fsnotify.
Layers with watch: periodic are checked every ~30 seconds.
Layers with watch: off are not monitored (still included in saves).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("loading bento.yaml: %w", err)
			}

			// Resolve extensions and layers.
			resolved := resolveExtensions(dir, cfg)
			layers := resolved.Layers

			// Collect ignore patterns.
			ignorePatterns := append(config.DefaultIgnorePatterns, resolved.Ignore...)
			ignorePatterns = append(ignorePatterns, cfg.Ignore...)
			if bentoIgnore, err := workspace.LoadBentoIgnore(dir); err == nil {
				ignorePatterns = append(ignorePatterns, bentoIgnore...)
			}

			// Resolve debounce.
			debounce := 10 // default
			if flagDebounce > 0 {
				debounce = flagDebounce
			} else if cfg.Watch.Debounce > 0 {
				debounce = cfg.Watch.Debounce
			}

			// Resolve message.
			message := "auto-save"
			if flagMessage != "" {
				message = flagMessage
			} else if cfg.Watch.Message != "" {
				message = cfg.Watch.Message
			}

			// Resolve skip secret scan.
			skipSecretScan := flagSkipSecretScan || cfg.Watch.SkipSecretScan

			// Pre-flight: if secrets.mode is not configured, do a quick scan
			// at startup to prompt the user before entering the watch loop.
			// This prevents the first auto-save from failing silently in quiet mode.
			secretsMode := cfg.Secrets.Mode
			if skipSecretScan {
				secretsMode = config.SecretsModeOff
			}
			if secretsMode == "" {
				fmt.Println("Running pre-flight secret scan...")
				if preScanner, scanErr := secrets.NewSecretScanner(nil); scanErr == nil {
					preScanner.SetBaseDir(dir)

					// Load .gitleaksignore if present.
					ignorePath := filepath.Join(dir, ".gitleaksignore")
					if _, statErr := os.Stat(ignorePath); statErr == nil {
						_ = preScanner.LoadGitleaksIgnore(ignorePath)
					}

					// Collect workspace files from all layers.
					scanner := workspace.NewScanner(dir, layers, ignorePatterns)
					if scanResults, scanErr := scanner.Scan(); scanErr == nil {
						var allFiles []string
						for _, sr := range scanResults {
							for _, f := range sr.WorkspaceFiles {
								allFiles = append(allFiles, filepath.Join(dir, f))
							}
						}
						if hits, scanErr := preScanner.ScanFiles(allFiles); scanErr == nil && len(hits) > 0 {
							// Secrets found and mode not configured — prompt now.
							chosen := promptSecretsMode(hits, false)
							cfg.Secrets.Mode = chosen
							if saveErr := config.Save(dir, cfg); saveErr != nil {
								fmt.Printf("Warning: could not persist secrets.mode to bento.yaml: %v\n", saveErr)
							} else {
								fmt.Printf("Saved secrets.mode: %s in bento.yaml\n", chosen)
							}
							// Update local state for the save callback.
							secretsMode = chosen
							if chosen == config.SecretsModeBlock {
								return fmt.Errorf("secrets.mode is block — resolve secrets before running watch")
							}
							if chosen == config.SecretsModeOff {
								skipSecretScan = true
							}
						} else if scanErr == nil {
							fmt.Println("Pre-flight secret scan: clean")
						}
					}
				}
			}

			// Open store for tiered GC.
			store, err := registry.NewStore(cfg.StorePath())
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Resolve retention tiers.
			tiers := cfg.Retention.Tiers
			if len(tiers) == 0 {
				tiers = policy.DefaultWatchTiers
			}

			// Build the save callback — called by the watcher on debounce fire.
			saveCount := 0
			saveFunc := func() error {
				saveCount++
				result, err := ExecuteSave(SaveOptions{
					Dir:                  dir,
					Message:              message,
					SkipSecretScan:       skipSecretScan,
					AllowMissingExternal: flagAllowMissingExternal,
					Quiet:                true,
				})
				if err != nil {
					return err
				}
				if result.Skipped {
					fmt.Printf("⏭ no changes, skipping checkpoint\n")
					return nil
				}

				fmt.Printf("OK %s (checkpoint %s)\n", result.Tag, time.Now().Format("15:04:05"))

				// Run tiered GC after successful save.
				if len(tiers) > 0 {
					deleted, gcErr := policy.TieredGC(store, tiers, true)
					if gcErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: auto-gc failed: %v\n", gcErr)
					} else if len(deleted) > 0 {
						fmt.Printf("  gc: pruned %d old checkpoint(s)\n", len(deleted))
					}
				}

				return nil
			}

			// Print startup banner.
			printWatchBanner(dir, layers, debounce)

			// Create and start watcher.
			w, err := watcher.New(watcher.Config{
				WorkDir:          dir,
				DebounceDuration: time.Duration(debounce) * time.Second,
				Layers:           layers,
				IgnorePatterns:   ignorePatterns,
				SaveFunc:         saveFunc,
			})
			if err != nil {
				return fmt.Errorf("starting watcher: %w", err)
			}

			// Signal handling: Ctrl-C / SIGTERM.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			fmt.Println("Watching for changes... (Ctrl-C to stop)")

			if err := w.Run(ctx); err != nil {
				return fmt.Errorf("watcher error: %w", err)
			}

			fmt.Printf("\nStopped. %d checkpoint(s) created.\n", saveCount)
			return nil
		},
	}

	cmd.Flags().IntVar(&flagDebounce, "debounce", 0, "seconds of quiet before saving (default: 10)")
	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "checkpoint message (default: \"auto-save\")")
	cmd.Flags().BoolVar(&flagSkipSecretScan, "skip-secret-scan", false, "skip secret scanning on auto-saves")
	cmd.Flags().BoolVar(&flagAllowMissingExternal, "allow-missing-external", false, "warn instead of error when external files are inaccessible")

	return cmd
}

// printWatchBanner prints a summary of the watch configuration at startup.
func printWatchBanner(dir string, layers []extension.LayerDef, debounce int) {
	fmt.Printf("Workspace: %s\n", dir)
	fmt.Printf("Debounce:  %ds\n", debounce)
	fmt.Println("Layers:")
	for _, l := range layers {
		method := l.WatchMethod
		if method == "" {
			method = "realtime"
		}
		detail := ""
		if method == extension.WatchPeriodic {
			var dirs []string
			for _, p := range l.Patterns {
				d := strings.TrimSuffix(p, "/**")
				if d != p && !strings.Contains(d, "*") {
					dirs = append(dirs, d)
				}
			}
			if len(dirs) > 0 {
				detail = fmt.Sprintf(" [%s]", strings.Join(dirs, ", "))
			}
		}
		fmt.Printf("  %-10s %s%s\n", l.Name+":", method, detail)
	}
}
