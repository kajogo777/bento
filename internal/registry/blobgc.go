package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// GCLockFile is the name of the lock file that prevents concurrent blob GC.
const GCLockFile = ".gc-lock"

// BlobGCResult holds the outcome of a blob garbage collection run.
type BlobGCResult struct {
	// Deleted is the list of blob digests that were removed.
	Deleted []string
	// BytesFreed is the total bytes reclaimed.
	BytesFreed int64
}

// BlobGC removes orphaned blobs from the shared blob pool that are not
// referenced by any manifest in any workspace. This is Phase 2 of garbage
// collection — it should be run after per-workspace GC (Phase 1) has
// cleaned up old manifests from individual workspace indexes.
//
// storeRoot is the top-level store directory (e.g. ~/.bento/store/).
//
// Algorithm:
//  1. Acquire a lock file to prevent concurrent GC.
//  2. Walk all workspace index.json files to collect referenced blob digests
//     (manifests, configs, and layers).
//  3. Walk the shared blobs/sha256/ directory.
//  4. Delete any blob not in the referenced set.
func BlobGC(storeRoot string) (*BlobGCResult, error) {
	lockPath := filepath.Join(storeRoot, GCLockFile)

	// Acquire lock file.
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("blob GC already in progress (lock file %s exists). If no GC is running, remove it manually", lockPath)
		}
		return nil, fmt.Errorf("creating GC lock: %w", err)
	}
	_ = lockFile.Close()
	defer func() { _ = os.Remove(lockPath) }()

	// Collect all referenced digests across all workspaces.
	referenced, err := collectReferencedDigests(storeRoot)
	if err != nil {
		return nil, fmt.Errorf("collecting referenced digests: %w", err)
	}

	// Walk the shared blob pool and delete unreferenced blobs.
	blobDir := filepath.Join(storeRoot, "blobs", "sha256")
	entries, err := os.ReadDir(blobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &BlobGCResult{}, nil
		}
		return nil, fmt.Errorf("reading blob directory: %w", err)
	}

	result := &BlobGCResult{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		digest := "sha256:" + entry.Name()
		if referenced[digest] {
			continue
		}

		blobPath := filepath.Join(blobDir, entry.Name())
		info, statErr := entry.Info()
		if statErr == nil {
			result.BytesFreed += info.Size()
		}

		if err := os.Remove(blobPath); err != nil {
			return result, fmt.Errorf("removing blob %s: %w", digest, err)
		}
		result.Deleted = append(result.Deleted, digest)
	}

	return result, nil
}

// collectReferencedDigests walks all workspace directories under storeRoot,
// reads each workspace's index.json, and collects every blob digest referenced
// by any manifest (the manifest itself, its config, and all layers).
func collectReferencedDigests(storeRoot string) (map[string]bool, error) {
	referenced := make(map[string]bool)

	entries, err := os.ReadDir(storeRoot)
	if err != nil {
		return nil, fmt.Errorf("reading store root: %w", err)
	}

	for _, entry := range entries {
		// Skip non-directories and the shared blobs directory.
		if !entry.IsDir() || entry.Name() == "blobs" {
			continue
		}

		wsDir := filepath.Join(storeRoot, entry.Name())
		indexPath := filepath.Join(wsDir, "index.json")

		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			// Not a workspace directory (no index.json) — skip.
			continue
		}

		var idx ocispec.Index
		if err := json.Unmarshal(indexData, &idx); err != nil {
			continue
		}

		// Each tagged manifest descriptor in the index is a referenced blob.
		// Untagged descriptors (left behind by DeleteCheckpoint/Untag) are
		// considered orphaned and their blobs can be reclaimed.
		for _, desc := range idx.Manifests {
			// Only consider descriptors that still have a tag.
			if _, hasTag := desc.Annotations[ocispec.AnnotationRefName]; !hasTag {
				continue
			}

			manifestDigest := desc.Digest.String()
			referenced[manifestDigest] = true

			// Read the manifest to find config + layer digests.
			manifestPath := blobPathFromDigest(storeRoot, manifestDigest)
			manifestData, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}

			var m ocispec.Manifest
			if err := json.Unmarshal(manifestData, &m); err != nil {
				continue
			}

			// Config blob.
			referenced[m.Config.Digest.String()] = true

			// Layer blobs.
			for _, layer := range m.Layers {
				referenced[layer.Digest.String()] = true
			}
		}
	}

	return referenced, nil
}

// blobPathFromDigest converts a digest like "sha256:abc123..." to the
// filesystem path under the shared blob pool.
func blobPathFromDigest(storeRoot, digestStr string) string {
	// digest format: "sha256:<hex>"
	parts := strings.SplitN(digestStr, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return filepath.Join(storeRoot, "blobs", parts[0], parts[1])
}
