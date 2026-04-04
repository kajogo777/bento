package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/opencontainers/go-digest"
)

func TestEnsureSharedBlobLayout_CreatesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink assertions not applicable on Windows (uses junctions)")
	}
	storeRoot := t.TempDir()
	wsPath := filepath.Join(storeRoot, "ws-aaa")

	if err := ensureSharedBlobLayout(wsPath); err != nil {
		t.Fatalf("ensureSharedBlobLayout failed: %v", err)
	}

	// Shared blob dir should exist.
	sharedBlobDir := filepath.Join(storeRoot, "blobs", "sha256")
	if fi, err := os.Stat(sharedBlobDir); err != nil || !fi.IsDir() {
		t.Fatalf("shared blob dir not created: %v", err)
	}

	// Workspace blobs should be a symlink.
	wsBlobLink := filepath.Join(wsPath, "blobs")
	fi, err := os.Lstat(wsBlobLink)
	if err != nil {
		t.Fatalf("lstat blobs link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected blobs to be a symlink")
	}

	target, err := os.Readlink(wsBlobLink)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "../blobs" {
		t.Errorf("symlink target: got %q, want %q", target, "../blobs")
	}
}

func TestEnsureSharedBlobLayout_Idempotent(t *testing.T) {
	storeRoot := t.TempDir()
	wsPath := filepath.Join(storeRoot, "ws-aaa")

	// Call twice — should not error.
	if err := ensureSharedBlobLayout(wsPath); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := ensureSharedBlobLayout(wsPath); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

func TestEnsureSharedBlobLayout_MultipleWorkspaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink assertions not applicable on Windows (uses junctions)")
	}
	storeRoot := t.TempDir()
	wsA := filepath.Join(storeRoot, "ws-aaa")
	wsB := filepath.Join(storeRoot, "ws-bbb")

	if err := ensureSharedBlobLayout(wsA); err != nil {
		t.Fatalf("ws-aaa setup failed: %v", err)
	}
	if err := ensureSharedBlobLayout(wsB); err != nil {
		t.Fatalf("ws-bbb setup failed: %v", err)
	}

	// Both symlinks should resolve to the same shared blob dir.
	resolvedA, _ := filepath.EvalSymlinks(filepath.Join(wsA, "blobs"))
	resolvedB, _ := filepath.EvalSymlinks(filepath.Join(wsB, "blobs"))
	if resolvedA != resolvedB {
		t.Errorf("symlinks resolve to different dirs: %q vs %q", resolvedA, resolvedB)
	}
}

func TestSharedBlobStore_BlobDedup(t *testing.T) {
	storeRoot := t.TempDir()
	wsPathA := filepath.Join(storeRoot, "ws-aaa")
	wsPathB := filepath.Join(storeRoot, "ws-bbb")

	storeA, err := NewStore(wsPathA)
	if err != nil {
		t.Fatalf("NewStore ws-aaa: %v", err)
	}
	storeB, err := NewStore(wsPathB)
	if err != nil {
		t.Fatalf("NewStore ws-bbb: %v", err)
	}

	// Build a checkpoint with shared layer data.
	layerData := makeGzipLayer("shared-content")
	layerInfo := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerData, 1)

	cfg := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	manifestBytes, configBytes, err := manifest.BuildManifest(cfg, []manifest.LayerInfo{layerInfo})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	storeLayers := []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerData, Digest: layerInfo.GzipDigest, Size: layerInfo.Size},
	}

	// Save the same checkpoint to both stores.
	_, err = storeA.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, storeLayers)
	if err != nil {
		t.Fatalf("SaveCheckpoint ws-aaa: %v", err)
	}
	_, err = storeB.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, storeLayers)
	if err != nil {
		t.Fatalf("SaveCheckpoint ws-bbb: %v", err)
	}

	// The shared blob pool should have the blob stored only once.
	blobDir := filepath.Join(storeRoot, "blobs", "sha256")
	entries, err := os.ReadDir(blobDir)
	if err != nil {
		t.Fatalf("reading blob dir: %v", err)
	}

	// Count unique blobs: manifest + config + layer = 3 unique blobs.
	// Both workspaces wrote the same content, so no duplicates.
	if len(entries) != 3 {
		t.Errorf("expected 3 unique blobs (manifest, config, layer), got %d", len(entries))
	}

	// Verify both stores can load the checkpoint.
	_, _, layersA, err := storeA.LoadCheckpoint("cp-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint ws-aaa: %v", err)
	}
	for i := range layersA {
		layersA[i].Cleanup()
	}

	_, _, layersB, err := storeB.LoadCheckpoint("cp-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint ws-bbb: %v", err)
	}
	for i := range layersB {
		layersB[i].Cleanup()
	}
}

