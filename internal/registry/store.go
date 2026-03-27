package registry

// Store is the interface for persisting and retrieving checkpoint artifacts.
type Store interface {
	// SaveCheckpoint writes a checkpoint (manifest, config, and layers) to the store.
	// Returns the manifest digest.
	SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []LayerData) (string, error)

	// LoadCheckpoint reads a checkpoint by reference (tag or digest).
	LoadCheckpoint(ref string) (manifestBytes, configBytes []byte, layers []LayerData, err error)

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

// LayerData holds the raw bytes and metadata for a single layer blob.
type LayerData struct {
	MediaType string
	Data      []byte
	Digest    string
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
