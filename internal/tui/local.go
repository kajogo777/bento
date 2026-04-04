package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// LocalArtifactSource implements ArtifactSource for a local OCI store.
type LocalArtifactSource struct {
	store   registry.Store
	workDir string

	// Cache: tag → manifest info (loaded eagerly)
	manifests map[string]*ManifestInfo

	// Cache: "tag:layerIndex" → file entries (loaded lazily)
	fileCache map[string][]FileEntry
}

// NewLocalArtifactSource creates a source backed by a local OCI store.
func NewLocalArtifactSource(store registry.Store, workDir string) *LocalArtifactSource {
	return &LocalArtifactSource{
		store:     store,
		workDir:   workDir,
		manifests: make(map[string]*ManifestInfo),
		fileCache: make(map[string][]FileEntry),
	}
}

func (s *LocalArtifactSource) ListCheckpoints() ([]CheckpointSummary, error) {
	entries, err := s.store.ListCheckpoints()
	if err != nil {
		return nil, err
	}

	// Group tags by digest (same logic as cli/list.go)
	type group struct {
		tags     []string
		created  string
		digest   string
		message  string
		sequence int
	}
	digestOrder := []string{}
	groups := map[string]*group{}
	for _, e := range entries {
		if g, ok := groups[e.Digest]; ok {
			g.tags = append(g.tags, e.Tag)
		} else {
			digestOrder = append(digestOrder, e.Digest)
			seq := 0
			// Try to parse sequence from tag
			if strings.HasPrefix(e.Tag, "cp-") {
				seq, _ = strconv.Atoi(strings.TrimPrefix(e.Tag, "cp-"))
			}
			groups[e.Digest] = &group{
				tags:     []string{e.Tag},
				created:  e.Created,
				digest:   e.Digest,
				message:  e.Message,
				sequence: seq,
			}
		}
	}

	var summaries []CheckpointSummary
	for _, d := range digestOrder {
		g := groups[d]
		summaries = append(summaries, CheckpointSummary{
			Tags:     g.tags,
			Digest:   g.digest,
			Created:  g.created,
			Message:  g.message,
			Sequence: g.sequence,
		})
	}

	// Sort by sequence descending (newest first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Sequence > summaries[j].Sequence
	})

	return summaries, nil
}

