package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/kajogo777/bento/internal/manifest"
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

func TestLocalStore_FullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Build a checkpoint.
	cfg := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Agent:         "claude",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
		Message:       "first checkpoint",
	}

	layerData := makeGzipLayer("hello-layer-content")
	layers := []manifest.LayerInfo{
		{
			Name:      "project",
			MediaType: manifest.MediaTypeProject,
			Data:      layerData,
			FileCount: 5,
		},
	}

	manifestBytes, configBytes, err := manifest.BuildManifest(cfg, layers)
	if err != nil {
		t.Fatalf("BuildManifest failed: %v", err)
	}

	storeLayers := []LayerData{
		{
			MediaType: manifest.MediaTypeProject,
			Data:      layerData,
		},
	}

	// SaveCheckpoint
	ref := "testproject:cp-1"
	digest, err := store.SaveCheckpoint(ref, manifestBytes, configBytes, storeLayers)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("digest should start with sha256:, got %q", digest)
	}
	if len(digest) != 7+64 { // "sha256:" + 64 hex chars
		t.Errorf("digest length: got %d, want %d", len(digest), 7+64)
	}

	// ListCheckpoints
	entries, err := store.ListCheckpoints()
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListCheckpoints: got %d entries, want 1", len(entries))
	}
	if entries[0].Digest != digest {
		t.Errorf("entry digest: got %q, want %q", entries[0].Digest, digest)
	}
	if entries[0].Tag != "cp-1" {
		t.Errorf("entry tag: got %q, want %q", entries[0].Tag, "cp-1")
	}
	if entries[0].Created != "2025-01-15T10:00:00Z" {
		t.Errorf("entry created: got %q, want %q", entries[0].Created, "2025-01-15T10:00:00Z")
	}
	if entries[0].Message != "first checkpoint" {
		t.Errorf("entry message: got %q, want %q", entries[0].Message, "first checkpoint")
	}

	// LoadCheckpoint
	loadedManifest, loadedConfig, loadedLayers, err := store.LoadCheckpoint("sha256:" + strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	// Verify manifest roundtrip.
	if string(loadedManifest) != string(manifestBytes) {
		t.Error("loaded manifest does not match saved manifest")
	}

	// Verify config roundtrip.
	loadedCfg, err := manifest.UnmarshalConfig(loadedConfig)
	if err != nil {
		t.Fatalf("failed to parse loaded config: %v", err)
	}
	if loadedCfg.Agent != "claude" {
		t.Errorf("loaded config agent: got %q, want %q", loadedCfg.Agent, "claude")
	}
	if loadedCfg.Checkpoint != 1 {
		t.Errorf("loaded config checkpoint: got %d, want 1", loadedCfg.Checkpoint)
	}

	// Verify layer data roundtrip. Layers are file-backed; read via NewReader().
	if len(loadedLayers) != 1 {
		t.Fatalf("loaded layers count: got %d, want 1", len(loadedLayers))
	}
	defer loadedLayers[0].Cleanup()
	r, err := loadedLayers[0].NewReader()
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	loadedData, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("reading layer: %v", err)
	}
	if string(loadedData) != string(layerData) {
		t.Errorf("loaded layer data mismatch")
	}
	if loadedLayers[0].MediaType != manifest.MediaTypeProject {
		t.Errorf("loaded layer mediaType: got %q, want %q", loadedLayers[0].MediaType, manifest.MediaTypeProject)
	}

	// ResolveTag
	resolvedDigest, err := store.ResolveTag("cp-1")
	if err != nil {
		t.Fatalf("ResolveTag failed: %v", err)
	}
	if resolvedDigest != digest {
		t.Errorf("ResolveTag: got %q, want %q", resolvedDigest, digest)
	}

	// Tag - create a new tag pointing to the same digest.
	if err := store.Tag(digest, "latest"); err != nil {
		t.Fatalf("Tag failed: %v", err)
	}

	latestDigest, err := store.ResolveTag("latest")
	if err != nil {
		t.Fatalf("ResolveTag(latest) failed: %v", err)
	}
	if latestDigest != digest {
		t.Errorf("latest digest: got %q, want %q", latestDigest, digest)
	}

	// Verify we now have 2 entries in the index.
	entries, err = store.ListCheckpoints()
	if err != nil {
		t.Fatalf("ListCheckpoints after tag failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListCheckpoints: got %d entries, want 2", len(entries))
	}

	// DeleteCheckpoint - delete by digest removes ALL entries with that digest.
	if err := store.DeleteCheckpoint(digest); err != nil {
		t.Fatalf("DeleteCheckpoint failed: %v", err)
	}

	entries, err = store.ListCheckpoints()
	if err != nil {
		t.Fatalf("ListCheckpoints after delete failed: %v", err)
	}
	// DeleteCheckpoint removes entries matching the digest. Since Tag creates a
	// separate descriptor, only one entry (the original cp-1) is removed.
	// The "latest" entry has the same digest, so it is also removed by the loop.
	// Actually, looking at the code: DeleteCheckpoint loops through all descriptors
	// and removes those matching the digest. Both cp-1 and latest have the same digest,
	// so both are removed.
	if len(entries) != 0 {
		t.Errorf("ListCheckpoints after delete: got %d entries, want 0", len(entries))
	}
}

func TestLocalStore_ResolveTag_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	_, err = store.ResolveTag("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tag, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestLocalStore_DeleteCheckpoint_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	err = store.DeleteCheckpoint("sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent digest, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestLocalStore_TagOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Save two different checkpoints.
	cfg1 := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	cfg2 := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    2,
		Created:       "2025-01-16T10:00:00Z",
	}

	m1, c1, _ := manifest.BuildManifest(cfg1, nil)
	m2, c2, _ := manifest.BuildManifest(cfg2, nil)

	digest1, err := store.SaveCheckpoint("testproject:cp-1", m1, c1, nil)
	if err != nil {
		t.Fatalf("SaveCheckpoint 1 failed: %v", err)
	}

	digest2, err := store.SaveCheckpoint("testproject:cp-2", m2, c2, nil)
	if err != nil {
		t.Fatalf("SaveCheckpoint 2 failed: %v", err)
	}

	// Tag both as "latest" - second should overwrite first.
	if err := store.Tag(digest1, "latest"); err != nil {
		t.Fatalf("Tag digest1 as latest failed: %v", err)
	}
	if err := store.Tag(digest2, "latest"); err != nil {
		t.Fatalf("Tag digest2 as latest failed: %v", err)
	}

	resolved, err := store.ResolveTag("latest")
	if err != nil {
		t.Fatalf("ResolveTag(latest) failed: %v", err)
	}
	if resolved != digest2 {
		t.Errorf("latest should point to digest2: got %q, want %q", resolved, digest2)
	}
}
