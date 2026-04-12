package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show workspace status and remote sync state",
		Long: `Show the current workspace state: head checkpoint, active extensions,
configured remote, and whether the remote has newer checkpoints.

This is a quick orientation command — use 'bento diff' for detailed
file-level changes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("no bento.yaml found. Run `bento init` first")
			}

			store, err := registry.NewStore(cfg.StorePath())
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			// Workspace info
			fmt.Printf("Workspace: %s\n", dir)
			if cfg.Task != "" {
				fmt.Printf("Task:      %s\n", cfg.Task)
			}

			// Extensions
			exts := extension.Resolve(dir, cfg.Extensions)
			if len(exts) > 0 {
				names := make([]string, len(exts))
				for i, e := range exts {
					names[i] = e.Name()
				}
				fmt.Printf("Extensions: %s\n", strings.Join(names, ", "))
			}

			// Head checkpoint info
			headRef := cfg.Head
			if headRef == "" {
				fmt.Println("\nHead:      (none — run `bento save` to create first checkpoint)")
				return nil
			}

			manifestBytes, _, err := store.LoadManifest(headRef)
			if err != nil {
				fmt.Printf("\nHead:      %s (unable to load)\n", truncateDigest(headRef))
			} else {
				info, parseErr := manifest.ParseCheckpointInfo(manifestBytes)
				if parseErr != nil {
					fmt.Printf("\nHead:      %s\n", truncateDigest(headRef))
				} else {
					tag := fmt.Sprintf("cp-%d", info.Sequence)
					ago := timeAgo(info.Created)
					fmt.Printf("\nHead:      %s", tag)
					if ago != "" {
						fmt.Printf(" (saved %s)", ago)
					}
					fmt.Println()
					if info.Message != "" {
						fmt.Printf("Message:   %s\n", info.Message)
					}
				}
			}

			// Local checkpoint count
			entries, listErr := store.ListCheckpoints()
			if listErr == nil && len(entries) > 0 {
				// Count unique digests (tags are grouped)
				digests := make(map[string]bool)
				for _, e := range entries {
					digests[e.Digest] = true
				}
				fmt.Printf("Local:     %d checkpoint(s)\n", len(digests))
			}

			// Remote sync state
			if cfg.Remote == "" {
				fmt.Println("\nRemote:    (none)")
			} else {
				fmt.Printf("\nRemote:    %s\n", cfg.Remote)

				// Determine head sequence for comparison.
				headSeq := 0
				if manifestBytes != nil {
					if hi, err := manifest.ParseCheckpointInfo(manifestBytes); err == nil {
						headSeq = hi.Sequence
					}
				}

				localMax := maxCheckpointSeq(entries)

				ctx := context.Background()
				remoteTags, remoteErr := registry.ListRemoteTags(ctx, cfg.Remote)
				if remoteErr != nil {
					fmt.Printf("  Sync:    unable to reach remote (%v)\n", remoteErr)
				} else {
					remoteMax := maxCpTagSeq(remoteTags)

					// Show remote vs head comparison.
					if remoteMax == 0 && headSeq == 0 {
						fmt.Println("  Sync:    both empty")
					} else if remoteMax > headSeq {
						behind := remoteMax - headSeq
						noun := "checkpoint"
						if behind > 1 {
							noun = "checkpoints"
						}
						fmt.Printf("  Sync:    %s%d %s behind%s (remote has cp-%d, head is cp-%d)\n",
							colorYellow, behind, noun, colorReset, remoteMax, headSeq)
						if localMax >= remoteMax {
							fmt.Println("           run `bento open latest` to restore")
						} else {
							fmt.Println("           run `bento pull` to sync, then `bento open latest`")
						}
					} else if headSeq > remoteMax {
						ahead := headSeq - remoteMax
						noun := "checkpoint"
						if ahead > 1 {
							noun = "checkpoints"
						}
						fmt.Printf("  Sync:    %s%d %s ahead%s (head is cp-%d, remote has cp-%d)\n",
							colorGreen, ahead, noun, colorReset, headSeq, remoteMax)
						fmt.Println("           run `bento push` to sync")
					} else {
						fmt.Printf("  Sync:    %sup to date%s (cp-%d)\n", colorGreen, colorReset, headSeq)
					}
				}
			}

			// Workspace changes since last save (lightweight summary)
			if headRef != "" {
				changed, summary := quickDiffSummary(dir, cfg, store)
				if changed {
					fmt.Printf("\nChanges:   %s%s%s since last save\n", colorYellow, summary, colorReset)
					fmt.Println("           run `bento diff` for details")
				} else if summary != "" {
					// summary contains error info
					fmt.Printf("\nChanges:   %s\n", summary)
				} else {
					fmt.Printf("\nChanges:   %sclean%s\n", colorGreen, colorReset)
				}
			}

			return nil
		},
	}

	return cmd
}

// timeAgo formats an RFC3339 timestamp as a human-readable relative time.
func timeAgo(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// maxCheckpointSeq finds the highest cp-N sequence number from local checkpoint entries.
func maxCheckpointSeq(entries []CheckpointEntry) int {
	max := 0
	for _, e := range entries {
		if seq := parseCpSeq(e.Tag); seq > max {
			max = seq
		}
	}
	return max
}

// CheckpointEntry re-export for this file (same type from registry package).
type CheckpointEntry = registry.CheckpointEntry

// maxCpTagSeq finds the highest cp-N sequence number from a list of tags.
func maxCpTagSeq(tags []string) int {
	max := 0
	for _, tag := range tags {
		if seq := parseCpSeq(tag); seq > max {
			max = seq
		}
	}
	return max
}

// parseCpSeq extracts the sequence number from a "cp-N" tag. Returns 0 if not a cp- tag.
func parseCpSeq(tag string) int {
	if !strings.HasPrefix(tag, "cp-") {
		return 0
	}
	n := 0
	fmt.Sscanf(tag[3:], "%d", &n)
	return n
}

// quickDiffSummary does a lightweight check of workspace changes since the last save.
// Returns (hasChanges, summary). If an error occurs, hasChanges is false and summary
// contains the error description.
func quickDiffSummary(dir string, cfg *config.BentoConfig, store registry.Store) (bool, string) {
	ref := cfg.Head
	if ref == "" {
		ref = "latest"
	}

	_, _, layers, err := store.LoadCheckpoint(ref)
	if err != nil {
		return false, "unable to load last checkpoint"
	}
	defer func() {
		for i := range layers {
			layers[i].Cleanup()
		}
	}()

	// Resolve extensions for scanning
	var layerDefs []extension.LayerDef
	if len(cfg.Layers) > 0 {
		layerDefs = configToLayerDefs(cfg.Layers)
	} else {
		exts := extension.Resolve(dir, cfg.Extensions)
		var contributions []extension.Contribution
		for _, ext := range exts {
			contributions = append(contributions, ext.Contribute(dir))
		}
		merged := extension.Merge(contributions)
		layerDefs = merged.Layers
	}

	ignorePatterns := append(config.DefaultIgnorePatterns, cfg.Ignore...)
	scanner := workspace.NewScanner(dir, layerDefs, ignorePatterns, nil)
	scanResults, err := scanner.Scan()
	if err != nil {
		return false, "unable to scan workspace"
	}

	// Compare layer-by-layer using file hashes
	totalAdded, totalRemoved, totalModified := 0, 0, 0

	for i, layer := range layers {
		layerName := ""
		if i < len(layerDefs) {
			layerName = layerDefs[i].Name
		}

		sr, ok := scanResults[layerName]
		if !ok {
			continue
		}

		r, err := layer.NewReader()
		if err != nil {
			continue
		}
		savedHashes, _ := workspace.ListLayerFilesWithHashesFromReader(r)
		_ = r.Close()

		currentHashes := make(map[string]string)
		for _, f := range sr.WorkspaceFiles {
			hash, err := workspace.HashFileStreaming(filepath.Join(dir, f))
			if err == nil {
				currentHashes[f] = hash
			}
		}
		for _, ef := range sr.ExternalFiles {
			hash, err := workspace.HashFileStreaming(ef.AbsPath)
			if err == nil {
				currentHashes[workspace.DisplayPath(ef.ArchivePath)] = hash
			}
		}

		added, removed, modified := diffFileMaps(savedHashes, currentHashes)
		totalAdded += len(added)
		totalRemoved += len(removed)
		totalModified += len(modified)
	}

	if totalAdded == 0 && totalRemoved == 0 && totalModified == 0 {
		return false, ""
	}

	var parts []string
	if totalAdded > 0 {
		parts = append(parts, fmt.Sprintf("%d added", totalAdded))
	}
	if totalRemoved > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", totalRemoved))
	}
	if totalModified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", totalModified))
	}

	total := totalAdded + totalRemoved + totalModified
	word := "files"
	if total == 1 {
		word = "file"
	}
	return true, fmt.Sprintf("%d %s changed (%s)", total, word, strings.Join(parts, ", "))
}


