package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"
)

// LocalStore implements Store using an oras-go OCI image layout on disk.
type LocalStore struct {
	oci *oci.Store
	ctx context.Context
}

// newLocalStore opens or creates an OCI image layout store at the given path.
// It ensures the shared blob pool exists at the store root level and that the
// workspace's blobs directory is a symlink into the shared pool.
func newLocalStore(storePath string) (*LocalStore, error) {
	if err := ensureSharedBlobLayout(storePath); err != nil {
		return nil, fmt.Errorf("setting up shared blob layout: %w", err)
	}

	ctx := context.Background()
	store, err := oci.NewWithContext(ctx, storePath)
	if err != nil {
		return nil, fmt.Errorf("opening OCI store at %s: %w", storePath, err)
	}
	store.AutoSaveIndex = true
	return &LocalStore{oci: store, ctx: ctx}, nil
}

// ensureSharedBlobLayout creates the shared blob pool at the store root
// (parent of storePath) and links storePath/blobs to the shared pool.
//
// On Unix, a symlink is used (blobs → ../blobs).
// On Windows, a directory junction is used (requires absolute target path
// but no admin privileges).
//
// Layout after setup:
//
//	store_root/
//	├── blobs/sha256/          ← shared across all workspaces
//	└── ws-xxx/
//	    ├── oci-layout
//	    ├── index.json
//	    └── blobs → ../blobs   ← symlink or junction
func ensureSharedBlobLayout(storePath string) error {
	storeRoot := filepath.Dir(storePath)
	sharedBlobDir := filepath.Join(storeRoot, "blobs", "sha256")
	wsBlobLink := filepath.Join(storePath, "blobs")

	// Ensure workspace directory exists.
	if err := os.MkdirAll(storePath, 0755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	// Ensure shared blob pool exists.
	if err := os.MkdirAll(sharedBlobDir, 0755); err != nil {
		return fmt.Errorf("creating shared blob dir: %w", err)
	}

	// Check current state of the blobs path in the workspace.
	fi, err := os.Lstat(wsBlobLink)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			// Already a symlink/junction — nothing to do.
			return nil
		}
		// On Windows, junctions appear as directories with reparse points,
		// not as symlinks. Check if it already resolves to the shared pool.
		if fi.IsDir() {
			resolved, evalErr := filepath.EvalSymlinks(wsBlobLink)
			sharedBlobs := filepath.Join(storeRoot, "blobs")
			absShared, _ := filepath.Abs(sharedBlobs)
			if evalErr == nil && resolved == absShared {
				// Already a junction pointing to the right place.
				return nil
			}

			// Real directory exists — remove it only if empty.
			entries, readErr := os.ReadDir(wsBlobLink)
			if readErr != nil {
				return fmt.Errorf("reading workspace blobs dir: %w", readErr)
			}
			if len(entries) > 0 {
				// Non-empty blobs dir without migration — this shouldn't
				// happen in normal flow. Leave it alone to avoid data loss.
				return nil
			}
			if err := os.Remove(wsBlobLink); err != nil {
				return fmt.Errorf("removing empty workspace blobs dir: %w", err)
			}
		}
	}

	// Create the directory link (symlink on Unix, junction on Windows).
	if err := createDirLink(filepath.Join(storeRoot, "blobs"), wsBlobLink); err != nil {
		// If the link already exists (race condition), that's fine.
		if !os.IsExist(err) {
			return fmt.Errorf("creating blobs link: %w", err)
		}
	}

	return nil
}