func TestBlobGC_PrunesOrphanedBlobs(t *testing.T) {
	storeRoot := t.TempDir()
	wsPath := filepath.Join(storeRoot, "ws-aaa")

	store, err := NewStore(wsPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Save a checkpoint.
	layerData := makeGzipLayer("gc-test-content")
	layerInfo := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerData, 1)

	cfg := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	manifestBytes, configBytes, err := manifest.BuildManifest(cfg, []manifest.LayerInfo{layerInfo})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	_, err = store.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerData, Digest: layerInfo.GzipDigest, Size: layerInfo.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Write an orphaned blob directly to the shared pool.
	orphanData := []byte("orphaned-blob-data")
	orphanDigest := digest.FromBytes(orphanData)
	orphanPath := filepath.Join(storeRoot, "blobs", "sha256", orphanDigest.Encoded())
	if err := os.WriteFile(orphanPath, orphanData, 0644); err != nil {
		t.Fatalf("writing orphan blob: %v", err)
	}

	// Run blob GC.
	result, err := BlobGC(storeRoot)
	if err != nil {
		t.Fatalf("BlobGC: %v", err)
	}

	// The orphan should be deleted.
	if len(result.Deleted) != 1 {
		t.Fatalf("expected 1 deleted blob, got %d: %v", len(result.Deleted), result.Deleted)
	}
	if result.Deleted[0] != orphanDigest.String() {
		t.Errorf("deleted digest: got %q, want %q", result.Deleted[0], orphanDigest.String())
	}
	if result.BytesFreed != int64(len(orphanData)) {
		t.Errorf("bytes freed: got %d, want %d", result.BytesFreed, len(orphanData))
	}

	// The referenced blobs should still exist — verify by loading.
	_, _, layers, err := store.LoadCheckpoint("cp-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint after GC: %v", err)
	}
	for i := range layers {
		layers[i].Cleanup()
	}
}

func TestBlobGC_NoOrphans(t *testing.T) {
	storeRoot := t.TempDir()
	wsPath := filepath.Join(storeRoot, "ws-aaa")

	store, err := NewStore(wsPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Save a checkpoint.
	layerData := makeGzipLayer("no-orphan-content")
	layerInfo := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerData, 1)

	cfg := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	manifestBytes, configBytes, err := manifest.BuildManifest(cfg, []manifest.LayerInfo{layerInfo})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	_, err = store.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerData, Digest: layerInfo.GzipDigest, Size: layerInfo.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Run blob GC — nothing should be deleted.
	result, err := BlobGC(storeRoot)
	if err != nil {
		t.Fatalf("BlobGC: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted blobs, got %d", len(result.Deleted))
	}
}

