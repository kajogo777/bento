package tui

import (
	"github.com/kajogo777/bento/internal/manifest"
)

// DiffStatus represents the change status of a file compared to its parent checkpoint.
type DiffStatus int

const (
	Unchanged DiffStatus = iota
	Added
	Removed
	Modified
)

// String returns a single-character sigil for the diff status.
func (d DiffStatus) String() string {
	switch d {
	case Added:
		return "+"
	case Removed:
		return "-"
	case Modified:
		return "~"
	default:
		return " "
	}
}

// CheckpointSummary holds lightweight metadata for listing checkpoints.
type CheckpointSummary struct {
	Tags     []string
	Digest   string
	Created  string
	Message  string
	Sequence int
}

// ManifestInfo holds parsed manifest + config metadata for a single checkpoint.
type ManifestInfo struct {
	CheckpointInfo *manifest.CheckpointInfo
	Config         *manifest.BentoConfigObj
	Layers         []LayerSummary
	ScrubPaths     map[string]bool // set of file paths with scrubbed secrets
}

// LayerSummary holds metadata about a single OCI layer.
type LayerSummary struct {
	Name      string
	Size      int64
	FileCount int
	Digest    string
}

// FileEntry represents a single file within a layer.
type FileEntry struct {
	Path       string
	Size       int64
	IsText     bool
	HasScrubs  bool
	DiffStatus DiffStatus
}

// ArtifactSource abstracts local vs remote artifact access.
// Implementations lazily load layer blobs — only manifests/configs are loaded eagerly.
type ArtifactSource interface {
	// ListCheckpoints returns all tagged checkpoints.
	ListCheckpoints() ([]CheckpointSummary, error)

	// LoadManifestInfo loads manifest + config for a checkpoint by tag.
	LoadManifestInfo(tag string) (*ManifestInfo, error)

	// ListLayerFiles returns files in a layer, annotated with diff status
	// vs parent checkpoint. Loads layer blob on first call (lazy), caches after.
	ListLayerFiles(tag string, layerIndex int) ([]FileEntry, error)

	// PreviewFile returns text content for a file, capped at maxBytes.
	PreviewFile(tag string, layerIndex int, path string, maxBytes int64) ([]byte, error)

	// DiffFileContent returns a unified diff string of a file's content
	// between current and parent checkpoint. Returns raw content if no parent.
	DiffFileContent(tag string, layerIndex int, path string, maxBytes int64) (string, error)

	// Close cleans up temp files and cached resources.
	Close() error
}