// SaveCheckpoint writes a checkpoint's blobs (config, layers, manifest) and tags it.
func (s *LocalStore) SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []LayerData) (string, error) {
	_, tag, err := ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}

	// Push config blob using the standard OCI config media type
	configDigest := digest.FromBytes(configBytes)
	configDesc := ocispec.Descriptor{
		MediaType: manifest.ConfigMediaType,
		Digest:    configDigest,
		Size:      int64(len(configBytes)),
	}
	if err := s.pushIfNotExists(configDesc, configBytes); err != nil {
		return "", fmt.Errorf("pushing config: %w", err)
	}

	// Push layer blobs
	for _, ld := range layers {
		var layerDigest digest.Digest
		if ld.Digest != "" {
			layerDigest = digest.Digest(ld.Digest)
		} else {
			layerDigest = digest.FromBytes(ld.Data)
		}
		layerDesc := ocispec.Descriptor{
			MediaType: ld.MediaType,
			Digest:    layerDigest,
			Size:      ld.BlobSize(),
		}
		if err := s.pushLayerIfNotExists(layerDesc, ld); err != nil {
			return "", fmt.Errorf("pushing layer: %w", err)
		}
	}

	// Push manifest
	manifestDigest := digest.FromBytes(manifestBytes)
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestBytes)),
	}
	if err := s.pushIfNotExists(manifestDesc, manifestBytes); err != nil {
		return "", fmt.Errorf("pushing manifest: %w", err)
	}

	// Tag
	if err := s.oci.Tag(s.ctx, manifestDesc, tag); err != nil {
		return "", fmt.Errorf("tagging %s: %w", tag, err)
	}

	return manifestDigest.String(), nil
}

