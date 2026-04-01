package registry

import (
	"bytes"
	"io"
	"os"
)

// Store is the interface for persisting and retrieving checkpoint artifacts.
type Store interface {
	// SaveCheckpoint writes a checkpoint (manifest, config, and layers) to the store.
	// Returns the manifest digest.
	SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []LayerData) (string, error)

	// LoadCheckpoint reads a checkpoint by reference (tag or digest).
	// Layers are backed by temporary files on disk; callers must call
	// LayerData.Cleanup() on each layer when done to remove temp files.
	LoadCheckpoint(ref string) (manifestBytes, configBytes []byte, layers []LayerData, err error)

	// LoadManifest reads only the manifest and config for a checkpoint, without
	// fetching layer blobs. Use this when layer content is not needed (e.g.
	// reading parent layer digests during save).
	LoadManifest(ref string) (manifestBytes, configBytes []byte, err error)

	// ListCheckpoints returns all tagged checkpoint entries.
	ListCheckpoints() ([]CheckpointEntry, error)

	// ResolveTag resolves a tag to its manifest digest.
	ResolveTag(tag string) (string, error)

	// Tag assigns a tag to an existing manifest digest.
	Tag(digest, tag string) error

	// DeleteCheckpoint removes a manifest entry from the index.
	// It does not delete blobs; that is left to garbage collection.
	DeleteCheckpoint(digest string) error
}

// LayerData holds metadata and content access for a single layer blob.
// Content is either in-memory (Data != nil) or on disk (Path != "").
// File-backed layers must be cleaned up with Cleanup() when no longer needed.
type LayerData struct {
	MediaType string
	// Data holds in-memory layer content. Nil for file-backed layers.
	Data []byte
	// Path is the path to a temp file holding the layer content.
	// Mutually exclusive with Data. Call Cleanup() to remove the temp file.
	Path   string
	Digest string
	Size   int64  // byte size, required when Digest is pre-computed
	DiffID string // "sha256:<hex>" of uncompressed tar bytes (OCI config diff_id), optional
}

// BlobSize returns the size of the layer content.
// If Size is set it is returned directly; otherwise len(Data) is used.
func (ld *LayerData) BlobSize() int64 {
	if ld.Size > 0 {
		return ld.Size
	}
	return int64(len(ld.Data))
}

// NewReader returns an io.ReadCloser over the layer content.
// For file-backed layers (Path != ""), the caller must close the returned reader.
// For in-memory layers (Data != nil), close is a no-op.
func (ld *LayerData) NewReader() (io.ReadCloser, error) {
	if ld.Path != "" {
		return os.Open(ld.Path)
	}
	return io.NopCloser(bytes.NewReader(ld.Data)), nil
}

// Cleanup removes the temp file for file-backed layers.
// Safe to call on in-memory layers (no-op).
func (ld *LayerData) Cleanup() {
	if ld.Path != "" {
		_ = os.Remove(ld.Path)
	}
}

// CheckpointEntry is a summary of a checkpoint as listed in the index.
type CheckpointEntry struct {
	Tag     string
	Digest  string
	Created string
	Message string
}

// NewStore creates a new local OCI-layout store at the given path.
func NewStore(storePath string) (Store, error) {
	return newLocalStore(storePath)
}
