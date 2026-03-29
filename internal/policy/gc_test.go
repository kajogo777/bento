package policy

import (
	"fmt"
	"testing"
	"time"

	"github.com/kajogo777/bento/internal/config"
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

// --- TieredGC tests ---
// CRITICAL: These tests protect against accidental data deletion.
// Every edge case must be covered. When in doubt, keep data.

// makeTimedEntries creates entries with specific ages relative to now.
func makeTimedEntries(ages []time.Duration, tags []string) []registry.CheckpointEntry {
	now := time.Now()
	entries := make([]registry.CheckpointEntry, len(ages))
	for i, age := range ages {
		entries[i] = registry.CheckpointEntry{
			Digest:  fmt.Sprintf("sha256:tiered%d", i),
			Created: now.Add(-age).Format(time.RFC3339),
		}
		if i < len(tags) {
			entries[i].Tag = tags[i]
		}
	}
	return entries
}

// dur is a test helper that returns a pointer to a duration.
func dur(d time.Duration) *time.Duration { return &d }

// assertNothingDeleted fails if any checkpoints were deleted.
func assertNothingDeleted(t *testing.T, deleted []string, context string) {
	t.Helper()
	if len(deleted) != 0 {
		t.Errorf("%s: expected 0 deleted, got %d: %v", context, len(deleted), deleted)
	}
}

// assertDeleted fails if the deleted list doesn't match the expected digests.
func assertDeleted(t *testing.T, deleted []string, expected []string, context string) {
	t.Helper()
	if len(deleted) != len(expected) {
		t.Errorf("%s: expected %d deleted, got %d: %v", context, len(expected), len(deleted), deleted)
		return
	}
	dset := make(map[string]bool)
	for _, d := range deleted {
		dset[d] = true
	}
	for _, e := range expected {
		if !dset[e] {
			t.Errorf("%s: expected %s to be deleted, but it wasn't. deleted=%v", context, e, deleted)
		}
	}
}

// assertNotDeleted fails if any of the given digests were deleted.
func assertNotDeleted(t *testing.T, deleted []string, protected []string, context string) {
	t.Helper()
	dset := make(map[string]bool)
	for _, d := range deleted {
		dset[d] = true
	}
	for _, p := range protected {
		if dset[p] {
			t.Errorf("%s: %s should NOT be deleted, but it was", context, p)
		}
	}
}

// =========================================================================
// 1. EMPTY / NIL INPUTS — must never crash or delete anything
// =========================================================================

func TestTieredGC_EmptyStore(t *testing.T) {
	store := &mockStore{entries: nil}
	deleted, err := TieredGC(store, DefaultWatchTiers, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "empty store")
}

func TestTieredGC_NilTiers(t *testing.T) {
	// No tiers defined = no policy = everything is outside scope = keep all.
	entries := makeTimedEntries(
		[]time.Duration{5 * time.Minute, 1 * time.Hour, 2 * time.Hour, 30 * 24 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "nil tiers")
}

func TestTieredGC_EmptyTiersList(t *testing.T) {
	entries := makeTimedEntries(
		[]time.Duration{5 * time.Minute, 1 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, []config.RetentionTier{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "empty tiers list")
}

// =========================================================================
// 2. RESOLUTION OMITTED (nil) — keep all checkpoints in the tier
// =========================================================================

func TestTieredGC_ResolutionOmitted_KeepsAll(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour}, // resolution nil = keep all
	}
	entries := makeTimedEntries(
		[]time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 55 * time.Minute},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "resolution omitted")
}

// =========================================================================
// 3. RESOLUTION 0 — delete all checkpoints in the tier
// =========================================================================

func TestTieredGC_ResolutionZero_DeletesAll(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour},          // keep all <1h
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)}, // delete everything >1h
	}
	entries := makeTimedEntries(
		[]time.Duration{30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 24 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tiered0 (30m) is in keep-all tier — kept.
	// tiered1, tiered2, tiered3 are in resolution:0 tier — deleted.
	assertDeleted(t, deleted, []string{"sha256:tiered1", "sha256:tiered2", "sha256:tiered3"}, "resolution zero")
	assertNotDeleted(t, deleted, []string{"sha256:tiered0"}, "resolution zero")
}

func TestTieredGC_ResolutionZero_ProtectsUserTags(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)}, // delete everything
	}
	entries := makeTimedEntries(
		[]time.Duration{1 * time.Hour, 2 * time.Hour, 3 * time.Hour},
		[]string{"", "v1-release", ""},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, true) // keepTagged = true
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tiered1 has user tag — keep always wins over remove.
	assertDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered2"}, "resolution zero + user tags")
	assertNotDeleted(t, deleted, []string{"sha256:tiered1"}, "resolution zero + user tags")
}