// LoadManifest reads only the manifest and config for a checkpoint without
// fetching layer blobs. Use this when layer content is not needed (e.g.
// reading parent layer digests during save).
func (s *LocalStore) LoadManifest(ref string) (manifestBytes []byte, configBytes []byte, err error) {
	desc, err := s.oci.Resolve(s.ctx, ref)
	if err != nil {
		return nil, nil, fmt.Errorf("tag %q not found", ref)
	}

	manifestBytes, err = s.fetchBlob(desc)
	if err != nil {
		return nil, nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	configBytes, err = s.fetchBlob(m.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	return manifestBytes, configBytes, nil
}

// LoadCheckpoint reads a checkpoint by tag or digest. Layer blobs are streamed
// to temporary files rather than loaded into memory to support large layers.
// Callers MUST call LayerData.Cleanup() on each returned layer when done.
func (s *LocalStore) LoadCheckpoint(ref string) (manifestBytes, configBytes []byte, layers []LayerData, err error) {
	manifestBytes, configBytes, err = s.LoadManifest(ref)
	if err != nil {
		return nil, nil, nil, err
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	layers = make([]LayerData, 0, len(m.Layers))
	for _, ld := range m.Layers {
		path, err := s.fetchBlobToTemp(ld)
		if err != nil {
			// Clean up already-created temp files before returning
			for i := range layers {
				layers[i].Cleanup()
			}
			return nil, nil, nil, fmt.Errorf("reading layer %s: %w", ld.Digest, err)
		}
		layers = append(layers, LayerData{
			MediaType: ld.MediaType,
			Path:      path,
			Digest:    ld.Digest.String(),
		})
	}

	return manifestBytes, configBytes, layers, nil
}

// ListCheckpoints returns all tagged entries from the index.
func (s *LocalStore) ListCheckpoints() ([]CheckpointEntry, error) {
	var entries []CheckpointEntry

	err := s.oci.Tags(s.ctx, "", func(tags []string) error {
		for _, tag := range tags {
			desc, err := s.oci.Resolve(s.ctx, tag)
			if err != nil {
				continue
			}
			// Fetch manifest to read annotations (manifests are small, OK in memory)
			data, err := s.fetchBlob(desc)
			if err != nil {
				continue
			}
			var m ocispec.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}

			entries = append(entries, CheckpointEntry{
				Tag:     tag,
				Digest:  desc.Digest.String(),
				Created: m.Annotations["org.opencontainers.image.created"],
				Message: m.Annotations["dev.bento.checkpoint.message"],
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// ResolveTag resolves a tag to its manifest digest.
func (s *LocalStore) ResolveTag(tag string) (string, error) {
	desc, err := s.oci.Resolve(s.ctx, tag)
	if err != nil {
		return "", fmt.Errorf("tag %q not found", tag)
	}
	return desc.Digest.String(), nil
}

// Tag assigns a tag to an existing manifest digest.
func (s *LocalStore) Tag(digestStr, tag string) error {
	// Find the descriptor for this digest by resolving any existing tag that points to it
	var found *ocispec.Descriptor
	_ = s.oci.Tags(s.ctx, "", func(tags []string) error {
		for _, t := range tags {
			desc, err := s.oci.Resolve(s.ctx, t)
			if err == nil && desc.Digest.String() == digestStr {
				found = &desc
				return fmt.Errorf("found") // break out of callback
			}
		}
		return nil
	})

	if found == nil {
		return fmt.Errorf("digest %q not found", digestStr)
	}

	return s.oci.Tag(s.ctx, *found, tag)
}

// DeleteCheckpoint removes a manifest and its tags from the store.
func (s *LocalStore) DeleteCheckpoint(digestStr string) error {
	// Find and untag all tags pointing to this digest
	var tagsToRemove []string
	_ = s.oci.Tags(s.ctx, "", func(tags []string) error {
		for _, t := range tags {
			desc, err := s.oci.Resolve(s.ctx, t)
			if err == nil && desc.Digest.String() == digestStr {
				tagsToRemove = append(tagsToRemove, t)
			}
		}
		return nil
	})

	if len(tagsToRemove) == 0 {
		return fmt.Errorf("digest %q not found", digestStr)
	}

	for _, t := range tagsToRemove {
		if err := s.oci.Untag(s.ctx, t); err != nil {
			return fmt.Errorf("untagging %s: %w", t, err)
		}
	}

	return nil
}

// OCI returns the underlying oras OCI store for direct use (e.g. oras.Copy).
func (s *LocalStore) OCI() *oci.Store {
	return s.oci
}

// InjectLayer adds a new layer to an existing checkpoint's manifest.
// It pushes the layer blob, rebuilds the manifest with the additional layer
// descriptor, pushes the new manifest, and re-tags. The original manifest
// is replaced (same tag points to the new manifest).
func (s *LocalStore) InjectLayer(tag string, layer LayerData, annotations map[string]string) error {
	// Resolve the existing manifest.
	desc, err := s.oci.Resolve(s.ctx, tag)
	if err != nil {
		return fmt.Errorf("resolving tag %q: %w", tag, err)
	}

	manifestBytes, err := s.fetchBlob(desc)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	// Push the new layer blob.
	layerDigest := digest.Digest(layer.Digest)
	layerDesc := ocispec.Descriptor{
		MediaType:   layer.MediaType,
		Digest:      layerDigest,
		Size:        layer.BlobSize(),
		Annotations: annotations,
	}
	if err := s.pushLayerIfNotExists(layerDesc, layer); err != nil {
		return fmt.Errorf("pushing layer blob: %w", err)
	}

	// Append the layer descriptor to the manifest.
	m.Layers = append(m.Layers, layerDesc)

	// Update the OCI config to add the new layer's diffID.
	// Use the proper uncompressed tar digest when available (from PackResult.DiffID),
	// falling back to the gzip digest for backward compatibility.
	configBytes, err := s.fetchBlob(m.Config)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var imageConfig ocispec.Image
	if err := json.Unmarshal(configBytes, &imageConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	diffID := layerDigest
	if layer.DiffID != "" {
		diffID = digest.Digest(layer.DiffID)
	}
	imageConfig.RootFS.DiffIDs = append(imageConfig.RootFS.DiffIDs, diffID)

	newConfigBytes, err := json.Marshal(imageConfig)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	newConfigDigest := digest.FromBytes(newConfigBytes)
	newConfigDesc := ocispec.Descriptor{
		MediaType: manifest.ConfigMediaType,
		Digest:    newConfigDigest,
		Size:      int64(len(newConfigBytes)),
	}
	if err := s.pushIfNotExists(newConfigDesc, newConfigBytes); err != nil {
		return fmt.Errorf("pushing config: %w", err)
	}
	m.Config = newConfigDesc

	// Push the new manifest.
	newManifestBytes, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	newManifestDigest := digest.FromBytes(newManifestBytes)
	newManifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    newManifestDigest,
		Size:      int64(len(newManifestBytes)),
	}
	if err := s.pushIfNotExists(newManifestDesc, newManifestBytes); err != nil {
		return fmt.Errorf("pushing manifest: %w", err)
	}

	// Re-tag to point to the new manifest.
	if err := s.oci.Tag(s.ctx, newManifestDesc, tag); err != nil {
		return fmt.Errorf("re-tagging %s: %w", tag, err)
	}

	return nil
}

// RemoveSecretsLayer removes the encrypted secrets layer from a checkpoint's
// manifest and config, then re-tags. Used by push to replace the secrets layer
// when re-wrapping with different sender/recipients.
func (s *LocalStore) RemoveSecretsLayer(tag string) error {
	desc, err := s.oci.Resolve(s.ctx, tag)
	if err != nil {
		return fmt.Errorf("resolving tag %q: %w", tag, err)
	}

	manifestBytes, err := s.fetchBlob(desc)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	// Find and remove the secrets layer.
	found := -1
	for i, ld := range m.Layers {
		if ld.Annotations[manifest.AnnotationSecretsEncrypted] == "true" {
			found = i
			break
		}
	}
	if found == -1 {
		return nil // nothing to remove
	}
	m.Layers = append(m.Layers[:found], m.Layers[found+1:]...)

	// Also remove the corresponding diffID from the config.
	configBytes, err := s.fetchBlob(m.Config)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	var imageConfig ocispec.Image
	if err := json.Unmarshal(configBytes, &imageConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	if found < len(imageConfig.RootFS.DiffIDs) {
		imageConfig.RootFS.DiffIDs = append(imageConfig.RootFS.DiffIDs[:found], imageConfig.RootFS.DiffIDs[found+1:]...)
	}

	newConfigBytes, err := json.Marshal(imageConfig)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	newConfigDigest := digest.FromBytes(newConfigBytes)
	newConfigDesc := ocispec.Descriptor{
		MediaType: manifest.ConfigMediaType,
		Digest:    newConfigDigest,
		Size:      int64(len(newConfigBytes)),
	}
	if err := s.pushIfNotExists(newConfigDesc, newConfigBytes); err != nil {
		return fmt.Errorf("pushing config: %w", err)
	}
	m.Config = newConfigDesc

	newManifestBytes, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	newManifestDigest := digest.FromBytes(newManifestBytes)
	newManifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    newManifestDigest,
		Size:      int64(len(newManifestBytes)),
	}
	if err := s.pushIfNotExists(newManifestDesc, newManifestBytes); err != nil {
		return fmt.Errorf("pushing manifest: %w", err)
	}
	if err := s.oci.Tag(s.ctx, newManifestDesc, tag); err != nil {
		return fmt.Errorf("re-tagging %s: %w", tag, err)
	}

	return nil
}

// pushIfNotExists pushes a blob only if it doesn't already exist in the store.
func (s *LocalStore) pushIfNotExists(desc ocispec.Descriptor, data []byte) error {
	exists, err := s.oci.Exists(s.ctx, desc)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.oci.Push(s.ctx, desc, bytes.NewReader(data))
}

// pushLayerIfNotExists pushes a layer blob only if it doesn't already exist.
// Streams from a temp file (Path) or in-memory data (Data).
func (s *LocalStore) pushLayerIfNotExists(desc ocispec.Descriptor, ld LayerData) error {
	exists, err := s.oci.Exists(s.ctx, desc)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	rc, err := ld.NewReader()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return s.oci.Push(s.ctx, desc, rc)
}

// fetchBlob reads a complete blob by descriptor into memory and verifies its
// digest. Only use for small blobs (manifest, config). For layer blobs use
// fetchBlobToTemp.
func (s *LocalStore) fetchBlob(desc ocispec.Descriptor) ([]byte, error) {
	rc, err := s.oci.Fetch(s.ctx, desc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	// Verify integrity
	if actual := digest.FromBytes(data); actual != desc.Digest {
		return nil, fmt.Errorf("blob integrity check failed for %s: expected %s, got %s", desc.Digest, desc.Digest, actual)
	}

	return data, nil
}

// fetchBlobToTemp streams a blob to a temporary file and verifies its digest.
// Returns the path to the temp file. Callers must remove it when done.
func (s *LocalStore) fetchBlobToTemp(desc ocispec.Descriptor) (string, error) {
	rc, err := s.oci.Fetch(s.ctx, desc)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	tmpFile, err := os.CreateTemp("", "bento-layer-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, h), rc); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("streaming blob to temp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("closing temp file: %w", err)
	}

	// Verify digest
	actualDigest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if actualDigest != desc.Digest.String() {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("blob integrity check failed: expected %s, got %s", desc.Digest, actualDigest)
	}

	return tmpPath, nil
}