func (s *LocalArtifactSource) LoadManifestInfo(tag string) (*ManifestInfo, error) {
	if info, ok := s.manifests[tag]; ok {
		return info, nil
	}

	manifestBytes, configBytes, err := s.store.LoadManifest(tag)
	if err != nil {
		return nil, fmt.Errorf("loading manifest %s: %w", tag, err)
	}

	cpInfo, err := manifest.ParseCheckpointInfo(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing checkpoint info: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	var bentoCfg *manifest.BentoConfigObj
	if len(configBytes) > 0 {
		bentoCfg, _ = manifest.UnmarshalConfig(configBytes)
	}

	// Build layer summaries from manifest descriptors
	layers := make([]LayerSummary, 0, len(m.Layers))
	for i, ld := range m.Layers {
		name := fmt.Sprintf("layer-%d", i)
		if title, ok := ld.Annotations[manifest.AnnotationTitle]; ok {
			name = title
		}
		fileCount := 0
		if fc, ok := ld.Annotations[manifest.AnnotationLayerFileCount]; ok {
			fileCount, _ = strconv.Atoi(fc)
		}
		layers = append(layers, LayerSummary{
			Name:      name,
			Size:      ld.Size,
			FileCount: fileCount,
			Digest:    ld.Digest.String(),
		})
	}

	// Build scrub paths set
	scrubPaths := make(map[string]bool)
	if bentoCfg != nil {
		for _, rec := range bentoCfg.ScrubRecords {
			scrubPaths[rec.Path] = true
		}
	}

	info := &ManifestInfo{
		CheckpointInfo: cpInfo,
		Config:         bentoCfg,
		Layers:         layers,
		ScrubPaths:     scrubPaths,
	}
	s.manifests[tag] = info
	return info, nil
}

func (s *LocalArtifactSource) ListLayerFiles(tag string, layerIndex int) ([]FileEntry, error) {
	cacheKey := fmt.Sprintf("%s:%d", tag, layerIndex)
	if cached, ok := s.fileCache[cacheKey]; ok {
		return cached, nil
	}

	info, err := s.LoadManifestInfo(tag)
	if err != nil {
		return nil, err
	}
	if layerIndex < 0 || layerIndex >= len(info.Layers) {
		return nil, fmt.Errorf("layer index %d out of range (0-%d)", layerIndex, len(info.Layers)-1)
	}

	// Load current layer file hashes
	_, _, layers, err := s.store.LoadCheckpoint(tag)
	if err != nil {
		return nil, fmt.Errorf("loading checkpoint %s: %w", tag, err)
	}
	defer func() {
		for i := range layers {
			layers[i].Cleanup()
		}
	}()

	if layerIndex >= len(layers) {
		return nil, fmt.Errorf("layer index %d not found in checkpoint", layerIndex)
	}

	r, err := layers[layerIndex].NewReader()
	if err != nil {
		return nil, fmt.Errorf("reading layer: %w", err)
	}
	currentHashes, err := workspace.ListLayerFilesWithHashesFromReader(r)
	_ = r.Close()
	if err != nil {
		return nil, fmt.Errorf("listing layer files: %w", err)
	}

	// Load parent layer file hashes (if parent exists)
	var parentHashes map[string]string
	if info.CheckpointInfo.Parent != "" {
		parentTag := resolveParentTag(info)
		if parentTag != "" {
			if _, _, parentLayers, pErr := s.store.LoadCheckpoint(parentTag); pErr == nil {
				defer func() {
					for i := range parentLayers {
						parentLayers[i].Cleanup()
					}
				}()
				if layerIndex < len(parentLayers) {
					pr, prErr := parentLayers[layerIndex].NewReader()
					if prErr == nil {
						parentHashes, _ = workspace.ListLayerFilesWithHashesFromReader(pr)
						_ = pr.Close()
					}
				}
			}
		}
	}

	// Build file entries with diff status
	entries := buildFileEntries(currentHashes, parentHashes, info.ScrubPaths)
	s.fileCache[cacheKey] = entries
	return entries, nil
}

func (s *LocalArtifactSource) PreviewFile(tag string, layerIndex int, path string, maxBytes int64) ([]byte, error) {
	_, _, layers, err := s.store.LoadCheckpoint(tag)
	if err != nil {
		return nil, fmt.Errorf("loading checkpoint %s: %w", tag, err)
	}
	defer func() {
		for i := range layers {
			layers[i].Cleanup()
		}
	}()

	if layerIndex >= len(layers) {
		return nil, fmt.Errorf("layer index %d not found", layerIndex)
	}

	r, err := layers[layerIndex].NewReader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	return workspace.ExtractFileContentFromLayer(r, path, maxBytes)
}

func (s *LocalArtifactSource) DiffFileContent(tag string, layerIndex int, path string, maxBytes int64) (string, error) {
	info, err := s.LoadManifestInfo(tag)
	if err != nil {
		return "", err
	}

	// Get current file content
	currentContent, err := s.PreviewFile(tag, layerIndex, path, maxBytes)
	if err != nil {
		return "", fmt.Errorf("reading current file: %w", err)
	}

	// If no parent, return raw content
	parentTag := resolveParentTag(info)
	if parentTag == "" {
		return string(currentContent), nil
	}

	// Get parent file content
	parentContent, parentErr := s.PreviewFile(parentTag, layerIndex, path, maxBytes)

	// File is new (not in parent)
	if parentErr != nil {
		return string(currentContent), nil
	}

	oldLines := SplitLines(parentContent)
	newLines := SplitLines(currentContent)

	diff := workspace.UnifiedDiff(
		fmt.Sprintf("a/%s (%s)", path, parentTag),
		fmt.Sprintf("b/%s (%s)", path, tag),
		oldLines, newLines, 3,
	)

	if diff == "" {
		return string(currentContent), nil
	}
	return diff, nil
}

func (s *LocalArtifactSource) Close() error {
	return nil // local store manages its own lifecycle
}

// resolveParentTag derives the parent checkpoint tag from ManifestInfo.
// Uses the checkpoint sequence to construct "cp-N" where N = current - 1.
func resolveParentTag(info *ManifestInfo) string {
	if info.CheckpointInfo.Parent == "" {
		return ""
	}
	seq := info.CheckpointInfo.Sequence
	if seq <= 1 {
		return ""
	}
	return fmt.Sprintf("cp-%d", seq-1)
}

// buildFileEntries creates FileEntry slice from current and parent hash maps.
// Mirrors the diffFileMaps logic from cli/root.go.
func buildFileEntries(current, parent map[string]string, scrubPaths map[string]bool) []FileEntry {
	entries := make([]FileEntry, 0, len(current))

	for path, hash := range current {
		status := Unchanged
		if parent != nil {
			if parentHash, ok := parent[path]; !ok {
				status = Added
			} else if parentHash != hash {
				status = Modified
			}
		}
		entries = append(entries, FileEntry{
			Path:       path,
			Size:       0, // size not available from hash map; will be enriched later if needed
			IsText:     IsTextFile(path),
			HasScrubs:  scrubPaths[path],
			DiffStatus: status,
		})
	}

	// Files removed in current (present in parent but not current)
	for path := range parent {
		if _, ok := current[path]; !ok {
			entries = append(entries, FileEntry{
				Path:       path,
				Size:       0,
				IsText:     IsTextFile(path),
				HasScrubs:  scrubPaths[path],
				DiffStatus: Removed,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries
}

// NewArtifactSource creates an ArtifactSource from a ref string.
// If ref contains "/" it's treated as a remote registry reference (not yet implemented).
// Otherwise, it opens the local store.
func NewArtifactSource(ref string, workDir string) (ArtifactSource, string, error) {
	isRemoteRef := strings.Contains(ref, "/")
	if isRemoteRef {
		return nil, "", fmt.Errorf("remote artifact exploration not yet implemented — use a local ref or pull first")
	}

	// Local ref: parse and open store
	storeName, tag, err := registry.ParseRef(ref)
	if err != nil {
		return nil, "", err
	}

	storePath := ""
	cfg, cfgErr := config.Load(workDir)
	if cfgErr == nil {
		if storeName == "" {
			storeName = cfg.ID
		}
		storePath = cfg.StorePath()
	} else {
		if storeName == "" {
			storeName = "."
		}
		storePath = config.DefaultStorePath() + "/" + storeName
	}

	// If storeName was provided but different from config, use it
	if storeName != "" && cfgErr == nil && storeName != cfg.ID {
		storePath = cfg.Store + "/" + storeName
	}

	store, err := registry.NewStore(storePath)
	if err != nil {
		return nil, "", fmt.Errorf("opening store: %w", err)
	}

	return NewLocalArtifactSource(store, workDir), tag, nil
}
