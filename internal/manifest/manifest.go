package manifest

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// LayerInfo describes a single layer to include in the manifest.
type LayerInfo struct {
	Name      string
	MediaType string
	Data      []byte
	FileCount int
	Frequency string
}

// BuildManifest constructs an OCI manifest and serialized config for a checkpoint.
// It returns the JSON-encoded manifest and config bytes.
func BuildManifest(cfg *BentoConfigObj, layers []LayerInfo) (manifestBytes []byte, configBytes []byte, err error) {
	// Marshal the config object.
	configBytes, err = json.Marshal(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal config: %w", err)
	}

	// Build config descriptor.
	configDesc := ocispec.Descriptor{
		MediaType: ConfigMediaType,
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}

	// Build layer descriptors.
	layerDescs := make([]ocispec.Descriptor, 0, len(layers))
	for _, l := range layers {
		annotations := map[string]string{
			AnnotationTitle: l.Name,
		}
		if l.FileCount > 0 {
			annotations[AnnotationLayerFileCount] = strconv.Itoa(l.FileCount)
		}
		if l.Frequency != "" {
			annotations[AnnotationLayerChangeFreq] = l.Frequency
		}

		layerDescs = append(layerDescs, ocispec.Descriptor{
			MediaType:   l.MediaType,
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
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType:  ArtifactType,
		Config:        configDesc,
		Layers:        layerDescs,
		Annotations:   manifestAnnotations,
	}

	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal manifest: %w", err)
	}

	return manifestBytes, configBytes, nil
}
