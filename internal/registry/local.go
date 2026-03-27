package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	ociLayoutFile    = "oci-layout"
	indexFile        = "index.json"
	blobsDir         = "blobs"
	sha256Dir        = "sha256"
	ociLayoutContent = `{"imageLayoutVersion":"1.0.0"}`

	annotationRefName = "org.opencontainers.image.ref.name"
)

// LocalStore implements Store using the OCI Image Layout format on the local filesystem.
type LocalStore struct {
	root string
}

// ensureLayout creates the oci-layout file and an initial index.json if they do not exist.
func (s *LocalStore) ensureLayout() error {
	blobPath := filepath.Join(s.root, blobsDir, sha256Dir)
	if err := os.MkdirAll(blobPath, 0o755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	layoutPath := filepath.Join(s.root, ociLayoutFile)
	if _, err := os.Stat(layoutPath); os.IsNotExist(err) {
		if err := os.WriteFile(layoutPath, []byte(ociLayoutContent), 0o644); err != nil {
			return fmt.Errorf("write oci-layout: %w", err)
		}
	}

	idxPath := filepath.Join(s.root, indexFile)
	if _, err := os.Stat(idxPath); os.IsNotExist(err) {
		idx := ocispec.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageIndex,
			Manifests: []ocispec.Descriptor{},
		}
		data, err := json.MarshalIndent(idx, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal initial index: %w", err)
		}
		if err := os.WriteFile(idxPath, data, 0o644); err != nil {
			return fmt.Errorf("write initial index: %w", err)
		}
	}

	return nil
}

// writeBlob writes data to blobs/sha256/<hex> and returns the digest string.
func (s *LocalStore) writeBlob(data []byte) (string, error) {
	h := sha256.Sum256(data)
	hex := fmt.Sprintf("%x", h)
	digest := "sha256:" + hex

	blobPath := filepath.Join(s.root, blobsDir, sha256Dir, hex)
	if _, err := os.Stat(blobPath); err == nil {
		// Blob already exists.
		return digest, nil
	}

	if err := os.WriteFile(blobPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write blob %s: %w", hex, err)
	}
	return digest, nil
}

// readBlob reads a blob by its digest.
func (s *LocalStore) readBlob(digest string) ([]byte, error) {
	hex := strings.TrimPrefix(digest, "sha256:")
	blobPath := filepath.Join(s.root, blobsDir, sha256Dir, hex)
	data, err := os.ReadFile(blobPath)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", digest, err)
	}
	return data, nil
}

// readIndex reads and parses the index.json file.
func (s *LocalStore) readIndex() (*ocispec.Index, error) {
	idxPath := filepath.Join(s.root, indexFile)
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("unmarshal index: %w", err)
	}
	return &idx, nil
}

// writeIndex writes the index back to disk.
func (s *LocalStore) writeIndex(idx *ocispec.Index) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	idxPath := filepath.Join(s.root, indexFile)
	if err := os.WriteFile(idxPath, data, 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	return nil
}

// SaveCheckpoint writes a checkpoint's blobs and updates the index.
func (s *LocalStore) SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []LayerData) (string, error) {
	// Write config blob.
	if _, err := s.writeBlob(configBytes); err != nil {
		return "", fmt.Errorf("save config blob: %w", err)
	}

	// Write layer blobs.
	for _, l := range layers {
		if _, err := s.writeBlob(l.Data); err != nil {
			return "", fmt.Errorf("save layer blob: %w", err)
		}
	}

	// Write manifest blob.
	manifestDigest, err := s.writeBlob(manifestBytes)
	if err != nil {
		return "", fmt.Errorf("save manifest blob: %w", err)
	}

	// Parse the tag from the ref.
	_, tag, err := ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}

	// Update index.json.
	idx, err := s.readIndex()
	if err != nil {
		return "", err
	}

	// Remove any existing entry with the same tag.
	filtered := make([]ocispec.Descriptor, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		if d.Annotations[annotationRefName] != tag {
			filtered = append(filtered, d)
		}
	}

	// Parse manifest to get annotations for the index entry.
	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return "", fmt.Errorf("parse manifest for index: %w", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    godigest.Digest(manifestDigest),
		Size:      int64(len(manifestBytes)),
		Annotations: map[string]string{
			annotationRefName: tag,
		},
	}
	// Copy select annotations from the manifest.
	if created, ok := m.Annotations["org.opencontainers.image.created"]; ok {
		desc.Annotations["org.opencontainers.image.created"] = created
	}
	if msg, ok := m.Annotations["dev.bento.checkpoint.message"]; ok {
		desc.Annotations["dev.bento.checkpoint.message"] = msg
	}

	filtered = append(filtered, desc)
	idx.Manifests = filtered

	if err := s.writeIndex(idx); err != nil {
		return "", err
	}

	return manifestDigest, nil
}

