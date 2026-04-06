package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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
	var flagFile string

	cmd := &cobra.Command{
		Use:   "diff [ref1] [ref2]",
		Short: "Show changes since last checkpoint, or compare two checkpoints",
		Long: `With no arguments, shows what changed in the workspace since the last save.
With one argument, compares that checkpoint to the current workspace.
With two arguments, compares two checkpoints.

Use --file to show a unified diff for a specific file.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(flagDir)
			if err != nil {
				return err
			}

			if flagFile != "" {
				return diffFile(dir, args, flagFile)
			}

			if len(args) <= 1 {
				return diffWorkspace(dir, args)
			}
			return diffCheckpoints(dir, args)
		},
	}

	cmd.Flags().StringVar(&flagFile, "file", "", "show unified diff for a specific file")
	return cmd
}

func diffWorkspace(dir string, args []string) error {
	cfg, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

	// Default to head (this directory's position), fall back to "latest".
	ref := "latest"
	if len(args) == 1 {
		ref = args[0]
	} else if cfg.Head != "" {
		ref = cfg.Head
	}
	_, tag, _ := registry.ParseRef(ref)

	manifestBytes, configBytes, layers, err := store.LoadCheckpoint(tag)
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

	// Load scrub records to get per-file content hashes. The saved layer has
	// scrubbed content (with placeholders), but the workspace has real secrets.
	// Instead of re-scrubbing, we compare the workspace file's hash against the
	// stored ContentHash (SHA256 of original pre-scrub content). If they match,
	// the file hasn't changed — use the saved layer's hash for comparison.
	scrubContentHashes := make(map[string]string) // relPath → "sha256:<hex>" of original content
	if bentoCfg, parseErr := manifest.UnmarshalConfig(configBytes); parseErr == nil {
		for _, rec := range bentoCfg.ScrubRecords {
			if rec.ContentHash != "" {
				scrubContentHashes[rec.Path] = rec.ContentHash
			}
		}
	}

	resolved := resolveExtensions(dir, cfg)
	layerDefs := resolved.Layers

	ignorePatterns := append(config.DefaultIgnorePatterns, resolved.Ignore...)
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

	type layerDiffResult struct {
		added, removed, modified []string
		lineCounts               map[string]fileLineCounts
		name                     string
		skip                     bool
	}

	// Iterate manifest layers (not layerDefs) so all layers — including
	// non-workspace layers like secrets — are shown consistently.
	numLayers := len(m.Layers)
	layerResults := make([]layerDiffResult, numLayers)

	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	for i, desc := range m.Layers {
		if i >= len(layers) {
			layerResults[i].skip = true
			continue
		}

		layerName := fmt.Sprintf("layer-%d", i)
		if name, ok := desc.Annotations[manifest.AnnotationTitle]; ok {
			layerName = name
		}

		// Non-workspace layers (e.g., secrets) have no workspace files to
		// compare — they're always "unchanged" from the workspace perspective.
		sr, hasWorkspaceFiles := scanResults[layerName]
		if !hasWorkspaceFiles {
			layerResults[i] = layerDiffResult{name: layerName}
			continue
		}

		i := i // capture loop variable
		g.Go(func() error {
			// Stream the saved layer to compute hashes (avoids loading GBs into memory)
			r, err := layers[i].NewReader()
			if err != nil {
				layerResults[i].skip = true
				return nil
			}
			savedHashes, _ := workspace.ListLayerFilesWithHashesFromReader(r)
			_ = r.Close()

			currentHashes := make(map[string]string)

			// Hash workspace and external files concurrently using errgroup.
			var mu sync.Mutex
			hg := new(errgroup.Group)
			hg.SetLimit(runtime.NumCPU())

			for _, f := range sr.WorkspaceFiles {
				f := f
				hg.Go(func() error {
					hash, err := workspace.HashFileStreaming(filepath.Join(dir, f))
					if err == nil {
						// For files with scrubbed secrets, the saved layer has
						// placeholder content (different hash). If the workspace
						// file's hash matches the stored pre-scrub ContentHash,
						// the file is unchanged — use the saved layer's hash so
						// diffFileMaps sees no change.
						if expectedHash, ok := scrubContentHashes[f]; ok && hash == expectedHash {
							mu.Lock()
							if savedHash, exists := savedHashes[f]; exists {
								currentHashes[f] = savedHash
							} else {
								currentHashes[f] = hash
							}
							mu.Unlock()
						} else {
							mu.Lock()
							currentHashes[f] = hash
							mu.Unlock()
						}
					}
					return nil // skip files that error, matching original behavior
				})
			}
			for _, ef := range sr.ExternalFiles {
				ef := ef
				hg.Go(func() error {
					hash, err := workspace.HashFileStreaming(ef.AbsPath)
					if err == nil {
						mu.Lock()
						currentHashes[workspace.DisplayPath(ef.ArchivePath)] = hash
						mu.Unlock()
					}
					return nil // skip files that error, matching original behavior
				})
			}
			_ = hg.Wait() // individual file errors are silently skipped

			added, removed, modified := diffFileMaps(savedHashes, currentHashes)

			// Compute line counts for added/removed/modified files.
			lineCounts := computeWorkspaceLineCounts(layers[i], added, removed, modified, dir, sr)

			layerResults[i] = layerDiffResult{
				added:      added,
				removed:    removed,
				modified:   modified,
				lineCounts: lineCounts,
				name:       layerName,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Print results in original layer order.
	hasChanges := false
	for i, res := range layerResults {
		if res.skip {
			continue
		}
		printLayerDiff(i, numLayers, res.name, res.added, res.removed, res.modified, res.lineCounts, &hasChanges)
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

	numLayers := len(layers1)
	if len(layers2) < numLayers {
		numLayers = len(layers2)
	}

	type checkpointDiffResult struct {
		added, removed, modified []string
		lineCounts               map[string]fileLineCounts
		name                     string
		unchanged                bool
		digest                   string
		skip                     bool
	}

	cpResults := make([]checkpointDiffResult, numLayers)

	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	for i := 0; i < numLayers; i++ {
		i := i // capture loop variable
		name := layerName(m1.Layers, i)
		g.Go(func() error {
			if layers1[i].Digest == layers2[i].Digest {
				cpResults[i] = checkpointDiffResult{
					name:      name,
					unchanged: true,
					digest:    layers1[i].Digest,
				}
				return nil
			}

			// Stream both layers concurrently for hash comparison.
			var hashes1, hashes2 map[string]string
			var mu sync.Mutex
			hg := new(errgroup.Group)
			hg.SetLimit(2)

			hg.Go(func() error {
				r1, err := layers1[i].NewReader()
				if err != nil {
					return nil
				}
				h, _ := workspace.ListLayerFilesWithHashesFromReader(r1)
				_ = r1.Close()
				mu.Lock()
				hashes1 = h
				mu.Unlock()
				return nil
			})
			hg.Go(func() error {
				r2, err := layers2[i].NewReader()
				if err != nil {
					return nil
				}
				h, _ := workspace.ListLayerFilesWithHashesFromReader(r2)
				_ = r2.Close()
				mu.Lock()
				hashes2 = h
				mu.Unlock()
				return nil
			})
			_ = hg.Wait()

			if hashes1 == nil || hashes2 == nil {
				cpResults[i].skip = true
				return nil
			}

			added, removed, modified := diffFileMaps(hashes1, hashes2)

			// Compute line counts by re-reading both layers for the changed files.
			lineCounts := computeCheckpointLineCounts(layers1[i], layers2[i], added, removed, modified)

			cpResults[i] = checkpointDiffResult{
				name:       name,
				added:      added,
				removed:    removed,
				modified:   modified,
				lineCounts: lineCounts,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Print results in original layer order.
	hasChanges := false
	for i, res := range cpResults {
		if res.skip {
			continue
		}
		if res.unchanged {
			fmt.Printf("\n  %s[%d/%d] %s: unchanged%s (%s)\n",
				colorDim, i+1, numLayers, res.name, colorReset, truncateDigest(res.digest))
			continue
		}
		printLayerDiff(i, numLayers, res.name, res.added, res.removed, res.modified, res.lineCounts, &hasChanges)
	}
	if !hasChanges {
		fmt.Println("\nNo changes between checkpoints.")
	}

	return nil
}

// maxFileDiffBytes is the per-file content cap for unified diff extraction.
const maxFileDiffBytes = 10 << 20 // 10 MiB

// diffFile shows a unified diff for a single file.
// With 0 or 1 args: workspace vs checkpoint. With 2 args: checkpoint vs checkpoint.
func diffFile(dir string, args []string, filePath string) error {
	filePath = workspace.NormalizePath(filePath)

	cfg, store, err := loadConfigAndStore(dir)
	if err != nil {
		return err
	}

	if len(args) <= 1 {
		return diffFileWorkspace(dir, args, filePath, cfg, store)
	}
	return diffFileCheckpoints(dir, args, filePath, store)
}

func diffFileWorkspace(dir string, args []string, filePath string, cfg *config.BentoConfig, store registry.Store) error {
	// Default to head (this directory's position), fall back to "latest".
	ref := "latest"
	if len(args) == 1 {
		ref = args[0]
	} else if cfg.Head != "" {
		ref = cfg.Head
	}
	_, tag, _ := registry.ParseRef(ref)

	_, _, layers, err := store.LoadCheckpoint(tag)
	if err != nil {
		return fmt.Errorf("no checkpoints found. Run `bento save` first")
	}
	defer func() {
		for i := range layers {
			layers[i].Cleanup()
		}
	}()

	// Find the file in one of the checkpoint layers
	var oldContent []byte
	for _, layer := range layers {
		r, err := layer.NewReader()
		if err != nil {
			continue
		}
		data, err := workspace.ExtractFileContentFromLayer(r, filePath, maxFileDiffBytes)
		_ = r.Close()
		if err == nil {
			oldContent = data
			break
		}
	}

	// Read current workspace file
	var newContent []byte
	wsPath := filepath.Join(dir, filepath.FromSlash(filePath))
	if data, err := os.ReadFile(wsPath); err == nil {
		newContent = data
	}

	if oldContent == nil && newContent == nil {
		return fmt.Errorf("file %s not found in checkpoint or workspace", filePath)
	}

	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	oldName := fmt.Sprintf("a/%s (%s)", filePath, tag)
	newName := fmt.Sprintf("b/%s (workspace)", filePath)

	diff := workspace.UnifiedDiff(oldName, newName, oldLines, newLines, 3)
	if diff == "" {
		fmt.Printf("No changes in %s\n", filePath)
		return nil
	}
	printColorizedDiff(diff)
	return nil
}

func diffFileCheckpoints(dir string, args []string, filePath string, store registry.Store) error {
	_, tag1, _ := registry.ParseRef(args[0])
	_, tag2, _ := registry.ParseRef(args[1])

	_, _, layers1, err := store.LoadCheckpoint(tag1)
	if err != nil {
		return fmt.Errorf("loading %s: %w", args[0], err)
	}
	defer func() {
		for i := range layers1 {
			layers1[i].Cleanup()
		}
	}()

	_, _, layers2, err := store.LoadCheckpoint(tag2)
	if err != nil {
		return fmt.Errorf("loading %s: %w", args[1], err)
	}
	defer func() {
		for i := range layers2 {
			layers2[i].Cleanup()
		}
	}()

	// Extract file from both checkpoints
	var oldContent, newContent []byte
	for _, layer := range layers1 {
		r, err := layer.NewReader()
		if err != nil {
			continue
		}
		data, err := workspace.ExtractFileContentFromLayer(r, filePath, maxFileDiffBytes)
		_ = r.Close()
		if err == nil {
			oldContent = data
			break
		}
	}
	for _, layer := range layers2 {
		r, err := layer.NewReader()
		if err != nil {
			continue
		}
		data, err := workspace.ExtractFileContentFromLayer(r, filePath, maxFileDiffBytes)
		_ = r.Close()
		if err == nil {
			newContent = data
			break
		}
	}

	if oldContent == nil && newContent == nil {
		return fmt.Errorf("file %s not found in either checkpoint", filePath)
	}

	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	oldName := fmt.Sprintf("a/%s (%s)", filePath, tag1)
	newName := fmt.Sprintf("b/%s (%s)", filePath, tag2)

	diff := workspace.UnifiedDiff(oldName, newName, oldLines, newLines, 3)
	if diff == "" {
		fmt.Printf("No changes in %s\n", filePath)
		return nil
	}
	printColorizedDiff(diff)
	return nil
}

// splitLines splits content into lines, handling both \n and \r\n.
// Returns nil for nil input (represents non-existent file).
func splitLines(data []byte) []string {
	if data == nil {
		return nil
	}
	s := string(data)
	// Remove trailing newline to avoid extra empty line
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// printColorizedDiff prints a unified diff with ANSI colors:
// red for removals, green for additions, cyan for hunk headers, bold for file headers.
func printColorizedDiff(diff string) {
	colorCyan := "\033[36m"
	colorBold := "\033[1m"
	for _, line := range strings.Split(diff, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
			fmt.Printf("%s%s%s\n", colorBold, line, colorReset)
		case strings.HasPrefix(line, "@@"):
			fmt.Printf("%s%s%s\n", colorCyan, line, colorReset)
		case strings.HasPrefix(line, "-"):
			fmt.Printf("%s%s%s\n", colorRed, line, colorReset)
		case strings.HasPrefix(line, "+"):
			fmt.Printf("%s%s%s\n", colorGreen, line, colorReset)
		default:
			fmt.Println(line)
		}
	}
}

// computeWorkspaceLineCounts computes per-file line change counts for a
// workspace diff (saved layer vs current workspace) using streaming line hashes.
func computeWorkspaceLineCounts(
	layer registry.LayerData,
	added, removed, modified []string,
	dir string,
	sr *workspace.ScanResult,
) map[string]fileLineCounts {
	extAbsPath := make(map[string]string)
	for _, ef := range sr.ExternalFiles {
		extAbsPath[workspace.DisplayPath(ef.ArchivePath)] = ef.AbsPath
	}

	needFromLayer := make(map[string]bool)
	for _, f := range removed {
		needFromLayer[f] = true
	}
	for _, f := range modified {
		needFromLayer[f] = true
	}

	var savedHashes map[string]workspace.LineHashSet
	if len(needFromLayer) > 0 {
		if r, err := layer.NewReader(); err == nil {
			savedHashes, _ = workspace.ExtractLineHashesFromLayer(r, needFromLayer, maxLineDiffBytes)
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

	for _, f := range added {
		set, err := workspace.HashLinesFromFile(absPathFor(f))
		if err != nil {
			continue
		}
		total := 0
		for _, c := range set {
			total += c
		}
		result[f] = fileLineCounts{added: total}
	}
	for _, f := range removed {
		set := savedHashes[f]
		if set == nil {
			continue
		}
		total := 0
		for _, c := range set {
			total += c
		}
		result[f] = fileLineCounts{removed: total}
	}
	for _, f := range modified {
		old := savedHashes[f]
		if old == nil {
			continue // file was too large or absent in checkpoint — skip workspace hash too
		}
		new, err := workspace.HashLinesFromFile(absPathFor(f))
		if err != nil {
			continue
		}
		a, r := countLineDiffFromSets(old, new)
		result[f] = fileLineCounts{added: a, removed: r}
	}
	return result
}

// computeCheckpointLineCounts computes per-file line counts when comparing two
// saved checkpoints using streaming line hashes.
func computeCheckpointLineCounts(
	layer1, layer2 registry.LayerData,
	added, removed, modified []string,
) map[string]fileLineCounts {
	needFrom1 := make(map[string]bool)
	for _, f := range removed {
		needFrom1[f] = true
	}
	for _, f := range modified {
		needFrom1[f] = true
	}

	needFrom2 := make(map[string]bool)
	for _, f := range added {
		needFrom2[f] = true
	}
	for _, f := range modified {
		needFrom2[f] = true
	}

	var hashes1, hashes2 map[string]workspace.LineHashSet
	if len(needFrom1) > 0 {
		if r, err := layer1.NewReader(); err == nil {
			hashes1, _ = workspace.ExtractLineHashesFromLayer(r, needFrom1, maxLineDiffBytes)
			_ = r.Close()
		}
	}
	if len(needFrom2) > 0 {
		if r, err := layer2.NewReader(); err == nil {
			hashes2, _ = workspace.ExtractLineHashesFromLayer(r, needFrom2, maxLineDiffBytes)
			_ = r.Close()
		}
	}

	result := make(map[string]fileLineCounts)
	for _, f := range added {
		set := hashes2[f]
		if set == nil {
			continue
		}
		total := 0
		for _, c := range set {
			total += c
		}
		result[f] = fileLineCounts{added: total}
	}
	for _, f := range removed {
		set := hashes1[f]
		if set == nil {
			continue
		}
		total := 0
		for _, c := range set {
			total += c
		}
		result[f] = fileLineCounts{removed: total}
	}
	for _, f := range modified {
		old, new := hashes1[f], hashes2[f]
		if old == nil || new == nil {
			continue
		}
		a, r := countLineDiffFromSets(old, new)
		result[f] = fileLineCounts{added: a, removed: r}
	}
	return result
}

func truncateDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}

