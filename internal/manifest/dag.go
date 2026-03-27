package manifest

import (
	"encoding/json"
	"fmt"
	"strconv"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// CheckpointInfo holds the metadata extracted from a manifest's annotations.
type CheckpointInfo struct {
	Digest   string
	Tag      string
	Sequence int
	Parent   string
	Message  string
	Created  string
	Agent    string
}

// ParseCheckpointInfo extracts checkpoint metadata from a serialized OCI manifest.
func ParseCheckpointInfo(manifestBytes []byte) (*CheckpointInfo, error) {
	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	info := &CheckpointInfo{}

	if m.Annotations == nil {
		return info, nil
	}

	info.Created = m.Annotations[AnnotationCreated]
	info.Parent = m.Annotations[AnnotationCheckpointParent]
	info.Message = m.Annotations[AnnotationCheckpointMessage]
	info.Agent = m.Annotations[AnnotationAgent]

	if seqStr, ok := m.Annotations[AnnotationCheckpointSeq]; ok {
		seq, err := strconv.Atoi(seqStr)
		if err != nil {
			return nil, fmt.Errorf("parse checkpoint sequence %q: %w", seqStr, err)
		}
		info.Sequence = seq
	}

	return info, nil
}