// LoadCheckpoint reads a checkpoint by tag or digest.
func (s *LocalStore) LoadCheckpoint(ref string) (manifestBytes, configBytes []byte, layers []LayerData, err error) {
	// Resolve ref to digest.
	digest := ref
	if !strings.HasPrefix(ref, "sha256:") {
		_, tag, parseErr := ParseRef(ref)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		resolved, resolveErr := s.ResolveTag(tag)
		if resolveErr != nil {
			return nil, nil, nil, resolveErr
		}
		digest = resolved
	}

	// Read manifest.
	manifestBytes, err = s.readBlob(digest)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read manifest: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, nil, nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	// Read config.
	configBytes, err = s.readBlob(string(m.Config.Digest))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read config: %w", err)
	}

	// Read layers.
	layers = make([]LayerData, 0, len(m.Layers))
	for _, ld := range m.Layers {
		data, readErr := s.readBlob(string(ld.Digest))
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("read layer %s: %w", ld.Digest, readErr)
		}
		layers = append(layers, LayerData{
			MediaType: ld.MediaType,
			Data:      data,
			Digest:    string(ld.Digest),
		})
	}

	return manifestBytes, configBytes, layers, nil
}

// ListCheckpoints returns all tagged entries from the index.
func (s *LocalStore) ListCheckpoints() ([]CheckpointEntry, error) {
	idx, err := s.readIndex()
	if err != nil {
		return nil, err
	}

	entries := make([]CheckpointEntry, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		entry := CheckpointEntry{
			Digest:  string(d.Digest),
			Tag:     d.Annotations[annotationRefName],
			Created: d.Annotations["org.opencontainers.image.created"],
			Message: d.Annotations["dev.bento.checkpoint.message"],
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ResolveTag finds the digest for a given tag in the index.
func (s *LocalStore) ResolveTag(tag string) (string, error) {
	idx, err := s.readIndex()
	if err != nil {
		return "", err
	}

	for _, d := range idx.Manifests {
		if d.Annotations[annotationRefName] == tag {
			return string(d.Digest), nil
		}
	}
	return "", fmt.Errorf("tag %q not found", tag)
}

// Tag assigns a tag to an existing manifest digest.
func (s *LocalStore) Tag(digest, tag string) error {
	idx, err := s.readIndex()
	if err != nil {
		return err
	}

	// Find the descriptor for the digest.
	var found *ocispec.Descriptor
	for i := range idx.Manifests {
		if string(idx.Manifests[i].Digest) == digest {
			found = &idx.Manifests[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("digest %q not found in index", digest)
	}

	// Remove any existing entry with the target tag.
	filtered := make([]ocispec.Descriptor, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		if d.Annotations[annotationRefName] != tag {
			filtered = append(filtered, d)
		}
	}

	// Add a new descriptor with the tag (deep copy annotations to avoid aliasing).
	desc := *found
	desc.Annotations = make(map[string]string)
	for k, v := range found.Annotations {
		desc.Annotations[k] = v
	}
	desc.Annotations[annotationRefName] = tag
	filtered = append(filtered, desc)
	idx.Manifests = filtered

	return s.writeIndex(idx)
}

// DeleteCheckpoint removes a manifest entry from the index by digest.
// Blobs are not deleted; garbage collection handles that separately.
func (s *LocalStore) DeleteCheckpoint(digest string) error {
	idx, err := s.readIndex()
	if err != nil {
		return err
	}

	filtered := make([]ocispec.Descriptor, 0, len(idx.Manifests))
	found := false
	for _, d := range idx.Manifests {
		if string(d.Digest) == digest {
			found = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !found {
		return fmt.Errorf("digest %q not found in index", digest)
	}

	idx.Manifests = filtered
	return s.writeIndex(idx)
}
