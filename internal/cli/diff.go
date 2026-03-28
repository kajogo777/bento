package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// maxLineDiffBytes is the per-file content cap passed to ExtractFilesFromLayer.
// Files larger than this are treated as binary and shown without line counts.
const maxLineDiffBytes = 2 << 20 // 2 MiB

var (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[2m"
)

func init() {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		colorReset = ""
		colorRed = ""
		colorGreen = ""
		colorYellow = ""
		colorDim = ""
	}
}

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [ref1] [ref2]",
		Short: "Show changes since last checkpoint, or compare two checkpoints",
		Long: `With no arguments, shows what changed in the workspace since the last save.
With one argument, compares that checkpoint to the current workspace.
With two arguments, compares two checkpoints.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			if len(args) <= 1 {
				return diffWorkspace(dir, args)
			}
			return diffCheckpoints(dir, args)
		},
	}

	return cmd
}

func diffWorkspace(dir string, args []string) error {
	cfg, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

	ref := "latest"
	if len(args) == 1 {
		ref = args[0]
	}
	_, tag, _ := registry.ParseRef(ref)

	manifestBytes, _, layers, err := store.LoadCheckpoint(tag)
	if err != nil {
		return fmt.Errorf("no checkpoints found. Run `bento save` first")
	}
	defer func() {
		for i := range layers {
			layers[i].Cleanup()
		}
	}()

	var m ocispec.Manifest
	_ = json.Unmarshal(manifestBytes, &m)

	h := resolveHarness(dir, cfg)
	layerDefs := h.Layers(dir)

	ignorePatterns := append(config.DefaultIgnorePatterns, h.Ignore()...)
	ignorePatterns = append(ignorePatterns, cfg.Ignore...)
	if bentoIgnore, err := workspace.LoadBentoIgnore(dir); err == nil {
		ignorePatterns = append(ignorePatterns, bentoIgnore...)
	}

	scanner := workspace.NewScanner(dir, layerDefs, ignorePatterns)
	scanResults, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("scanning workspace: %w", err)
	}

	fmt.Printf("Comparing workspace → %s\n", tag)

	hasChanges := false
	for i, ld := range layerDefs {
		if i >= len(layers) {
			continue
		}

		// Stream the saved layer to compute hashes (avoids loading GBs into memory)
		r, err := layers[i].NewReader()
		if err != nil {
			continue
		}
		savedHashes, _ := workspace.ListLayerFilesWithHashesFromReader(r)
		_ = r.Close()

		sr := scanResults[ld.Name]
		currentHashes := make(map[string]string)

		// Hash workspace files by streaming, not loading full content into memory
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

		// Compute line counts for added/removed/modified files.
		lineCounts := computeWorkspaceLineCounts(layers[i], added, removed, modified, dir, sr)
		printLayerDiff(ld.Name, added, removed, modified, lineCounts, &hasChanges)
	}

	if !hasChanges {
		fmt.Println("\nNo changes since last checkpoint.")
	}

	return nil
}

func diffCheckpoints(dir string, args []string) error {
	_, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

	_, tag1, _ := registry.ParseRef(args[0])
	_, tag2, _ := registry.ParseRef(args[1])

	manifestBytes1, _, layers1, err := store.LoadCheckpoint(tag1)
	if err != nil {
		return fmt.Errorf("loading %s: %w", args[0], err)
	}
	defer func() {
		for i := range layers1 {
			layers1[i].Cleanup()
		}
	}()

	manifestBytes2, _, layers2, err := store.LoadCheckpoint(tag2)
	if err != nil {
		return fmt.Errorf("loading %s: %w", args[1], err)
	}
	defer func() {
		for i := range layers2 {
			layers2[i].Cleanup()
		}
	}()

	var m1, m2 ocispec.Manifest
	_ = json.Unmarshal(manifestBytes1, &m1)
	_ = json.Unmarshal(manifestBytes2, &m2)

	layerName := func(descs []ocispec.Descriptor, idx int) string {
		if idx < len(descs) {
			if name, ok := descs[idx].Annotations[manifest.AnnotationTitle]; ok {
				return name
			}
		}
		return fmt.Sprintf("layer-%d", idx)
	}

	fmt.Printf("Comparing %s → %s\n", args[0], args[1])

	hasChanges := false
	for i := 0; i < len(layers1) && i < len(layers2); i++ {
		name := layerName(m1.Layers, i)

		if layers1[i].Digest == layers2[i].Digest {
			fmt.Printf("\n  %s%s: unchanged%s (%s)\n",
				colorDim, name, colorReset, truncateDigest(layers1[i].Digest))
			continue
		}

		// Stream both layers for hash comparison (no full load into memory)
		r1, err := layers1[i].NewReader()
		if err != nil {
			continue
		}
		hashes1, _ := workspace.ListLayerFilesWithHashesFromReader(r1)
		_ = r1.Close()

		r2, err := layers2[i].NewReader()
		if err != nil {
			continue
		}
		hashes2, _ := workspace.ListLayerFilesWithHashesFromReader(r2)
		_ = r2.Close()

		added, removed, modified := diffFileMaps(hashes1, hashes2)

		// Compute line counts by re-reading both layers for the changed files.
		lineCounts := computeCheckpointLineCounts(layers1[i], layers2[i], added, removed, modified)
		printLayerDiff(name, added, removed, modified, lineCounts, &hasChanges)
	}
	if !hasChanges {
		fmt.Println("\nNo changes between checkpoints.")
	}

	return nil
}


// computeWorkspaceLineCounts extracts the content needed to compute per-file
// line change counts for a workspace diff (saved layer vs current workspace).
func computeWorkspaceLineCounts(
	layer registry.LayerData,
	added, removed, modified []string,
	dir string,
	sr *workspace.ScanResult,
) map[string]fileLineCounts {
	// Build a reverse map from display-path → abs path for external files.
	extAbsPath := make(map[string]string)
	for _, ef := range sr.ExternalFiles {
		extAbsPath[workspace.DisplayPath(ef.ArchivePath)] = ef.AbsPath
	}

	// Collect the set of files we need from the saved layer (removed + modified).
	needFromLayer := make(map[string]bool)
	for _, f := range removed {
		needFromLayer[f] = true
	}
	for _, f := range modified {
		needFromLayer[f] = true
	}

	// Extract those files from the saved layer in a single pass.
	var savedContent map[string][]byte
	if len(needFromLayer) > 0 {
		r, err := layer.NewReader()
		if err == nil {
			savedContent, _ = workspace.ExtractFilesFromLayer(r, needFromLayer, maxLineDiffBytes)
			_ = r.Close()
		}
	}

	result := make(map[string]fileLineCounts)

	absPathFor := func(f string) string {
		if ap, ok := extAbsPath[f]; ok {
			return ap
		}
		return filepath.Join(dir, f)
	}

	readWorkspace := func(f string) []byte {
		data, _ := os.ReadFile(absPathFor(f))
		return data
	}

	for _, f := range added {
		data := readWorkspace(f)
		if !looksLikeText(data) {
			continue
		}
		lines := len(splitLines(data))
		result[f] = fileLineCounts{added: lines}
	}
	for _, f := range removed {
		data := savedContent[f]
		if !looksLikeText(data) {
			continue
		}
		lines := len(splitLines(data))
		result[f] = fileLineCounts{removed: lines}
	}
	for _, f := range modified {
		oldData := savedContent[f]
		newData := readWorkspace(f)
		if !looksLikeText(oldData) || !looksLikeText(newData) {
			continue
		}
		a, r := countLineDiff(oldData, newData)
		result[f] = fileLineCounts{added: a, removed: r}
	}
	return result
}

// computeCheckpointLineCounts computes per-file line counts when comparing two
// saved checkpoints. Both layers are re-read once each.
func computeCheckpointLineCounts(
	layer1, layer2 registry.LayerData,
	added, removed, modified []string,
) map[string]fileLineCounts {
	// Files needed from layer1 (the "old" side): removed + modified.
	needFrom1 := make(map[string]bool)
	for _, f := range removed {
		needFrom1[f] = true
	}
	for _, f := range modified {
		needFrom1[f] = true
	}
	// Files needed from layer2 (the "new" side): added + modified.
	needFrom2 := make(map[string]bool)
	for _, f := range added {
		needFrom2[f] = true
	}
	for _, f := range modified {
		needFrom2[f] = true
	}

	var content1, content2 map[string][]byte
	if len(needFrom1) > 0 {
		if r, err := layer1.NewReader(); err == nil {
			content1, _ = workspace.ExtractFilesFromLayer(r, needFrom1, maxLineDiffBytes)
			_ = r.Close()
		}
	}
	if len(needFrom2) > 0 {
		if r, err := layer2.NewReader(); err == nil {
			content2, _ = workspace.ExtractFilesFromLayer(r, needFrom2, maxLineDiffBytes)
			_ = r.Close()
		}
	}

	result := make(map[string]fileLineCounts)
	for _, f := range added {
		data := content2[f]
		if !looksLikeText(data) {
			continue
		}
		result[f] = fileLineCounts{added: len(splitLines(data))}
	}
	for _, f := range removed {
		data := content1[f]
		if !looksLikeText(data) {
			continue
		}
		result[f] = fileLineCounts{removed: len(splitLines(data))}
	}
	for _, f := range modified {
		old, new := content1[f], content2[f]
		if !looksLikeText(old) || !looksLikeText(new) {
			continue
		}
		a, r := countLineDiff(old, new)
		result[f] = fileLineCounts{added: a, removed: r}
	}
	return result
}

// splitLines splits content into lines, trimming a trailing newline first.
func splitLines(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func truncateDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}
