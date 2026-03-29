package policy

import (
	"fmt"
	"sort"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/registry"
)

// GCOptions configures garbage collection behavior.
type GCOptions struct {
	KeepLast   int
	KeepTagged bool
}

// GarbageCollect removes old checkpoints from the store according to the given
// options. It keeps the most recent KeepLast checkpoints and, if KeepTagged is
// true, any checkpoint that has a tag. It returns the list of deleted digests.
func GarbageCollect(store registry.Store, opts GCOptions) (deleted []string, err error) {
	entries, err := store.ListCheckpoints()
	if err != nil {
		return nil, err
	}

	// Sort by creation time, newest first.
	sort.Slice(entries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, entries[i].Created)
		tj, _ := time.Parse(time.RFC3339, entries[j].Created)
		return ti.After(tj)
	})

	keep := make(map[string]bool)

	// Keep the last N checkpoints.
	for i := 0; i < len(entries) && i < opts.KeepLast; i++ {
		keep[entries[i].Digest] = true
	}

	// Keep tagged checkpoints if requested.
	if opts.KeepTagged {
		for _, e := range entries {
			if e.Tag != "" {
				keep[e.Digest] = true
			}
		}
	}

	// Delete everything not marked to keep, deduplicating digests to avoid
	// attempting to delete the same digest twice (which would fail if a digest
	// appears under multiple tags in the index).
	deletedSet := make(map[string]bool)
	for _, e := range entries {
		if keep[e.Digest] {
			continue
		}
		if deletedSet[e.Digest] {
			continue
		}
		if err := store.DeleteCheckpoint(e.Digest); err != nil {
			return deleted, err
		}
		deletedSet[e.Digest] = true
		deleted = append(deleted, e.Digest)
	}

	return deleted, nil
}

// durationPtr is a helper that returns a pointer to a time.Duration.
func durationPtr(d time.Duration) *time.Duration { return &d }

// DefaultWatchTiers provides sensible tiered retention defaults for watch mode.
// Recent checkpoints are kept at full granularity, older ones are downsampled.
// Checkpoints older than 7d are left untouched (outside policy scope).
var DefaultWatchTiers = []config.RetentionTier{
	{MaxAge: 1 * time.Hour},                                        // keep everything
	{MaxAge: 24 * time.Hour, Resolution: durationPtr(1 * time.Hour)},   // keep hourly
	{MaxAge: 7 * 24 * time.Hour, Resolution: durationPtr(24 * time.Hour)}, // keep daily
}

// isUserTag returns true if the tag was explicitly set by a user (not auto-generated).
// Auto-generated tags follow the pattern "cp-N" or "latest".
func isUserTag(tag string) bool {
	if tag == "" || tag == "latest" {
		return false
	}
	// cp-N pattern: "cp-" followed by digits
	if len(tag) >= 3 && tag[:3] == "cp-" {
		if len(tag) == 3 {
			return false // bare "cp-" is auto-generated (malformed but not user)
		}
		for _, c := range tag[3:] {
			if c < '0' || c > '9' {
				return true // has non-digit chars after "cp-", so user-defined
			}
		}
		return false // purely "cp-<digits>"
	}
	return true
}

// TieredGC prunes checkpoints according to a declarative tiered retention policy.
// Only checkpoints that fall within a defined tier are subject to GC. The behavior
// for each tier depends on its Resolution:
//
//   - Resolution omitted (nil): keep all checkpoints in this age range
//   - Resolution 0:             keep none — delete all checkpoints in this age range
//   - Resolution > 0:           keep one checkpoint per Resolution interval (newest in each bucket)
//
// Checkpoints older than the last tier's max_age are NOT touched — they are
// outside the policy scope and left as-is. User-tagged checkpoints are never
// deleted when keepTagged is true.
func TieredGC(store registry.Store, tiers []config.RetentionTier, keepTagged bool) (deleted []string, err error) {
	entries, err := store.ListCheckpoints()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	now := time.Now()

	// Sort newest first.
	sort.Slice(entries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, entries[i].Created)
		tj, _ := time.Parse(time.RFC3339, entries[j].Created)
		return ti.After(tj)
	})

	keep := make(map[string]bool)
	remove := make(map[string]bool)

	// Always keep user-tagged checkpoints if requested.
	if keepTagged {
		for _, e := range entries {
			if isUserTag(e.Tag) {
				keep[e.Digest] = true
			}
		}
	}

	// Apply tiers: for each checkpoint, find the applicable tier and decide.
	bucketUsed := make(map[string]bool)

	for _, e := range entries {
		t, _ := time.Parse(time.RFC3339, e.Created)
		age := now.Sub(t)

		tierIdx := -1
		for i, tier := range tiers {
			if age <= tier.MaxAge {
				tierIdx = i
				break
			}
		}

		if tierIdx == -1 {
			// Beyond all tiers — outside policy scope, leave untouched.
			keep[e.Digest] = true
			continue
		}

		tier := tiers[tierIdx]

		if tier.Resolution == nil {
			// Resolution omitted: keep all in this tier.
			keep[e.Digest] = true
			continue
		}

		r := *tier.Resolution

		if r == 0 {
			// Resolution 0: delete all in this tier.
			remove[e.Digest] = true
			continue
		}

		// Resolution > 0: keep one per bucket.
		bucket := age / r
		bucketKey := fmt.Sprintf("%d:%d", tierIdx, bucket)
		if !bucketUsed[bucketKey] {
			keep[e.Digest] = true
			bucketUsed[bucketKey] = true
		} else {
			remove[e.Digest] = true
		}
	}

	// Delete entries marked for removal that aren't protected by keep.
	// A checkpoint in keep always wins over remove (e.g. user-tagged checkpoints
	// in a resolution:0 tier are still preserved).
	deletedSet := make(map[string]bool)
	for _, e := range entries {
		if keep[e.Digest] || !remove[e.Digest] || deletedSet[e.Digest] {
			continue
		}
		if err := store.DeleteCheckpoint(e.Digest); err != nil {
			return deleted, err
		}
		deletedSet[e.Digest] = true
		deleted = append(deleted, e.Digest)
	}

	return deleted, nil
}
