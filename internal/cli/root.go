package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/harness"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/spf13/cobra"
)

// Global flags
var (
	flagVerbose bool
	flagDir     string
)

// loadConfigAndStore is a helper that loads bento.yaml from the workspace
// directory and opens the local OCI store for the project.
func loadConfigAndStore(dir string) (*config.BentoConfig, registry.Store, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("no bento.yaml found. Run `bento init` first")
	}
	projectName := filepath.Base(dir)
	storePath := filepath.Join(cfg.Store, projectName)
	store, err := registry.NewStore(storePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening store: %w", err)
	}
	return cfg, store, nil
}

// resolveHarness returns the harness based on config.
// If layers are defined in config, use them directly.
// Otherwise, resolve by agent name (auto-detect or named).
func resolveHarness(dir string, cfg *config.BentoConfig) harness.Harness {
	if len(cfg.Layers) > 0 {
		return harness.NewConfigLayerHarness(cfg.Layers)
	}
	return harness.ResolveAgent(dir, cfg.Agent)
}

// diffFileMaps compares two maps of filename->hash, returning added, removed, and modified files.
func diffFileMaps(old, new map[string]string) (added, removed, modified []string) {
	for f, hash := range new {
		if _, ok := old[f]; !ok {
			added = append(added, f)
		} else if old[f] != hash {
			modified = append(modified, f)
		}
	}
	for f := range old {
		if _, ok := new[f]; !ok {
			removed = append(removed, f)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(modified)
	return
}

// fileLineCounts holds the number of added and removed lines for a single file.
type fileLineCounts struct{ added, removed int }

// countLineDiff computes the number of added and removed lines between old and
// new content using an LCS-based diff. Files larger than 50 000 lines fall back
// to a raw line-count delta to keep the operation fast.
func countLineDiff(oldContent, newContent []byte) (added, removed int) {
	splitLines := func(b []byte) []string {
		s := strings.TrimRight(string(b), "\n")
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	n, m := len(oldLines), len(newLines)
	if n == 0 {
		return m, 0
	}
	if m == 0 {
		return 0, n
	}
	// For very large files, use a fast approximation.
	if n > 50000 || m > 50000 {
		delta := m - n
		if delta > 0 {
			return delta, 0
		}
		return 0, -delta
	}
	// Standard O(n*m) space-optimised LCS DP.
	dp := make([]int, m+1)
	for i := 0; i < n; i++ {
		prev := 0
		for j := 0; j < m; j++ {
			temp := dp[j+1]
			if oldLines[i] == newLines[j] {
				dp[j+1] = prev + 1
			} else if dp[j] > dp[j+1] {
				dp[j+1] = dp[j]
			}
			prev = temp
		}
	}
	lcs := dp[m]
	return m - lcs, n - lcs
}

// looksLikeText returns false when content contains a null byte, which is a
// reliable indicator of binary data.
func looksLikeText(b []byte) bool {
	// Only scan the first 8 KiB for speed.
	probe := b
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return !strings.ContainsRune(string(probe), 0)
}

// lineCountAnnotation formats a per-file line change annotation such as
// "(+12/-5 lines)" or "(+8 lines)".
func lineCountAnnotation(lc fileLineCounts) string {
	if lc.added == 0 && lc.removed == 0 {
		return ""
	}
	var parts []string
	if lc.added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", lc.added))
	}
	if lc.removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d", lc.removed))
	}
	word := "lines"
	if lc.added+lc.removed == 1 {
		word = "line"
	}
	return fmt.Sprintf("(%s %s)", strings.Join(parts, "/"), word)
}

// printLayerDiff prints the diff for a single layer.
// lineCounts maps file path → line change counts; pass nil to omit annotations.
func printLayerDiff(name string, added, removed, modified []string, lineCounts map[string]fileLineCounts, hasChanges *bool) {
	if len(added) == 0 && len(removed) == 0 && len(modified) == 0 {
		fmt.Printf("\n  %s%s: unchanged%s\n", colorDim, name, colorReset)
		return
	}
	*hasChanges = true

	// Build a human-readable summary: "3 changes (1 added, 2 modified)"
	var parts []string
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("%s%d added%s", colorGreen, len(added), colorReset))
	}
	if len(removed) > 0 {
		parts = append(parts, fmt.Sprintf("%s%d removed%s", colorRed, len(removed), colorReset))
	}
	if len(modified) > 0 {
		parts = append(parts, fmt.Sprintf("%s%d modified%s", colorYellow, len(modified), colorReset))
	}
	total := len(added) + len(removed) + len(modified)
	word := "changes"
	if total == 1 {
		word = "change"
	}
	fmt.Printf("\n  %s: %d %s (%s)\n", name, total, word, strings.Join(parts, ", "))

	printFile := func(sigil, color, f string) {
		ann := ""
		if lineCounts != nil {
			ann = lineCountAnnotation(lineCounts[f])
		}
		if ann != "" {
			fmt.Printf("    %s%s %s  %s%s%s\n", color, sigil, f, colorDim, ann, colorReset)
		} else {
			fmt.Printf("    %s%s %s%s\n", color, sigil, f, colorReset)
		}
	}
	for _, f := range added {
		printFile("+", colorGreen, f)
	}
	for _, f := range removed {
		printFile("-", colorRed, f)
	}
	for _, f := range modified {
		printFile("~", colorYellow, f)
	}
}

// NewRootCmd creates the root bento command.
func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "bento",
		Short: "Portable agent workspaces. Pack, ship, resume.",
		Long:  "Bento packages AI agent workspace state into portable, layered OCI artifacts.",
		Version: version,
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&flagDir, "dir", ".", "workspace directory")

	rootCmd.AddCommand(
		newInitCmd(),
		newSaveCmd(),
		newOpenCmd(),
		newListCmd(),
		newInspectCmd(),
		newDiffCmd(),
		newForkCmd(),
		newTagCmd(),
		newPushCmd(),
		newGCCmd(),
		newEnvCmd(),
	)

	return rootCmd
}