// =========================================================================
// 4. RESOLUTION > 0 — keep one per bucket (newest in each interval)
// =========================================================================

func TestTieredGC_HourlyResolution(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 24 * time.Hour, Resolution: dur(1 * time.Hour)},
	}
	entries := makeTimedEntries(
		[]time.Duration{
			1*time.Hour + 10*time.Minute, // bucket 1 — newest, kept
			1*time.Hour + 30*time.Minute, // bucket 1 — older, deleted
			2*time.Hour + 5*time.Minute,  // bucket 2 — newest, kept
			2*time.Hour + 45*time.Minute, // bucket 2 — older, deleted
			3*time.Hour + 10*time.Minute, // bucket 3 — newest, kept
			3*time.Hour + 50*time.Minute, // bucket 3 — older, deleted
		},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeleted(t, deleted,
		[]string{"sha256:tiered1", "sha256:tiered3", "sha256:tiered5"},
		"hourly resolution")
	assertNotDeleted(t, deleted,
		[]string{"sha256:tiered0", "sha256:tiered2", "sha256:tiered4"},
		"hourly resolution")
}

func TestTieredGC_SingleEntryPerBucket_NoDeletes(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 24 * time.Hour, Resolution: dur(1 * time.Hour)},
	}
	entries := makeTimedEntries(
		[]time.Duration{1*time.Hour + 10*time.Minute, 3*time.Hour + 10*time.Minute, 5*time.Hour + 10*time.Minute},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "one per bucket")
}

// =========================================================================
// 5. BEYOND ALL TIERS — outside policy scope, MUST be left untouched
// =========================================================================

func TestTieredGC_BeyondAllTiers_Untouched(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour}, // only covers <1h
	}
	entries := makeTimedEntries(
		[]time.Duration{30 * time.Minute, 2 * time.Hour, 24 * time.Hour, 30 * 24 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tiered0 (30m) is in keep-all tier — kept.
	// tiered1, tiered2, tiered3 are BEYOND all tiers — must NOT be touched.
	assertNothingDeleted(t, deleted, "beyond all tiers")
}

func TestTieredGC_BeyondAllTiers_WithDeleteTier(t *testing.T) {
	// User explicitly adds a delete tier for old data.
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour},                                       // keep all <1h
		{MaxAge: 7 * 24 * time.Hour, Resolution: dur(24 * time.Hour)}, // daily 1h-7d
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)},     // delete >7d
	}
	entries := makeTimedEntries(
		[]time.Duration{30 * time.Minute, 3 * 24 * time.Hour, 30 * 24 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tiered0 (30m) — keep-all tier.
	// tiered1 (3d) — daily tier, only one so kept.
	// tiered2 (30d) — delete tier.
	assertDeleted(t, deleted, []string{"sha256:tiered2"}, "explicit delete tier")
	assertNotDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered1"}, "explicit delete tier")
}

// =========================================================================
// 6. USER TAGS — must NEVER be deleted regardless of tier
// =========================================================================

func TestTieredGC_UserTagPreserved_InDeleteTier(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)}, // delete everything
	}
	entries := makeTimedEntries(
		[]time.Duration{1 * time.Hour, 2 * time.Hour, 3 * time.Hour},
		[]string{"milestone-v1", "v2-beta", "cp-5"},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// milestone-v1 and v2-beta are user tags — MUST be kept.
	// cp-5 is auto tag — deleted.
	assertNotDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered1"}, "user tags in delete tier")
	assertDeleted(t, deleted, []string{"sha256:tiered2"}, "auto tag in delete tier")
}

func TestTieredGC_UserTagPreserved_InResolutionTier(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 24 * time.Hour, Resolution: dur(1 * time.Hour)},
	}
	entries := makeTimedEntries(
		[]time.Duration{
			1*time.Hour + 10*time.Minute, // bucket 1 — newest
			1*time.Hour + 30*time.Minute, // bucket 1 — older, but user-tagged
		},
		[]string{"", "important-checkpoint"},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tiered1 would normally be pruned (second in bucket), but user tag protects it.
	assertNothingDeleted(t, deleted, "user tag in resolution tier bucket")
}

func TestTieredGC_UserTagPreserved_BeyondAllTiers(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour},
	}
	entries := makeTimedEntries(
		[]time.Duration{30 * 24 * time.Hour},
		[]string{"ancient-but-tagged"},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "user tag beyond all tiers")
}

func TestTieredGC_KeepTaggedFalse_TagsNotProtected(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)},
	}
	entries := makeTimedEntries(
		[]time.Duration{1 * time.Hour, 2 * time.Hour},
		[]string{"v1-release", "milestone"},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false) // keepTagged = false
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With keepTagged=false, user tags offer no protection.
	assertDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered1"}, "keepTagged false")
}

