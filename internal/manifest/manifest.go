package manifest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// LayerInfo describes a single layer to include in the manifest.
type LayerInfo struct {
	Name        string
	MediaType   string
	Data        []byte
	FileCount   int
	Annotations map[string]string // extra annotations merged into the layer descriptor
}

// BuildManifest constructs an OCI image manifest with a Docker-compatible config.
// The config is a valid OCI image config with bento metadata in Labels.
// Layers use the standard OCI layer media type for Docker/containerd compatibility.
func BuildManifest(cfg *BentoConfigObj, layers []LayerInfo) (manifestBytes []byte, configBytes []byte, err error) {
	// Build diff_ids: OCI requires the digest of the UNCOMPRESSED layer content.
	// We decompress each layer to compute the correct uncompressed digest.
	// Docker uses diff_ids for layer deduplication and verification.
	diffIDs := make([]digest.Digest, len(layers))
	for i, l := range layers {
		id, err := uncompressedLayerDigest(l.Data)
		if err != nil {
			return nil, nil, fmt.Errorf("computing diff_id for layer %s: %w", l.Name, err)
		}
		diffIDs[i] = id
	}

	// Build OCI image config with bento metadata in Labels.
	// This makes the artifact a valid OCI image that Docker can pull and extract.
	// Use linux/amd64 as the OCI platform regardless of build host.
	// Bento layers are OS-agnostic filesystem archives, but Docker requires
	// a linux platform to accept the image for COPY --from and extraction.
	os := "linux"
	arch := "amd64"

	labels := map[string]string{
		AnnotationFormatVersion: FormatVersion,
	}
	if cfg.Agent != "" {
		labels[AnnotationAgent] = cfg.Agent
	}
	if cfg.Task != "" {
		labels[AnnotationTask] = cfg.Task
	}
	if cfg.Harness != "" {
		labels[AnnotationHarness] = cfg.Harness
	}
	if cfg.Message != "" {
		labels[AnnotationCheckpointMessage] = cfg.Message
	}
	if cfg.ParentCheckpoint != "" {
		labels[AnnotationCheckpointParent] = cfg.ParentCheckpoint
	}
	labels[AnnotationCheckpointSeq] = strconv.Itoa(cfg.Checkpoint)

	// Store full bento config as a label for lossless round-trip
	bentoJSON, _ := json.Marshal(cfg)
	labels["dev.bento.config"] = string(bentoJSON)

	imageConfig := ocispec.Image{
		Platform: ocispec.Platform{
			Architecture: arch,
			OS:           os,
		},
		RootFS: ocispec.RootFS{
			Type:    "layers",
			DiffIDs: diffIDs,
		},
		Config: ocispec.ImageConfig{
			Labels: labels,
		},
	}
	if cfg.Created != "" {
		if t, err := time.Parse(time.RFC3339, cfg.Created); err == nil {
			imageConfig.Created = &t
		}
	}

	configBytes, err = json.Marshal(imageConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal config: %w", err)
	}

	// Build config descriptor.
	configDesc := ocispec.Descriptor{
		MediaType: ConfigMediaType,
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}

	// Build layer descriptors with standard OCI media type.
	// Layer semantics are carried by annotations.
	layerDescs := make([]ocispec.Descriptor, 0, len(layers))
	for _, l := range layers {
		annotations := map[string]string{
			AnnotationTitle: l.Name,
		}
		if l.FileCount > 0 {
			annotations[AnnotationLayerFileCount] = strconv.Itoa(l.FileCount)
		}
		for k, v := range l.Annotations {
			annotations[k] = v
		}

		layerDescs = append(layerDescs, ocispec.Descriptor{
			MediaType:   LayerMediaType,
			Digest:      digest.FromBytes(l.Data),
			Size:        int64(len(l.Data)),
			Annotations: annotations,
		})
	}

	// Build manifest-level annotations.
	manifestAnnotations := map[string]string{
		AnnotationFormatVersion: FormatVersion,
	}
	if cfg.Created != "" {
		manifestAnnotations[AnnotationCreated] = cfg.Created
	}
	if cfg.Checkpoint >= 0 {
		manifestAnnotations[AnnotationCheckpointSeq] = strconv.Itoa(cfg.Checkpoint)
	}
	if cfg.ParentCheckpoint != "" {
		manifestAnnotations[AnnotationCheckpointParent] = cfg.ParentCheckpoint
	}
	if cfg.Agent != "" {
		manifestAnnotations[AnnotationAgent] = cfg.Agent
	}
	if cfg.Task != "" {
		manifestAnnotations[AnnotationTask] = cfg.Task
	}
	if cfg.Harness != "" {
		manifestAnnotations[AnnotationHarness] = cfg.Harness
	}
	if cfg.Message != "" {
		manifestAnnotations[AnnotationCheckpointMessage] = cfg.Message
	}

	manifest := ocispec.Manifest{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageManifest,
		ArtifactType: ArtifactType,
		Config:      configDesc,
		Layers:      layerDescs,
		Annotations: manifestAnnotations,
	}

	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal manifest: %w", err)
	}

	return manifestBytes, configBytes, nil
}

// uncompressedLayerDigest decompresses a gzip-compressed layer and returns the
// SHA256 digest of the uncompressed content. This is required by the OCI image
// spec: diff_ids must be digests of uncompressed layer tar archives.
func uncompressedLayerDigest(compressedData []byte) (digest.Digest, error) {
	gr, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, gr); err != nil {
		return "", fmt.Errorf("decompressing layer: %w", err)
	}

	return digest.NewDigestFromHex("sha256", fmt.Sprintf("%x", h.Sum(nil))), nil
}
