package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"
)

// makeGzipLayer creates a minimal valid gzip-compressed tar archive for testing.
func makeGzipLayer(content string) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "test.txt", Size: int64(len(content)), Mode: 0644})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

func TestBuildManifest(t *testing.T) {
	cfg := &BentoConfigObj{
		SchemaVersion:    "1.0",
		Agent:            "claude",
		AgentVersion:     "3.5",
		Task:             "build-feature",
		SessionID:        "sess-abc",
		ParentCheckpoint: "sha256:parent",
		Checkpoint:       3,
		Created:          "2025-01-15T10:00:00Z",
		Harness:          "docker",
		Message:          "third checkpoint",
	}

	layers := []LayerInfo{
		{
			Name:      "project",
			MediaType: MediaTypeProject,
			Data:      makeGzipLayer("project-data"),
			FileCount: 42,
		},
		{
			Name:      "deps",
			MediaType: MediaTypeDeps,
			Data:      makeGzipLayer("deps-data"),
		},
	}

	manifestBytes, configBytes, err := BuildManifest(cfg, layers)
	if err != nil {
		t.Fatalf("BuildManifest failed: %v", err)
	}

	// Verify manifest JSON structure.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("failed to parse manifest JSON: %v", err)
	}

	// Check schemaVersion.
	var schemaVersion int
	if err := json.Unmarshal(m["schemaVersion"], &schemaVersion); err != nil {
		t.Fatalf("failed to parse schemaVersion: %v", err)
	}
	if schemaVersion != 2 {
		t.Errorf("schemaVersion: got %d, want 2", schemaVersion)
	}

	// Check artifactType.
	var artifactType string
	if err := json.Unmarshal(m["artifactType"], &artifactType); err != nil {
		t.Fatalf("failed to parse artifactType: %v", err)
	}
	if artifactType != ArtifactType {
		t.Errorf("artifactType: got %q, want %q", artifactType, ArtifactType)
	}

	// Check layers count.
	var layerDescs []json.RawMessage
	if err := json.Unmarshal(m["layers"], &layerDescs); err != nil {
		t.Fatalf("failed to parse layers: %v", err)
	}
	if len(layerDescs) != 2 {
		t.Errorf("layers count: got %d, want 2", len(layerDescs))
	}

	// Check annotations.
	var annotations map[string]string
	if err := json.Unmarshal(m["annotations"], &annotations); err != nil {
		t.Fatalf("failed to parse annotations: %v", err)
	}
	if annotations[AnnotationCreated] != "2025-01-15T10:00:00Z" {
		t.Errorf("annotation created: got %q, want %q", annotations[AnnotationCreated], "2025-01-15T10:00:00Z")
	}
	if annotations[AnnotationCheckpointSeq] != "3" {
		t.Errorf("annotation checkpoint seq: got %q, want %q", annotations[AnnotationCheckpointSeq], "3")
	}
	if annotations[AnnotationCheckpointParent] != "sha256:parent" {
		t.Errorf("annotation parent: got %q, want %q", annotations[AnnotationCheckpointParent], "sha256:parent")
	}
	if annotations[AnnotationAgent] != "claude" {
		t.Errorf("annotation agent: got %q, want %q", annotations[AnnotationAgent], "claude")
	}
	if annotations[AnnotationTask] != "build-feature" {
		t.Errorf("annotation task: got %q, want %q", annotations[AnnotationTask], "build-feature")
	}
	if annotations[AnnotationHarness] != "docker" {
		t.Errorf("annotation harness: got %q, want %q", annotations[AnnotationHarness], "docker")
	}
	if annotations[AnnotationCheckpointMessage] != "third checkpoint" {
		t.Errorf("annotation message: got %q, want %q", annotations[AnnotationCheckpointMessage], "third checkpoint")
	}
	if annotations[AnnotationFormatVersion] != FormatVersion {
		t.Errorf("annotation format version: got %q, want %q", annotations[AnnotationFormatVersion], FormatVersion)
	}

	// Verify config JSON contains correct fields.
	cfgParsed, err := UnmarshalConfig(configBytes)
	if err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}
	if cfgParsed.Agent != "claude" {
		t.Errorf("config agent: got %q, want %q", cfgParsed.Agent, "claude")
	}
	if cfgParsed.Checkpoint != 3 {
		t.Errorf("config checkpoint: got %d, want 3", cfgParsed.Checkpoint)
	}
	if cfgParsed.Task != "build-feature" {
		t.Errorf("config task: got %q, want %q", cfgParsed.Task, "build-feature")
	}
}

func TestBuildManifest_NoLayers(t *testing.T) {
	cfg := &BentoConfigObj{
		SchemaVersion: "1.0",
		Created:       "2025-01-15T10:00:00Z",
		Checkpoint:    0,
	}

	manifestBytes, _, err := BuildManifest(cfg, nil)
	if err != nil {
		t.Fatalf("BuildManifest with no layers failed: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("failed to parse manifest JSON: %v", err)
	}

	var layerDescs []json.RawMessage
	if err := json.Unmarshal(m["layers"], &layerDescs); err != nil {
		t.Fatalf("failed to parse layers: %v", err)
	}
	if len(layerDescs) != 0 {
		t.Errorf("expected 0 layers, got %d", len(layerDescs))
	}
}

func TestBuildManifest_LayerAnnotations(t *testing.T) {
	cfg := &BentoConfigObj{
		SchemaVersion: "1.0",
		Created:       "2025-01-15T10:00:00Z",
		Checkpoint:    1,
	}

	layers := []LayerInfo{
		{
			Name:      "project",
			MediaType: MediaTypeProject,
			Data:      makeGzipLayer("data"),
			FileCount: 10,
		},
	}

	manifestBytes, _, err := BuildManifest(cfg, layers)
	if err != nil {
		t.Fatalf("BuildManifest failed: %v", err)
	}

	// Parse the full manifest to check layer annotations.
	var m struct {
		Layers []struct {
			MediaType   string            `json:"mediaType"`
			Annotations map[string]string `json:"annotations"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}
	if len(m.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(m.Layers))
	}
	layer := m.Layers[0]
	if layer.MediaType != MediaTypeProject {
		t.Errorf("layer mediaType: got %q, want %q", layer.MediaType, MediaTypeProject)
	}
	if layer.Annotations[AnnotationTitle] != "project" {
		t.Errorf("layer title: got %q, want %q", layer.Annotations[AnnotationTitle], "project")
	}
	if layer.Annotations[AnnotationLayerFileCount] != "10" {
		t.Errorf("layer file count: got %q, want %q", layer.Annotations[AnnotationLayerFileCount], "10")
	}
}