// =========================================================================
// 7. AUTO TAGS (cp-N, latest) — are NOT protected by keepTagged
// =========================================================================

func TestTieredGC_AutoTagsNotProtected(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)},
	}
	entries := makeTimedEntries(
		[]time.Duration{1 * time.Hour, 2 * time.Hour, 3 * time.Hour},
		[]string{"cp-1", "latest", "cp-42"},
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, true) // keepTagged = true, but these are auto tags
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered1", "sha256:tiered2"}, "auto tags")
}

// =========================================================================
// 8. MULTI-TIER scenarios — realistic configurations
// =========================================================================

func TestTieredGC_DefaultTiers(t *testing.T) {
	// DefaultWatchTiers: keep-all <1h, hourly <24h, daily <7d, untouched beyond.
	entries := makeTimedEntries(
		[]time.Duration{
			10 * time.Minute,             // in keep-all tier — kept
			30 * time.Minute,             // in keep-all tier — kept
			2*time.Hour + 10*time.Minute, // hourly tier, bucket 2 — kept (first)
			2*time.Hour + 30*time.Minute, // hourly tier, bucket 2 — deleted (second)
			5*time.Hour + 10*time.Minute, // hourly tier, bucket 5 — kept
			2 * 24 * time.Hour,           // daily tier, bucket 2 — kept
			3 * 24 * time.Hour,           // daily tier, bucket 3 — kept
			30 * 24 * time.Hour,          // beyond all tiers — untouched
		},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, DefaultWatchTiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only tiered3 (second in hourly bucket 2) should be deleted.
	assertDeleted(t, deleted, []string{"sha256:tiered3"}, "default tiers")
	assertNotDeleted(t, deleted,
		[]string{"sha256:tiered0", "sha256:tiered1", "sha256:tiered2",
			"sha256:tiered4", "sha256:tiered5", "sha256:tiered6", "sha256:tiered7"},
		"default tiers")
}

func TestTieredGC_KeepAllThenDelete(t *testing.T) {
	// Keep everything for 1h, then delete everything else.
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour},
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)},
	}
	entries := makeTimedEntries(
		[]time.Duration{10 * time.Minute, 50 * time.Minute, 2 * time.Hour, 24 * time.Hour},
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeleted(t, deleted, []string{"sha256:tiered2", "sha256:tiered3"}, "keep then delete")
	assertNotDeleted(t, deleted, []string{"sha256:tiered0", "sha256:tiered1"}, "keep then delete")
}

// =========================================================================
// 9. BOUNDARY CONDITIONS — checkpoints exactly at tier boundaries
// =========================================================================

func TestTieredGC_ExactlyAtBoundary(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 1 * time.Hour},          // keep all <= 1h
		{MaxAge: 24 * time.Hour, Resolution: dur(1 * time.Hour)}, // hourly 1h-24h
	}
	entries := makeTimedEntries(
		[]time.Duration{1 * time.Hour}, // exactly at boundary — should be in first tier (<=)
		nil,
	)
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNothingDeleted(t, deleted, "exactly at boundary")
}

// =========================================================================
// 10. DUPLICATE DIGESTS — same digest in multiple entries
// =========================================================================

func TestTieredGC_DuplicateDigests(t *testing.T) {
	tiers := []config.RetentionTier{
		{MaxAge: 100 * 365 * 24 * time.Hour, Resolution: dur(0)},
	}
	now := time.Now()
	entries := []registry.CheckpointEntry{
		{Digest: "sha256:same", Created: now.Add(-1 * time.Hour).Format(time.RFC3339), Tag: "cp-1"},
		{Digest: "sha256:same", Created: now.Add(-2 * time.Hour).Format(time.RFC3339), Tag: "cp-2"},
	}
	store := &mockStore{entries: entries}
	deleted, err := TieredGC(store, tiers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both entries have same digest — should only attempt to delete once.
	if len(deleted) > 1 {
		t.Errorf("duplicate digest should only be deleted once, got %d: %v", len(deleted), deleted)
	}
}

// =========================================================================
// 11. isUserTag — verify tag classification
// =========================================================================

func TestIsUserTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"", false},
		{"latest", false},
		{"cp-1", false},
		{"cp-42", false},
		{"cp-999", false},
		{"v1", true},
		{"milestone", true},
		{"cp-abc", true}, // has non-digits after cp-
		{"release-1.0", true},
		{"my-tag", true},
		{"cp-", false}, // edge: "cp-" with nothing after
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("tag=%q", tc.tag), func(t *testing.T) {
			if got := isUserTag(tc.tag); got != tc.want {
				t.Errorf("isUserTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}
