package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

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
func newLocalStore(storePath string) (*LocalStore, error) {
	ctx := context.Background()
	store, err := oci.NewWithContext(ctx, storePath)
	if err != nil {
		return nil, fmt.Errorf("opening OCI store at %s: %w", storePath, err)
	}
	store.AutoSaveIndex = true
	return &LocalStore{oci: store, ctx: ctx}, nil
}

// SaveCheckpoint writes a checkpoint's blobs (config, layers, manifest) and tags it.
func (s *LocalStore) SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []LayerData) (string, error) {
	_, tag, err := ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}

	// Push config blob
	configDigest := digest.FromBytes(configBytes)
	configDesc := ocispec.Descriptor{
		MediaType: "application/vnd.bento.config.v1+json",
		Digest:    configDigest,
		Size:      int64(len(configBytes)),
	}
	if err := s.pushIfNotExists(configDesc, configBytes); err != nil {
		return "", fmt.Errorf("pushing config: %w", err)
	}

	// Push layer blobs
	for _, ld := range layers {
		layerDigest := digest.FromBytes(ld.Data)
		layerDesc := ocispec.Descriptor{
			MediaType: ld.MediaType,
			Digest:    layerDigest,
			Size:      int64(len(ld.Data)),
		}
		if err := s.pushIfNotExists(layerDesc, ld.Data); err != nil {
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

// LoadCheckpoint reads a checkpoint by tag or digest.
func (s *LocalStore) LoadCheckpoint(ref string) (manifestBytes, configBytes []byte, layers []LayerData, err error) {
	// Resolve ref to descriptor
	desc, err := s.oci.Resolve(s.ctx, ref)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("tag %q not found", ref)
	}

	// Fetch manifest
	manifestBytes, err = s.fetchBlob(desc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Fetch config
	configBytes, err = s.fetchBlob(m.Config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading config: %w", err)
	}

	// Fetch layers
	layers = make([]LayerData, 0, len(m.Layers))
	for _, ld := range m.Layers {
		data, err := s.fetchBlob(ld)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading layer %s: %w", ld.Digest, err)
		}
		layers = append(layers, LayerData{
			MediaType: ld.MediaType,
			Data:      data,
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
			// Fetch manifest to read annotations
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
	// We need to iterate tags to find one that matches
	var found *ocispec.Descriptor
	_ = s.oci.Tags(s.ctx, "", func(tags []string) error {
		for _, t := range tags {
			desc, err := s.oci.Resolve(s.ctx, t)
			if err == nil && desc.Digest.String() == digestStr {
				found = &desc
				return fmt.Errorf("found") // break
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

// fetchBlob reads a complete blob by descriptor.
func (s *LocalStore) fetchBlob(desc ocispec.Descriptor) ([]byte, error) {
	rc, err := s.oci.Fetch(s.ctx, desc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