func TestBlobGC_LockPreventsConurrent(t *testing.T) {
	storeRoot := t.TempDir()

	// Create shared blob dir so BlobGC doesn't fail early.
	if err := os.MkdirAll(filepath.Join(storeRoot, "blobs", "sha256"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create lock file manually.
	lockPath := filepath.Join(storeRoot, GCLockFile)
	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// BlobGC should fail with lock error.
	_, err = BlobGC(storeRoot)
	if err == nil {
		t.Fatal("expected error due to lock file, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestBlobGC_EmptyStore(t *testing.T) {
	storeRoot := t.TempDir()

	// No blobs dir at all — should succeed with nothing deleted.
	result, err := BlobGC(storeRoot)
	if err != nil {
		t.Fatalf("BlobGC on empty store: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %d", len(result.Deleted))
	}
}

func TestBlobGC_CrossWorkspaceRetention(t *testing.T) {
	storeRoot := t.TempDir()
	wsPathA := filepath.Join(storeRoot, "ws-aaa")
	wsPathB := filepath.Join(storeRoot, "ws-bbb")

	storeA, err := NewStore(wsPathA)
	if err != nil {
		t.Fatalf("NewStore ws-aaa: %v", err)
	}
	storeB, err := NewStore(wsPathB)
	if err != nil {
		t.Fatalf("NewStore ws-bbb: %v", err)
	}

	// Save different checkpoints to each workspace.
	layerDataA := makeGzipLayer("content-a")
	layerInfoA := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerDataA, 1)
	cfgA := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	manifestA, configA, err := manifest.BuildManifest(cfgA, []manifest.LayerInfo{layerInfoA})
	if err != nil {
		t.Fatalf("BuildManifest A: %v", err)
	}

	layerDataB := makeGzipLayer("content-b")
	layerInfoB := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerDataB, 1)
	cfgB := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-16T10:00:00Z",
	}
	manifestB, configB, err := manifest.BuildManifest(cfgB, []manifest.LayerInfo{layerInfoB})
	if err != nil {
		t.Fatalf("BuildManifest B: %v", err)
	}

	_, err = storeA.SaveCheckpoint("test:cp-1", manifestA, configA, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerDataA, Digest: layerInfoA.GzipDigest, Size: layerInfoA.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint ws-aaa: %v", err)
	}

	_, err = storeB.SaveCheckpoint("test:cp-1", manifestB, configB, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerDataB, Digest: layerInfoB.GzipDigest, Size: layerInfoB.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint ws-bbb: %v", err)
	}

	// Delete checkpoint from ws-aaa's index.
	entriesA, _ := storeA.ListCheckpoints()
	for _, e := range entriesA {
		_ = storeA.DeleteCheckpoint(e.Digest)
	}

	// Run blob GC — ws-aaa's unique blobs should be pruned,
	// ws-bbb's blobs should be retained.
	result, err := BlobGC(storeRoot)
	if err != nil {
		t.Fatalf("BlobGC: %v", err)
	}

	// ws-aaa had unique blobs (manifest, config, layer) that are now orphaned = 3.
	if len(result.Deleted) != 3 {
		t.Errorf("expected 3 orphaned blobs from ws-aaa, got %d: %v", len(result.Deleted), result.Deleted)
	}

	// ws-bbb should still work.
	_, _, layersB, err := storeB.LoadCheckpoint("cp-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint ws-bbb after GC: %v", err)
	}
	for i := range layersB {
		layersB[i].Cleanup()
	}
}

// TestSharedBlobStore_SymlinkResolvesCorrectly verifies that blobs written
// through one workspace's symlink are readable through another workspace's symlink.
func TestSharedBlobStore_SymlinkResolvesCorrectly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink assertions not applicable on Windows (uses junctions)")
	}
	storeRoot := t.TempDir()
	wsPathA := filepath.Join(storeRoot, "ws-aaa")
	wsPathB := filepath.Join(storeRoot, "ws-bbb")

	storeA, err := NewStore(wsPathA)
	if err != nil {
		t.Fatalf("NewStore ws-aaa: %v", err)
	}

	// Save a checkpoint to ws-aaa.
	layerData := makeGzipLayer("cross-workspace-content")
	layerInfo := makeManifestLayerInfo("project", manifest.MediaTypeProject, layerData, 1)

	cfg := &manifest.BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    1,
		Created:       "2025-01-15T10:00:00Z",
	}
	manifestBytes, configBytes, err := manifest.BuildManifest(cfg, []manifest.LayerInfo{layerInfo})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	digestStr, err := storeA.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerData, Digest: layerInfo.GzipDigest, Size: layerInfo.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Now open ws-bbb and manually write the same index entry (simulating
	// what would happen if we tagged the same manifest in ws-bbb).
	storeB, err := NewStore(wsPathB)
	if err != nil {
		t.Fatalf("NewStore ws-bbb: %v", err)
	}

	// Save the same checkpoint to ws-bbb — blobs already exist in shared pool.
	_, err = storeB.SaveCheckpoint("test:cp-1", manifestBytes, configBytes, []LayerData{
		{MediaType: manifest.MediaTypeProject, Data: layerData, Digest: layerInfo.GzipDigest, Size: layerInfo.Size},
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint ws-bbb: %v", err)
	}

	// Both should resolve to the same digest.
	digestA, _ := storeA.ResolveTag("cp-1")
	digestB, _ := storeB.ResolveTag("cp-1")
	if digestA != digestStr || digestB != digestStr {
		t.Errorf("digests don't match: A=%q B=%q expected=%q", digestA, digestB, digestStr)
	}
}
