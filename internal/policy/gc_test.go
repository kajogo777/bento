package policy

import (
	"fmt"
	"testing"
	"time"

	"github.com/kajogo777/bento/internal/registry"
)

// mockStore implements registry.Store for testing GC logic.
type mockStore struct {
	entries []registry.CheckpointEntry
	deleted []string
}

func (m *mockStore) SaveCheckpoint(ref string, manifestBytes, configBytes []byte, layers []registry.LayerData) (string, error) {
	return "", nil
}

func (m *mockStore) LoadCheckpoint(ref string) ([]byte, []byte, []registry.LayerData, error) {
	return nil, nil, nil, nil
}

func (m *mockStore) LoadManifest(ref string) ([]byte, []byte, error) {
	return nil, nil, nil
}

func (m *mockStore) ListCheckpoints() ([]registry.CheckpointEntry, error) {
	return m.entries, nil
}

func (m *mockStore) ResolveTag(tag string) (string, error) {
	return "", fmt.Errorf("not found")
}

func (m *mockStore) Tag(digest, tag string) error {
	return nil
}

func (m *mockStore) DeleteCheckpoint(digest string) error {
	m.deleted = append(m.deleted, digest)
	return nil
}

func makeEntries(count int) []registry.CheckpointEntry {
	entries := make([]registry.CheckpointEntry, count)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		entries[i] = registry.CheckpointEntry{
			Digest:  fmt.Sprintf("sha256:digest%d", i),
			Created: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		}
	}
	return entries
}

func TestGarbageCollect_KeepLastN(t *testing.T) {
	entries := makeEntries(5)
	store := &mockStore{entries: entries}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 2, KeepTagged: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 5 entries, keep 2 newest -> 3 deleted
	if len(deleted) != 3 {
		t.Fatalf("expected 3 deleted, got %d: %v", len(deleted), deleted)
	}

	// Verify the newest 2 are NOT deleted (digest3 and digest4 are newest).
	deletedSet := make(map[string]bool)
	for _, d := range deleted {
		deletedSet[d] = true
	}
	if deletedSet["sha256:digest4"] {
		t.Error("newest entry should not be deleted")
	}
	if deletedSet["sha256:digest3"] {
		t.Error("second newest entry should not be deleted")
	}
}

func TestGarbageCollect_KeepTagged(t *testing.T) {
	entries := makeEntries(5)
	// Tag the oldest entry.
	entries[0].Tag = "important"

	store := &mockStore{entries: entries}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 2, KeepTagged: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 5 entries, keep 2 newest + 1 tagged = 3 kept -> 2 deleted
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted, got %d: %v", len(deleted), deleted)
	}

	deletedSet := make(map[string]bool)
	for _, d := range deleted {
		deletedSet[d] = true
	}
	if deletedSet["sha256:digest0"] {
		t.Error("tagged entry should not be deleted")
	}
}

func TestGarbageCollect_KeepTaggedFalse(t *testing.T) {
	entries := makeEntries(5)
	entries[0].Tag = "important"

	store := &mockStore{entries: entries}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 2, KeepTagged: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without KeepTagged, the tagged entry is also eligible for deletion.
	if len(deleted) != 3 {
		t.Fatalf("expected 3 deleted, got %d: %v", len(deleted), deleted)
	}

	deletedSet := make(map[string]bool)
	for _, d := range deleted {
		deletedSet[d] = true
	}
	if !deletedSet["sha256:digest0"] {
		t.Error("tagged entry should be deleted when KeepTagged is false")
	}
}

func TestGarbageCollect_KeepAll(t *testing.T) {
	entries := makeEntries(3)
	store := &mockStore{entries: entries}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 10, KeepTagged: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected nothing deleted when KeepLast > total, got %d", len(deleted))
	}
}

func TestGarbageCollect_EmptyStore(t *testing.T) {
	store := &mockStore{entries: nil}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 2, KeepTagged: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected nothing deleted for empty store, got %d", len(deleted))
	}
}

func TestGarbageCollect_AllTaggedKept(t *testing.T) {
	entries := makeEntries(4)
	// Tag all entries.
	for i := range entries {
		entries[i].Tag = fmt.Sprintf("tag%d", i)
	}

	store := &mockStore{entries: entries}

	deleted, err := GarbageCollect(store, GCOptions{KeepLast: 1, KeepTagged: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All are tagged and KeepTagged=true, so nothing deleted.
	if len(deleted) != 0 {
		t.Fatalf("expected nothing deleted when all are tagged, got %d", len(deleted))
	}
}
