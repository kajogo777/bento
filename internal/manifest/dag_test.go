package manifest

import (
	"encoding/json"
	"testing"
)

func TestParseCheckpointInfo(t *testing.T) {
	// Build a valid manifest using BuildManifest so the JSON is realistic.
	cfg := &BentoConfigObj{
		SchemaVersion:    "1.0",
		Agent:            "claude",
		Task:             "build-feature",
		ParentCheckpoint: "sha256:parentdigest",
		Checkpoint:       7,
		Created:          "2025-02-01T12:00:00Z",
		Message:          "seventh checkpoint",
	}

	manifestBytes, _, err := BuildManifest(cfg, nil)
	if err != nil {
		t.Fatalf("BuildManifest failed: %v", err)
	}

	info, err := ParseCheckpointInfo(manifestBytes)
	if err != nil {
		t.Fatalf("ParseCheckpointInfo failed: %v", err)
	}

	if info.Sequence != 7 {
		t.Errorf("Sequence: got %d, want 7", info.Sequence)
	}
	if info.Parent != "sha256:parentdigest" {
		t.Errorf("Parent: got %q, want %q", info.Parent, "sha256:parentdigest")
	}
	if info.Message != "seventh checkpoint" {
		t.Errorf("Message: got %q, want %q", info.Message, "seventh checkpoint")
	}
	if info.Created != "2025-02-01T12:00:00Z" {
		t.Errorf("Created: got %q, want %q", info.Created, "2025-02-01T12:00:00Z")
	}
	if info.Agent != "claude" {
		t.Errorf("Agent: got %q, want %q", info.Agent, "claude")
	}
}

func TestParseCheckpointInfo_NoAnnotations(t *testing.T) {
	// A minimal manifest with no annotations.
	m := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.bento.config.v1+json",
			"digest":    "sha256:abc",
			"size":      10,
		},
		"layers": []interface{}{},
	}
	data, _ := json.Marshal(m)

	info, err := ParseCheckpointInfo(data)
	if err != nil {
		t.Fatalf("ParseCheckpointInfo failed: %v", err)
	}

	if info.Sequence != 0 {
		t.Errorf("Sequence: got %d, want 0", info.Sequence)
	}
	if info.Parent != "" {
		t.Errorf("Parent: got %q, want empty", info.Parent)
	}
	if info.Message != "" {
		t.Errorf("Message: got %q, want empty", info.Message)
	}
	if info.Created != "" {
		t.Errorf("Created: got %q, want empty", info.Created)
	}
	if info.Agent != "" {
		t.Errorf("Agent: got %q, want empty", info.Agent)
	}
}

func TestParseCheckpointInfo_InvalidJSON(t *testing.T) {
	_, err := ParseCheckpointInfo([]byte("not valid json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseCheckpointInfo_InvalidSequence(t *testing.T) {
	m := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.bento.config.v1+json",
			"digest":    "sha256:abc",
			"size":      10,
		},
		"layers": []interface{}{},
		"annotations": map[string]string{
			AnnotationCheckpointSeq: "not-a-number",
		},
	}
	data, _ := json.Marshal(m)

	_, err := ParseCheckpointInfo(data)
	if err == nil {
		t.Fatal("expected error for invalid sequence, got nil")
	}
}
